package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/originaleric/digcrawl/config"
	internalasr "github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/internal/ocr"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
)

// EnhanceOptions describes explicit enrichments requested through the Skill.
type EnhanceOptions struct {
	// Deprecated: CallID is ignored. It remains only for source compatibility
	// with existing Go callers; external correlation IDs must not identify
	// mutable local media state.
	CallID           string
	IncludeOCR       bool
	IncludeASR       bool
	IncludeSummary   bool
	SourceText       string
	ScreenshotBase64 string
	ArtifactStore    artifact.Store
	// LocalVideoPaths are provider-owned local MP4 inputs. The enhancement core
	// reads them for ASR/frame OCR and returns only extracted content; ownership
	// and cleanup remain with the provider.
	LocalVideoPaths []string
	// LocalImages are successfully retained remote image inputs for this call.
	// They are OCR-only process inputs: their absolute paths never leave this
	// package in output metadata or result payloads.
	LocalImages []LocalImageInput
}

// LocalImageInput associates one retained local image with the remote source
// URL that remains the public OCR result identity.
type LocalImageInput struct {
	Path      string
	SourceURL string
	Type      string
}

// OCRRequested is the direct-read OCR policy for a Skill profile. Enabling
// Skill.OCR is an operator opt-in to process page images by default; the
// per-call flag remains available for callers that need an explicit request.
// The default profile keeps OCR disabled, so ordinary keyless reads do not
// download images or send them to an OCR provider.
func OCRRequested(cfg config.SkillConfig, explicitlyRequested bool) bool {
	return explicitlyRequested || cfg.OCR.EnabledOrDefault()
}

// DirectReadOCRRequested centralizes the Skill OCR boundary used by every
// direct provider. Collect owns a separate root-level export/OCR pipeline, so
// a Collect context must never turn a Skill default into hidden media work.
func DirectReadOCRRequested(ctx context.Context, cfg config.SkillConfig, explicitlyRequested bool) bool {
	return !executor.IsCollectExecution(ctx) && OCRRequested(cfg, explicitlyRequested)
}

// Enhance applies the Skill-owned media/OCR/ASR/video-summary pipeline.
// Its media and output semantics deliberately mirror collect without importing it.
func Enhance(ctx context.Context, out map[string]any, cfg config.SkillConfig, opts EnhanceOptions) error {
	if out == nil || (!opts.IncludeOCR && !opts.IncludeASR && !opts.IncludeSummary) {
		return nil
	}
	needsAssetsDir, requiresASR, err := validateEnhancementRequest(out, cfg, opts)
	if err != nil {
		return err
	}
	assetsDir := ""
	if needsAssetsDir {
		assetsDir, err = newSkillAssetsDir(cfg.Media.Dir)
		if err != nil {
			return err
		}
	}
	videoFrameOCR := opts.IncludeOCR && cfg.OCR.VideoFrames.EnabledOrDefault()

	var downloaded []map[string]any
	var ocrDownloaded []map[string]any
	localDownloaded := localVideoEntries(opts.LocalVideoPaths)
	localImageRefs := localImageOCRRefs(opts.LocalImages)
	asrAttempted := false
	if assetsDir != "" && opts.IncludeOCR && !localImagesCoverAllOCRMedia(out, opts.LocalImages) {
		downloadOpts := AssetDownloadOptions{
			MaxBytes:      cfg.Media.MaxBytes,
			TimeoutSec:    cfg.Media.TimeoutSec,
			ArtifactStore: opts.ArtifactStore,
			ImagesOnly:    true,
		}
		if _, err := DownloadAssets(ctx, assetsDir, out, downloadOpts); err != nil {
			return err
		}
		ocrDownloaded = readDownloadedAssets(assetsDir)
	}
	downloaded = append(downloaded, ocrDownloaded...)
	downloaded = append(downloaded, localDownloaded...)
	if assetsDir != "" && requiresASR && len(localDownloaded) == 0 {
		downloaded, asrAttempted, err = downloadAndTranscribeASRCandidates(ctx, out, cfg.ASR, cfg.Media, assetsDir, ocrDownloaded, opts.ArtifactStore)
		if err != nil {
			return err
		}
	} else if requiresASR && len(localDownloaded) > 0 {
		asrAttempted = true
		enrichASRFromDownloadedMedia(ctx, out, cfg.ASR, localDownloaded, assetsDir)
	}
	if videoFrameOCR && len(downloadedVideoPaths(downloaded, assetsDir)) == 0 {
		videoDownloaded, downloadErr := downloadVideoFrameCandidate(ctx, out, cfg.Media, assetsDir, opts.ArtifactStore)
		if downloadErr != nil {
			return downloadErr
		}
		if len(videoDownloaded) > 0 {
			downloaded = append(downloaded, videoDownloaded...)
			if _, err := writeAssetsManifest(assetsDir, out, downloaded); err != nil {
				return err
			}
		}
	}

	if opts.IncludeOCR {
		var videoFrameRefs []ocr.ImageRef
		if videoFrameOCR {
			cleanupVideoFrames := func() {}
			videoFrameRefs, cleanupVideoFrames, err = ocr.ExtractLocalVideoFrameRefs(ctx, cfg.OCR, downloadedVideoPaths(downloaded, assetsDir))
			defer cleanupVideoFrames()
			meta := platformMetadata(out)
			meta["ocr_video_frame_count"] = len(videoFrameRefs)
			if err != nil {
				meta["ocr_video_frames_error"] = err.Error()
			} else {
				delete(meta, "ocr_video_frames_error")
			}
			out["platform_metadata"] = meta
		}
		ocrOpts := ocr.EnrichOptions{Downloaded: downloaded, ExtraImages: append(localImageRefs, videoFrameRefs...)}
		if assetsDir != "" {
			ocrOpts.AssetsDir = assetsDir
		}
		if opts.ScreenshotBase64 != "" {
			ocrOpts.ExtraImages = append(ocrOpts.ExtraImages, ocr.PNGDataURLImageRef("screenshot", opts.ScreenshotBase64))
		}
		ocr.EnrichMap(ctx, out, cfg.OCR, ocrOpts)
		if err := ocr.RequestedFailure(out); err != nil {
			return err
		}
	}

	if requiresASR && !asrAttempted {
		enrichASRFromDownloadedMedia(ctx, out, cfg.ASR, downloaded, assetsDir)
	}
	if opts.IncludeASR && strings.TrimSpace(stringValue(out["asr_text"])) == "" {
		return fmt.Errorf("ASR unavailable: %s", firstNonEmpty(metadataText(out, "asr_error"), "no downloaded video/audio asset was available for ASR"))
	}
	if opts.IncludeSummary {
		if err := summarizeVideo(ctx, out, cfg.Summary, opts.SourceText); err != nil {
			return err
		}
	}
	return nil
}

func localImageOCRRefs(images []LocalImageInput) []ocr.ImageRef {
	refs := make([]ocr.ImageRef, 0, len(images))
	for _, image := range images {
		path := strings.TrimSpace(image.Path)
		if path == "" {
			continue
		}
		mediaType := strings.ToLower(strings.TrimSpace(image.Type))
		if mediaType != "image" && mediaType != "photo" {
			continue
		}
		refs = append(refs, ocr.ImageRef{LocalPath: path, URL: strings.TrimSpace(image.SourceURL), Type: mediaType})
	}
	return refs
}

// localImagesCoverAllOCRMedia is intentionally scoped to this Enhance call.
// Retained files only suppress the existing ImagesOnly acquisition when every
// remote image/photo already has a matching local input; otherwise the old
// all-image download path remains authoritative.
func localImagesCoverAllOCRMedia(out map[string]any, images []LocalImageInput) bool {
	if len(images) == 0 {
		return false
	}
	covered := make(map[string]struct{}, len(images))
	for _, image := range images {
		if path, source := strings.TrimSpace(image.Path), strings.TrimSpace(image.SourceURL); path != "" && source != "" {
			covered[source] = struct{}{}
		}
	}
	candidates := 0
	seen := map[string]struct{}{}
	for _, item := range extractMediaItems(out) {
		mediaType, _ := item["type"].(string)
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
		if mediaType != "image" && mediaType != "photo" {
			continue
		}
		rawURL, _ := item["url"].(string)
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		if _, exists := seen[rawURL]; exists {
			continue
		}
		seen[rawURL] = struct{}{}
		candidates++
		if _, ok := covered[rawURL]; !ok {
			return false
		}
	}
	return candidates > 0
}

// ValidateVideoEnhancementRequest applies the same opt-in checks as Enhance
// before a video provider acquires local media for an explicit Skill request.
func ValidateVideoEnhancementRequest(cfg config.SkillConfig, opts EnhanceOptions) error {
	_, _, err := validateEnhancementRequest(map[string]any{"content_type": "video"}, cfg, opts)
	return err
}

// downloadAndTranscribeASRCandidates keeps download and transcription coupled
// for every ASR call. OCR assets are passed through unchanged, while only
// audio/video candidates consume the ASR attempt budget.
func downloadAndTranscribeASRCandidates(ctx context.Context, out map[string]any, asrCfg config.ASRConfig, mediaCfg config.SkillMediaConfig, assetsDir string, initialDownloaded []map[string]any, artStore artifact.Store) ([]map[string]any, bool, error) {
	downloaded := append([]map[string]any(nil), initialDownloaded...)
	hasLocalASRInput := false
	for candidate := 0; candidate < mediaCfg.MaxASRCandidatesOrDefault(); candidate++ {
		if _, err := DownloadAssets(ctx, assetsDir, out, AssetDownloadOptions{
			MaxBytes:          mediaCfg.MaxBytes,
			TimeoutSec:        mediaCfg.TimeoutSec,
			ArtifactStore:     artStore,
			ASRInputOnly:      true,
			ASRCandidateIndex: candidate,
		}); err != nil {
			return downloaded, hasLocalASRInput, err
		}
		attempt := asrMediaEntries(readDownloadedAssets(assetsDir))
		if len(attempt) == 0 {
			break
		}
		downloaded = append(downloaded, attempt...)
		if !hasLocalASRMedia(attempt) {
			continue
		}
		hasLocalASRInput = true
		enrichASRFromDownloadedMedia(ctx, out, asrCfg, attempt, assetsDir)
		if strings.TrimSpace(stringValue(out["asr_text"])) != "" {
			break
		}
	}
	if _, err := writeAssetsManifest(assetsDir, out, downloaded); err != nil {
		return downloaded, hasLocalASRInput, err
	}
	if !hasLocalASRInput {
		if err := asrMediaDownloadError(downloaded); err != nil {
			return downloaded, false, err
		}
	}
	return downloaded, hasLocalASRInput, nil
}

func asrMediaEntries(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if isASRMediaType(stringValue(item["type"])) {
			out = append(out, item)
		}
	}
	return out
}

func hasLocalASRMedia(items []map[string]any) bool {
	for _, item := range items {
		if isASRMediaType(stringValue(item["type"])) && strings.TrimSpace(stringValue(item["local"])) != "" {
			return true
		}
	}
	return false
}

// validateEnhancementRequest enforces explicit Skill configuration before any
// media download, transcription, or summary-provider work begins.
func validateEnhancementRequest(out map[string]any, cfg config.SkillConfig, opts EnhanceOptions) (bool, bool, error) {
	if opts.IncludeOCR && !cfg.OCR.EnabledOrDefault() {
		return false, false, fmt.Errorf("Skill.OCR.Enabled must be true when OCR is requested")
	}

	videoSummary := opts.IncludeSummary && isVideo(out)
	videoFrameOCR := opts.IncludeOCR && isVideo(out) && cfg.OCR.VideoFrames.EnabledOrDefault()
	requiresASR := opts.IncludeASR || videoSummary
	if requiresASR || videoFrameOCR {
		if !cfg.Media.DownloadMedia {
			return false, false, fmt.Errorf("Skill.Media.DownloadMedia must be true when ASR, video summary, or video-frame OCR is requested")
		}
	}
	if requiresASR {
		if !cfg.ASR.EnabledOrDefault() {
			return false, false, fmt.Errorf("Skill.ASR.Enabled must be true when ASR or video summary is requested")
		}
	}
	if videoSummary {
		if !cfg.Summary.EnabledOrDefault() {
			return false, false, fmt.Errorf("Skill.Summary.Enabled must be true when a local video summary is requested")
		}
		if strings.TrimSpace(os.ExpandEnv(cfg.Summary.LLM.APIKey)) == "" {
			return false, false, fmt.Errorf("Skill.Summary.LLM.APIKey is required when a local video summary is requested")
		}
	}

	// Provider-owned local video inputs are consumed directly by ASR and frame
	// OCR. A Skill directory is only needed when this execution itself acquires
	// page assets or ASR candidates.
	localVideoInputs := localVideoEntries(opts.LocalVideoPaths)
	needsOCRAssetDownload := opts.IncludeOCR && (!localImagesCoverAllOCRMedia(out, opts.LocalImages) || (videoFrameOCR && len(localVideoInputs) == 0))
	needsAssetsDir := cfg.Media.DownloadMedia && (needsOCRAssetDownload || (requiresASR && len(localVideoInputs) == 0))
	if needsAssetsDir {
		if err := validateSkillAssetsRoot(cfg.Media.Dir); err != nil {
			return false, false, err
		}
	}
	return needsAssetsDir, requiresASR, nil
}

func validateSkillAssetsRoot(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("Skill.Media.Dir is required when media download is requested")
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("Skill.Media.Dir must be an absolute path")
	}
	return nil
}

// newSkillAssetsDir creates a private, execution-owned directory. Call IDs are
// correlation data supplied by a transport and must never identify mutable
// local media state.
func newSkillAssetsDir(root string) (string, error) {
	if err := validateSkillAssetsRoot(root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", fmt.Errorf("create Skill.Media.Dir: %w", err)
	}
	return os.MkdirTemp(root, "digcrawl-skill-assets-*")
}

func readDownloadedAssets(assetsDir string) []map[string]any {
	data, err := os.ReadFile(filepath.Join(assetsDir, "media.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Downloaded []map[string]any `json:"downloaded"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	return manifest.Downloaded
}

func downloadedVideoPaths(downloaded []map[string]any, assetsDir string) []string {
	paths := make([]string, 0, len(downloaded))
	for _, item := range downloaded {
		if !strings.EqualFold(stringValue(item["type"]), "video") {
			continue
		}
		path := localMediaPath(item, assetsDir)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func localVideoEntries(paths []string) []map[string]any {
	entries := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		entries = append(entries, map[string]any{"type": "video", "absolute_path": path})
	}
	return entries
}

func localMediaPath(item map[string]any, assetsDir string) string {
	if absolute := strings.TrimSpace(stringValue(item["absolute_path"])); absolute != "" {
		return filepath.Clean(absolute)
	}
	local := strings.TrimSpace(stringValue(item["local"]))
	if local == "" || assetsDir == "" {
		return ""
	}
	return filepath.Join(assetsDir, filepath.Base(local))
}

// downloadVideoFrameCandidate reuses the existing bounded single-candidate
// acquisition path, narrowed to video media so audio cannot consume the frame
// OCR slot. The original output remains untouched for regular image OCR.
func downloadVideoFrameCandidate(ctx context.Context, out map[string]any, mediaCfg config.SkillMediaConfig, assetsDir string, artStore artifact.Store) ([]map[string]any, error) {
	var videos []any
	for _, item := range extractMediaItems(out) {
		if strings.EqualFold(stringValue(item["type"]), "video") {
			videos = append(videos, item)
		}
	}
	if len(videos) == 0 {
		return nil, nil
	}
	videoOut := make(map[string]any, len(out)+1)
	for key, value := range out {
		videoOut[key] = value
	}
	videoOut["media"] = videos
	if _, err := DownloadAssets(ctx, assetsDir, videoOut, AssetDownloadOptions{
		MaxBytes:      mediaCfg.MaxBytes,
		TimeoutSec:    mediaCfg.TimeoutSec,
		ArtifactStore: artStore,
		ASRInputOnly:  true,
	}); err != nil {
		return nil, err
	}
	return readDownloadedAssets(assetsDir), nil
}

const mediaDownloadFailedCode = "MEDIA_DOWNLOAD_FAILED"

// mediaDownloadError makes a failed ASR input acquisition visible at the tool
// boundary without exposing raw HTTP errors, which may contain signed URLs.
type mediaDownloadError struct {
	mediaType string
	reason    string
	retryable bool
	actual    int64
	limit     int64
}

func (e *mediaDownloadError) Error() string {
	if e == nil {
		return ""
	}
	if e.reason == "size_limit_exceeded" && e.actual > 0 && e.limit > 0 {
		return fmt.Sprintf("media download failed: %s size %d bytes exceeds configured limit %d bytes", e.mediaType, e.actual, e.limit)
	}
	return fmt.Sprintf("media download failed: %s download %s", e.mediaType, strings.ReplaceAll(e.reason, "_", " "))
}

func (e *mediaDownloadError) ToolErrorCode() string { return mediaDownloadFailedCode }

func (e *mediaDownloadError) ToolErrorMessage() string { return e.Error() }

func (e *mediaDownloadError) ToolErrorDetails() map[string]any {
	if e == nil {
		return nil
	}
	details := map[string]any{
		"component":  "media_download",
		"media_type": e.mediaType,
		"reason":     e.reason,
		"retryable":  e.retryable,
	}
	if e.actual > 0 {
		details["actual_bytes"] = e.actual
	}
	if e.limit > 0 {
		details["max_bytes"] = e.limit
	}
	return details
}

// asrMediaDownloadError exposes an acquisition failure before ASR starts.
// DownloadAssets records each attempted item in media.json so image downloads
// can remain best-effort; ASR, however, requires at least one local audio or
// video file and must not hide a failed media download behind an ASR fallback
// error.
func asrMediaDownloadError(items []map[string]any) error {
	for _, item := range items {
		mediaType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
		if (mediaType == "video" || mediaType == "audio") && strings.TrimSpace(stringValue(item["local"])) != "" {
			return nil
		}
	}
	for _, item := range items {
		mediaType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
		if mediaType != "video" && mediaType != "audio" {
			continue
		}
		if reason := strings.TrimSpace(stringValue(item["error"])); reason != "" {
			return newMediaDownloadError(mediaType, reason)
		}
	}
	return nil
}

func newMediaDownloadError(mediaType, rawReason string) *mediaDownloadError {
	reason := strings.ToLower(strings.TrimSpace(rawReason))
	err := &mediaDownloadError{mediaType: mediaType, reason: "unclassified", retryable: true}
	switch {
	case strings.Contains(reason, "exceeds max"):
		err.reason = "size_limit_exceeded"
		err.retryable = false
		sizeSource := reason
		if offset := strings.Index(sizeSource, "content length "); offset >= 0 {
			sizeSource = sizeSource[offset+len("content length "):]
		}
		values := strings.FieldsFunc(sizeSource, func(r rune) bool { return r < '0' || r > '9' })
		if len(values) >= 2 {
			err.actual, _ = strconv.ParseInt(values[0], 10, 64)
			err.limit, _ = strconv.ParseInt(values[1], 10, 64)
		}
	case strings.Contains(reason, "deadline exceeded"), strings.Contains(reason, "client.timeout"):
		err.reason = "timeout"
	case strings.Contains(reason, "http 429"):
		err.reason = "rate_limited"
	case strings.Contains(reason, "http 408"):
		err.reason = "timeout"
	case strings.Contains(reason, "media url missing"), strings.Contains(reason, "private/local"), strings.Contains(reason, "unsupported asset scheme"), strings.Contains(reason, "asset url missing host"):
		err.reason = "invalid_media_url"
		err.retryable = false
	case strings.Contains(reason, "http 4"):
		err.reason = "http_client_error"
		err.retryable = false
	case strings.Contains(reason, "http 5"):
		err.reason = "http_server_error"
	}
	return err
}

func enrichASRFromDownloadedMedia(ctx context.Context, out map[string]any, cfg config.ASRConfig, downloaded []map[string]any, assetsDir string) bool {
	if len(downloaded) == 0 {
		return false
	}
	meta := platformMetadata(out)
	changed := false
	for _, item := range downloaded {
		mediaType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
		if mediaType != "video" && mediaType != "audio" {
			continue
		}
		path := localMediaPath(item, assetsDir)
		if path == "" {
			continue
		}
		res, err := internalasr.Transcribe(ctx, path, cfg, internalasr.Options{})
		if err != nil {
			meta["asr_error"] = err.Error()
			changed = true
			continue
		}
		if res == nil || strings.TrimSpace(res.Text) == "" {
			meta["asr_error"] = "ASR returned empty transcript"
			changed = true
			continue
		}
		delete(meta, "asr_error")
		out["asr_text"] = strings.TrimSpace(res.Text)
		meta["asr_provider"] = res.Provider
		meta["asr_model"] = res.Model
		meta["asr_language"] = res.Language
		meta["asr_duration"] = res.Duration
		meta["asr_media_local"] = filepath.Base(path)
		out["platform_metadata"] = meta
		return true
	}
	out["platform_metadata"] = meta
	return changed
}

func summarizeVideo(ctx context.Context, out map[string]any, cfg config.SkillSummaryConfig, sourceText string) error {
	if !isVideo(out) {
		return nil
	}
	asrText := strings.TrimSpace(stringValue(out["asr_text"]))
	if asrText == "" {
		return fmt.Errorf("video transcript unavailable: %s", firstNonEmpty(metadataText(out, "asr_error"), "no downloaded video/audio asset was available for ASR"))
	}
	itemSummary, err := requestLocalVideoSummary(ctx, out, cfg, sourceText, asrText)
	if err != nil {
		return err
	}
	if err := validateVideoSummary(asrText, itemSummary); err != nil {
		return err
	}
	url := firstNonEmpty(stringValue(out["canonical_url"]), stringValue(out["source_url"]))
	title := stringValue(out["title"])
	out["summary"] = map[string]any{
		"digest_title":    firstNonEmpty(title, "Video Summary"),
		"overall_summary": itemSummary,
		"items": []map[string]any{{
			"item_id": stringValue(out["canonical_url"]),
			"title":   title,
			"url":     url,
			"summary": itemSummary,
		}},
		"summary_provider": "llm_direct",
	}
	return nil
}

func requestLocalVideoSummary(ctx context.Context, out map[string]any, cfg config.SkillSummaryConfig, sourceText, asrText string) (string, error) {
	b := cfg.InputBudgetOrDefault()
	input := map[string]any{
		"platform":     stringValue(out["platform"]),
		"title":        stringValue(out["title"]),
		"url":          firstNonEmpty(stringValue(out["canonical_url"]), stringValue(out["source_url"])),
		"published_at": stringValue(out["published_at"]),
		"text":         trimSummaryInput(sourceText, b.TextChars),
		"asr_text":     trimSummaryInput(asrText, b.ASRChars),
		"ocr_text":     trimSummaryInput(extractOCRText(out), b.OCRChars),
	}
	for encoded, _ := json.Marshal(input); len(encoded) > cfg.MaxInputCharsOrDefault(); encoded, _ = json.Marshal(input) {
		if text := stringValue(input["text"]); text != "" {
			input["text"] = halveSummaryInput(text)
			continue
		}
		if transcript := stringValue(input["asr_text"]); transcript != "" {
			input["asr_text"] = halveSummaryInput(transcript)
			continue
		}
		input["ocr_text"] = halveSummaryInput(stringValue(input["ocr_text"]))
		if stringValue(input["ocr_text"]) == "" {
			break
		}
	}
	inputData, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode local video summary input: %w", err)
	}

	baseURL := strings.TrimRight(os.ExpandEnv(strings.TrimSpace(cfg.LLM.BaseURL)), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.ExpandEnv(strings.TrimSpace(cfg.LLM.Model))
	if model == "" {
		model = "gpt-4o-mini"
	}
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是视频内容摘要助手。仅基于输入事实，输出严格 JSON：{\"summary\":\"不超过240字的中文摘要\"}。不得转写全文或编造。"},
			{"role": "user", "content": string(inputData)},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", fmt.Errorf("encode local video summary request: %w", err)
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSecOrDefault())*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create local video summary request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.ExpandEnv(strings.TrimSpace(cfg.LLM.APIKey)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("local video summary request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read local video summary response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("local video summary provider returned HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Choices) == 0 {
		return "", fmt.Errorf("local video summary provider returned no content")
	}
	var result struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(envelope.Choices[0].Message.Content), &result); err != nil {
		return "", fmt.Errorf("local video summary provider returned invalid JSON")
	}
	return strings.TrimSpace(result.Summary), nil
}

func validateVideoSummary(asrText, itemSummary string) error {
	itemSummary = strings.TrimSpace(itemSummary)
	if itemSummary == "" {
		return fmt.Errorf("local video summary provider returned no summary")
	}
	if utf8.RuneCountInString(itemSummary) > 240 {
		return fmt.Errorf("local video summary provider returned overlong summary")
	}
	if compactForCompare(itemSummary) == compactForCompare(asrText) {
		return fmt.Errorf("local video summary provider copied ASR transcript verbatim")
	}
	return nil
}

func trimSummaryInput(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	head := max * 3 / 5
	tail := max - head
	return strings.TrimSpace(string(runes[:head])) + "\n...\n" + strings.TrimSpace(string(runes[len(runes)-tail:]))
}

func halveSummaryInput(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= 1 {
		return ""
	}
	return string(runes[:len(runes)/2])
}

func isVideo(out map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(stringValue(out["content_type"])), "video")
}

func platformMetadata(out map[string]any) map[string]any {
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	return meta
}

func metadataText(out map[string]any, key string) string {
	return stringValue(platformMetadata(out)[key])
}

func extractOCRText(out map[string]any) string {
	meta := platformMetadata(out)
	if text := stringValue(meta["ocr_text"]); text != "" {
		return text
	}
	data, err := json.Marshal(meta["ocr_results"])
	if err != nil {
		return ""
	}
	var results []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &results) != nil {
		return ""
	}
	var parts []string
	for _, result := range results {
		if text := strings.TrimSpace(result.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func compactForCompare(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), "")
}
