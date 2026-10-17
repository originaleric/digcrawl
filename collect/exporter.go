package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	internalasr "github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/internal/ocr"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

// ExportOptions configures local file export.
type ExportOptions struct {
	OutputDir     string
	Formats       []string
	Now           time.Time
	DownloadMedia bool
	// IncludeMedia controls whether the exported content exposes its media
	// field. A nil value preserves the historic direct Export behavior.
	// DownloadMedia always uses the unfiltered execution result as its input.
	IncludeMedia      *bool
	SkipVideoDownload bool
	SkipImageDownload bool
	MaxMediaBytes     int64
	MediaTimeoutSec   int
	ArtifactStore     artifact.Store
	OCR               config.OCRConfig
	ASR               config.ASRConfig
}

// ExportResult lists written files.
type ExportResult struct {
	Paths      OutputPaths
	Written    []string
	Downloaded []map[string]any
}

// Export writes collect output to Markdown / HTML / JSON, downloads media, and updates assets manifest.
func Export(ctx context.Context, out map[string]any, opts ExportOptions) (ExportResult, error) {
	opts, paths, err := prepareExport(out, opts)
	if err != nil {
		return ExportResult{}, err
	}
	return exportToPaths(ctx, out, opts, paths)
}

// prepareExport normalizes output options and finds a currently unused path.
// Callers that coordinate concurrent exports may reserve the returned path
// before invoking exportToPaths.
func prepareExport(out map[string]any, opts ExportOptions) (ExportOptions, OutputPaths, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	opts.Formats = normalizeExportFormats(opts.Formats)
	title, _ := out["title"].(string)
	paths, err := BuildOutputPaths(opts.OutputDir, title, opts.Now)
	if err != nil {
		return opts, OutputPaths{}, err
	}
	paths, err = UniquePaths(paths, opts.Formats)
	if err != nil {
		return opts, OutputPaths{}, err
	}
	return opts, paths, nil
}

func exportToPaths(ctx context.Context, out map[string]any, opts ExportOptions, paths OutputPaths) (ExportResult, error) {
	formats := opts.Formats
	if opts.OCR.EnabledOrDefault() && !opts.DownloadMedia {
		ocr.EnrichMap(ctx, out, opts.OCR, ocr.EnrichOptions{})
	}

	var written []string

	visibleOut := exportVisibleOutput(out, opts)
	if containsFormat(formats, "md") {
		body := pickMarkdownBody(visibleOut)
		if err := os.WriteFile(paths.Markdown, []byte(body), 0o644); err != nil {
			return ExportResult{}, err
		}
		written = append(written, paths.Markdown)
	}

	if containsFormat(formats, "html") {
		body := pickHTMLBody(visibleOut)
		if body != "" {
			if err := os.WriteFile(paths.HTML, []byte(body), 0o644); err != nil {
				return ExportResult{}, err
			}
			written = append(written, paths.HTML)
		}
	}

	if containsFormat(formats, "json") {
		data, err := json.MarshalIndent(visibleOut, "", "  ")
		if err != nil {
			return ExportResult{}, err
		}
		if err := os.WriteFile(paths.JSON, data, 0o644); err != nil {
			return ExportResult{}, err
		}
		written = append(written, paths.JSON)
	}

	var downloaded []map[string]any
	ocrAppliedAfterWrite := false
	if opts.DownloadMedia {
		mediaOut := withoutRetainedMedia(out, opts.SkipVideoDownload, opts.SkipImageDownload)
		assetFiles, err := DownloadAssets(ctx, paths.Assets, mediaOut, AssetDownloadOptions{
			MaxBytes:      opts.MaxMediaBytes,
			TimeoutSec:    opts.MediaTimeoutSec,
			ArtifactStore: opts.ArtifactStore,
		})
		if err != nil {
			return ExportResult{}, err
		}
		written = append(written, assetFiles...)
		if data, err := os.ReadFile(filepath.Join(paths.Assets, "media.json")); err == nil {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				if d, ok := m["downloaded"].([]any); ok {
					for _, item := range d {
						if mm, ok := item.(map[string]any); ok {
							downloaded = append(downloaded, mm)
						}
					}
				}
			}
		}
		func() {
			var videoFrameRefs []ocr.ImageRef
			if opts.OCR.EnabledOrDefault() && opts.OCR.VideoFrames.EnabledOrDefault() {
				cleanupVideoFrames := func() {}
				videoFrameRefs, cleanupVideoFrames, err = ocr.ExtractLocalVideoFrameRefs(ctx, opts.OCR, downloadedVideoPaths(downloaded, paths.Assets))
				defer cleanupVideoFrames()
				meta := platformMetadata(out)
				meta["ocr_video_frame_count"] = len(videoFrameRefs)
				if err != nil {
					meta["ocr_video_frames_error"] = err.Error()
				} else {
					delete(meta, "ocr_video_frames_error")
				}
				out["platform_metadata"] = meta
				ocrAppliedAfterWrite = true
			}
			if opts.OCR.EnabledOrDefault() {
				if results := ocr.EnrichMap(ctx, out, opts.OCR, ocr.EnrichOptions{
					AssetsDir:   paths.Assets,
					Downloaded:  downloaded,
					ExtraImages: videoFrameRefs,
				}); len(results) > 0 {
					ocrAppliedAfterWrite = true
				}
			}
		}()
		if opts.ASR.EnabledOrDefault() {
			if enrichASRFromDownloadedMedia(ctx, out, opts.ASR, downloaded, paths.Assets) {
				ocrAppliedAfterWrite = true
			}
		}
	} else {
		manifestFiles, err := writeAssetsManifest(paths.Assets, out, nil)
		if err != nil {
			return ExportResult{}, err
		}
		written = append(written, manifestFiles...)
	}

	if ocrAppliedAfterWrite {
		if err := rewriteTextualOutputs(exportVisibleOutput(out, opts), paths, formats); err != nil {
			return ExportResult{}, err
		}
	}

	if containsFormat(formats, "md") && len(downloaded) > 0 {
		_ = RewriteMarkdownAssets(paths.Markdown, paths.Assets, downloaded)
	}

	return ExportResult{Paths: paths, Written: written, Downloaded: downloaded}, nil
}

// exportVisibleOutput is deliberately separate from the media input given to
// DownloadAssets. Collect may need hidden media references to complete an
// assets download after a provider-retention failure, while its published
// Markdown/HTML/JSON must still honor Collect.IncludeMedia.
func exportVisibleOutput(out map[string]any, opts ExportOptions) map[string]any {
	if opts.IncludeMedia == nil || *opts.IncludeMedia {
		return out
	}
	if out == nil || out["media"] == nil {
		return out
	}
	visible := make(map[string]any, len(out)-1)
	for key, value := range out {
		if key != "media" {
			visible[key] = value
		}
	}
	return visible
}

func withoutVideoMedia(out map[string]any) map[string]any {
	return withoutRetainedMedia(out, true, false)
}

// withoutRetainedMedia composes the independent image/video retention flags
// over one copied media list. Starting both filters from the original output
// would accidentally reintroduce a media type when both flags are true.
func withoutRetainedMedia(out map[string]any, skipVideo, skipImage bool) map[string]any {
	if !skipVideo && !skipImage {
		return out
	}
	if out == nil || out["media"] == nil {
		return out
	}
	filtered := make(map[string]any, len(out))
	for key, value := range out {
		filtered[key] = value
	}
	items := extractMediaItems(out)
	// extractMediaItems accepts the public []any representation. Preserve it
	// here so the filtered copy can travel through the ordinary asset exporter
	// without changing its media contract.
	filteredItems := make([]any, 0, len(items))
	for _, item := range items {
		mediaType, _ := item["type"].(string)
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
		if skipVideo && mediaType == "video" {
			continue
		}
		if skipImage && (mediaType == "image" || mediaType == "photo") {
			continue
		}
		filteredItems = append(filteredItems, item)
	}
	filtered["media"] = filteredItems
	return filtered
}

func rewriteTextualOutputs(out map[string]any, paths OutputPaths, formats []string) error {
	if containsFormat(formats, "md") {
		if err := os.WriteFile(paths.Markdown, []byte(pickMarkdownBody(out)), 0o644); err != nil {
			return err
		}
	}
	if containsFormat(formats, "html") {
		body := pickHTMLBody(out)
		if body != "" {
			if err := os.WriteFile(paths.HTML, []byte(body), 0o644); err != nil {
				return err
			}
		}
	}
	if containsFormat(formats, "json") {
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(paths.JSON, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func normalizeExportFormats(formats []string) []string {
	if len(formats) == 0 {
		return []string{"md", "json"}
	}
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		f = strings.ToLower(strings.TrimSpace(f))
		switch f {
		case "md", "markdown":
			out = append(out, "md")
		case "html", "htm":
			out = append(out, "html")
		case "json":
			out = append(out, "json")
		}
	}
	if len(out) == 0 {
		return []string{"md", "json"}
	}
	return out
}

func containsFormat(formats []string, want string) bool {
	for _, f := range formats {
		if f == want {
			return true
		}
	}
	return false
}

func pickMarkdownBody(out map[string]any) string {
	if md, ok := out["markdown"].(string); ok && strings.TrimSpace(md) != "" {
		return buildMarkdownDocument(out, md)
	}
	if text, ok := out["text"].(string); ok && strings.TrimSpace(text) != "" {
		return buildMarkdownDocument(out, text)
	}
	return buildMarkdownDocument(out, "")
}

func buildMarkdownDocument(out map[string]any, body string) string {
	title, _ := out["title"].(string)
	var b strings.Builder
	if title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	if src, ok := out["source_url"].(string); ok && src != "" {
		b.WriteString("> Source: ")
		b.WriteString(src)
		b.WriteString("\n\n")
	}
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	if asrText, _ := out["asr_text"].(string); strings.TrimSpace(asrText) != "" && !strings.Contains(body, asrText) {
		if body != "" {
			b.WriteString("\n")
		}
		b.WriteString("## ASR\n\n")
		b.WriteString(strings.TrimSpace(asrText))
		b.WriteString("\n")
	}
	return b.String()
}

func enrichASRFromDownloadedMedia(ctx context.Context, out map[string]any, cfg config.ASRConfig, downloaded []map[string]any, assetsDir string) bool {
	if len(downloaded) == 0 {
		return false
	}
	meta := platformMetadata(out)
	changed := false
	for _, item := range downloaded {
		mediaType, _ := item["type"].(string)
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
		if mediaType != "video" && mediaType != "audio" {
			continue
		}
		local, _ := item["local"].(string)
		if strings.TrimSpace(local) == "" {
			continue
		}
		path := filepath.Join(assetsDir, filepath.Base(local))
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
		meta["asr_media_local"] = local
		out["platform_metadata"] = meta
		return true
	}
	out["platform_metadata"] = meta
	return changed
}

func downloadedVideoPaths(downloaded []map[string]any, assetsDir string) []string {
	paths := make([]string, 0, len(downloaded))
	for _, item := range downloaded {
		mediaType, _ := item["type"].(string)
		if strings.ToLower(strings.TrimSpace(mediaType)) != "video" {
			continue
		}
		local, _ := item["local"].(string)
		if strings.TrimSpace(local) == "" {
			continue
		}
		paths = append(paths, filepath.Join(assetsDir, filepath.Base(local)))
	}
	return paths
}

func platformMetadata(out map[string]any) map[string]any {
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	return meta
}

func pickHTMLBody(out map[string]any) string {
	if html, ok := out["html"].(string); ok && strings.TrimSpace(html) != "" {
		return html
	}
	title, _ := out["title"].(string)
	text, _ := out["text"].(string)
	if text == "" {
		return ""
	}
	return fmt.Sprintf("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><pre>%s</pre></body></html>",
		escapeHTML(title), escapeHTML(text))
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
