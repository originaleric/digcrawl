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
)

// EnhanceOptions describes explicit enrichments requested through the Skill.
type EnhanceOptions struct {
	CallID           string
	IncludeOCR       bool
	IncludeASR       bool
	IncludeSummary   bool
	SourceText       string
	ScreenshotBase64 string
	ArtifactStore    artifact.Store
}

// Enhance applies the Skill-owned media/OCR/ASR/video-summary pipeline.
// Its media and output semantics deliberately mirror collect without importing it.
func Enhance(ctx context.Context, out map[string]any, cfg config.SkillConfig, opts EnhanceOptions) error {
	if out == nil || (!opts.IncludeOCR && !opts.IncludeASR && !opts.IncludeSummary) {
		return nil
	}
	assetsDir, requiresASR, err := validateEnhancementRequest(out, cfg, opts)
	if err != nil {
		return err
	}

	var downloaded []map[string]any
	var ocrDownloaded []map[string]any
	asrAttempted := false
	if assetsDir != "" && opts.IncludeOCR {
		if _, err := DownloadAssets(ctx, assetsDir, out, AssetDownloadOptions{
			MaxBytes:      cfg.Media.MaxBytes,
			TimeoutSec:    cfg.Media.TimeoutSec,
			ArtifactStore: opts.ArtifactStore,
			ImagesOnly:    true,
		}); err != nil {
			return err
		}
		ocrDownloaded = readDownloadedAssets(assetsDir)
	}
	if assetsDir != "" && requiresASR {
		downloaded, asrAttempted, err = downloadAndTranscribeASRCandidates(ctx, out, cfg.ASR, cfg.Media, assetsDir, ocrDownloaded)
		if err != nil {
			return err
		}
	} else {
		downloaded = ocrDownloaded
	}

	if opts.IncludeOCR {
		ocrOpts := ocr.EnrichOptions{Downloaded: downloaded}
		if assetsDir != "" {
			ocrOpts.AssetsDir = assetsDir
		}
		if opts.ScreenshotBase64 != "" {
			ocrOpts.ExtraImages = []ocr.ImageRef{ocr.PNGDataURLImageRef("screenshot", opts.ScreenshotBase64)}
		}
		ocr.EnrichMap(ctx, out, cfg.OCR, ocrOpts)
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

// downloadAndTranscribeASRCandidates keeps download and transcription coupled
// for every ASR call. OCR assets are passed through unchanged, while only
// audio/video candidates consume the ASR attempt budget.
func downloadAndTranscribeASRCandidates(ctx context.Context, out map[string]any, asrCfg config.ASRConfig, mediaCfg config.SkillMediaConfig, assetsDir string, initialDownloaded []map[string]any) ([]map[string]any, bool, error) {
	downloaded := append([]map[string]any(nil), initialDownloaded...)
	hasLocalASRInput := false
	for candidate := 0; candidate < mediaCfg.MaxASRCandidatesOrDefault(); candidate++ {
		if _, err := DownloadAssets(ctx, assetsDir, out, AssetDownloadOptions{
			MaxBytes:          mediaCfg.MaxBytes,
			TimeoutSec:        mediaCfg.TimeoutSec,
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
func validateEnhancementRequest(out map[string]any, cfg config.SkillConfig, opts EnhanceOptions) (string, bool, error) {
	if opts.IncludeOCR && !cfg.OCR.EnabledOrDefault() {
		return "", false, fmt.Errorf("Skill.OCR.Enabled must be true when OCR is requested")
	}

	videoSummary := opts.IncludeSummary && isVideo(out)
	requiresASR := opts.IncludeASR || videoSummary
	if requiresASR {
		if !cfg.Media.DownloadMedia {
			return "", false, fmt.Errorf("Skill.Media.DownloadMedia must be true when ASR or video summary is requested")
		}
		if !cfg.ASR.EnabledOrDefault() {
			return "", false, fmt.Errorf("Skill.ASR.Enabled must be true when ASR or video summary is requested")
		}
	}
	if videoSummary {
		if !cfg.Summary.EnabledOrDefault() {
			return "", false, fmt.Errorf("Skill.Summary.Enabled must be true when a local video summary is requested")
		}
		if strings.TrimSpace(os.ExpandEnv(cfg.Summary.LLM.APIKey)) == "" {
			return "", false, fmt.Errorf("Skill.Summary.LLM.APIKey is required when a local video summary is requested")
		}
	}

	if cfg.Media.DownloadMedia && (opts.IncludeOCR || requiresASR) {
		assetsDir, err := skillAssetsDir(cfg.Media.Dir, opts.CallID)
		if err != nil {
			return "", false, err
		}
		return assetsDir, requiresASR, nil
	}
	return "", requiresASR, nil
}

func skillAssetsDir(root, callID string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("Skill.Media.Dir is required when media download is requested")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("Skill.Media.Dir must be an absolute path")
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return "", fmt.Errorf("skill media call ID is required")
	}
	return filepath.Join(root, filepath.Base(callID)), nil
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
		local := strings.TrimSpace(stringValue(item["local"]))
		if local == "" || assetsDir == "" {
			continue
		}
		res, err := internalasr.Transcribe(ctx, filepath.Join(assetsDir, filepath.Base(local)), cfg, internalasr.Options{})
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
		meta["asr_media_local"] = local
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
