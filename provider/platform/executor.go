package platformprov

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/youtube"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
	"github.com/originaleric/digcrawl/runtime/policy"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/skill"
)

type platformReadInput struct {
	URL               string   `json:"url"`
	Format            []string `json:"format,omitempty"`
	IncludeMedia      bool     `json:"include_media,omitempty"`
	IncludeScreenshot bool     `json:"include_screenshot,omitempty"`
	IncludeOCR        bool     `json:"include_ocr,omitempty"`
	IncludeASR        bool     `json:"include_asr,omitempty"`
	IncludeSummary    bool     `json:"include_summary,omitempty"`
	WaitSelector      string   `json:"wait_selector,omitempty"`
	UseCookieDomain   string   `json:"use_cookie_domain,omitempty"`
}

func (in platformReadInput) toPlatformReadInput() iplatform.ReadInput {
	return iplatform.ReadInput{
		URL:    in.URL,
		Format: in.Format,
		// Direct OCR resolves its required media before this conversion. Keeping
		// IncludeOCR out of this low-level transport shape prevents a Collect
		// caller from accidentally inheriting the Skill's direct-read policy.
		IncludeMedia:      in.IncludeMedia || in.IncludeASR || in.IncludeSummary,
		IncludeScreenshot: in.IncludeScreenshot,
		WaitSelector:      in.WaitSelector,
		UseCookieDomain:   in.UseCookieDomain,
	}
}

func platformOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platform":               map[string]string{"type": "string"},
			"content_type":           map[string]string{"type": "string"},
			"source_url":             map[string]string{"type": "string"},
			"canonical_url":          map[string]string{"type": "string"},
			"title":                  map[string]string{"type": "string"},
			"text":                   map[string]string{"type": "string"},
			"markdown":               map[string]string{"type": "string"},
			"author":                 map[string]string{"type": "object"},
			"published_at":           map[string]string{"type": "string"},
			"captured_at":            map[string]string{"type": "string"},
			"media":                  map[string]any{"type": "array"},
			"engagement":             map[string]string{"type": "object"},
			"tags":                   map[string]any{"type": "array"},
			"platform_metadata":      map[string]string{"type": "object"},
			"screenshot_artifact_id": map[string]string{"type": "string"},
			"asr_text":               map[string]string{"type": "string"},
			"summary":                map[string]string{"type": "object"},
		},
	}
}

func platformInputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"url"},
		"properties": map[string]any{
			"url":                map[string]string{"type": "string"},
			"format":             map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
			"include_media":      map[string]any{"type": "boolean"},
			"include_screenshot": map[string]any{"type": "boolean"},
			"include_ocr":        map[string]any{"type": "boolean"},
			"include_asr":        map[string]any{"type": "boolean"},
			"include_summary":    map[string]any{"type": "boolean"},
			"wait_selector":      map[string]string{"type": "string"},
			"use_cookie_domain":  map[string]string{"type": "string"},
		},
	}
}

type platformReader func(ctx context.Context, in iplatform.ReadInput) (*iplatform.Content, error)

var acquireYouTubeVideo = youtube.DownloadVideo

func platformReadEntry(
	name, description string,
	capabilities []string,
	defaultDomains []string,
	configDomains []string,
	artStore artifact.Store,
	read platformReader,
) registry.Entry {
	return platformReadEntryWithConfig(name, description, capabilities, defaultDomains, configDomains, artStore, read, nil)
}

func platformReadEntryWithConfig(
	name, description string,
	capabilities []string,
	defaultDomains []string,
	configDomains []string,
	artStore artifact.Store,
	read platformReader,
	runtimeCfg *config.Config,
) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         name,
			Description:  description,
			InputSchema:  registry.MustSchema(platformInputSchema()),
			OutputSchema: registry.MustSchema(platformOutputSchema()),
			Capabilities: capabilities,
			Risk:         "network",
		},
		Handler: platformReadHandlerNamedWithConfig(name, defaultDomains, configDomains, artStore, read, runtimeCfg),
	}
}

func optionalRuntimeConfig(configs []*config.Config) *config.Config {
	if len(configs) == 0 {
		return nil
	}
	return configs[0]
}

func platformReadHandler(
	defaultDomains, configDomains []string,
	artStore artifact.Store,
	read platformReader,
) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	return platformReadHandlerNamed("", defaultDomains, configDomains, artStore, read)
}

func platformReadHandlerNamed(
	name string,
	defaultDomains, configDomains []string,
	artStore artifact.Store,
	read platformReader,
) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	return platformReadHandlerNamedWithConfig(name, defaultDomains, configDomains, artStore, read, nil)
}

func platformReadHandlerNamedWithConfig(
	name string,
	defaultDomains, configDomains []string,
	artStore artifact.Store,
	read platformReader,
	runtimeCfg *config.Config,
) func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
	if runtimeCfg == nil {
		runtimeCfg = config.Get()
	}
	return func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
		in, err := executor.DecodeInput[platformReadInput](call)
		if err != nil {
			return nil, nil, err
		}
		if err := validatePlatformURL(in.URL, call, configDomains, defaultDomains); err != nil {
			return nil, nil, err
		}
		if err := executor.ValidateCookieDomain(in.UseCookieDomain, call, configDomains); err != nil {
			return nil, nil, err
		}
		isCollect := executor.IsCollectExecution(ctx)
		skillCfg, videoDownloadCfg, imageDownloadCfg := activeSkillMediaRetentionConfig(runtimeCfg)
		retainProviderVideos := shouldRetainProviderVideos(videoDownloadCfg, skillCfg, name)
		retainProviderImages := shouldRetainProviderImages(imageDownloadCfg, name)
		ocrRequested := skill.DirectReadOCRRequested(ctx, skillCfg, in.IncludeOCR)
		// Retention needs media internally, but must not broaden the response
		// contract for a caller that did not ask for media or an enhancement
		// that already requires media.
		returnMedia := in.IncludeMedia || in.IncludeASR || in.IncludeSummary
		if !isCollect && in.IncludeOCR {
			returnMedia = true
		}
		if ocrRequested || retainProviderVideos || retainProviderImages {
			// A provider may only expose media when this flag is set. The explicit
			// local retention policy, and configured OCR, need media internally.
			// returnMedia above remains the public response contract.
			in.IncludeMedia = true
		}
		// Keep downstream provider acquisition and enhancement decisions on the
		// same resolved OCR policy. returnMedia was captured before this mutation.
		in.IncludeOCR = ocrRequested

		content, err := read(ctx, in.toPlatformReadInput())
		if err != nil {
			var re *iplatform.ReadError
			if errors.As(err, &re) {
				return iplatform.ErrorOutput(re), nil, re
			}
			return nil, nil, err
		}
		var artifacts []protocol.Artifact
		var localVideoPaths []string
		var localImages []skill.LocalImageInput
		youtubeVideoNeeded := name == ToolYouTubeVideoRead && (shouldDownloadYouTubeMediaWithVideoDownload(runtimeCfg, videoDownloadCfg, skillCfg, isCollect, in) || retainProviderVideos)
		if youtubeVideoNeeded {
			legacyCollectArtifact := isCollect && shouldDownloadYouTubeMediaWithVideoDownload(runtimeCfg, videoDownloadCfg, skillCfg, isCollect, in) && !retainProviderVideos
			if legacyCollectArtifact && artStore == nil {
				return nil, nil, fmt.Errorf("YouTube media delivery requires an enabled artifact store")
			}
			downloadCtx, cancelDownload := providerVideoDownloadContext(runtimeCfg, videoDownloadCfg, skillCfg, ctx, isCollect, retainProviderVideos)
			defer cancelDownload()
			retainDir := skillCfg.Media.Dir
			if retainProviderVideos && videoDownloadCfg.Providers != nil {
				retainDir = videoDownloadCfg.DirOrDefault()
			}
			videoPath, cleanupVideo, downloadErr := acquireYouTubeVideo(downloadCtx, in.URL, providerVideoDownloadMaxBytes(runtimeCfg, videoDownloadCfg, skillCfg, isCollect, retainProviderVideos), retainProviderVideos, retainDir)
			if downloadErr != nil {
				if retainProviderVideos {
					content.PlatformMetadata = iplatform.EnsureMeta(content.PlatformMetadata)
					retainErr := providerVideoDownloadFailure(content.PlatformMetadata, 1, retainedVideoFailureReason(downloadErr.Error()))
					out := iplatform.ApplyFormats(content, in.Format)
					if !returnMedia && !isCollect {
						delete(out, "media")
					}
					return out, artifacts, retainErr
				}
				return nil, nil, downloadErr
			}
			defer cleanupVideo()
			if legacyCollectArtifact {
				art, handoffErr := persistCollectedYouTubeVideo(ctx, artStore, videoPath, providerVideoDownloadMaxBytes(runtimeCfg, videoDownloadCfg, skillCfg, true, false))
				if handoffErr != nil {
					return nil, nil, fmt.Errorf("persist downloaded YouTube video: %w", handoffErr)
				}
				content.Media = append(content.Media, iplatform.MediaItem{Type: "video", ArtifactID: art.ID})
			}
			if !isCollect && (in.IncludeOCR || in.IncludeASR || in.IncludeSummary) {
				localVideoPaths = append(localVideoPaths, videoPath)
			}
			content.PlatformMetadata = iplatform.EnsureMeta(content.PlatformMetadata)
			content.PlatformMetadata["media_extraction_source"] = "youtube_yt_dlp"
			if retainProviderVideos {
				relativePath, err := retainedMediaRelativePath(retainDir, videoPath)
				if err != nil {
					retainErr := providerVideoDownloadFailure(content.PlatformMetadata, 1, "local_storage_failed")
					out := iplatform.ApplyFormats(content, in.Format)
					if !returnMedia && !isCollect {
						delete(out, "media")
					}
					return out, artifacts, retainErr
				}
				content.PlatformMetadata["downloaded_video_retained"] = true
				content.PlatformMetadata["retained_media_relative_path"] = relativePath
				content.PlatformMetadata["retained_media_relative_paths"] = []string{relativePath}
				content.PlatformMetadata["video_download_count"] = 1
				content.PlatformMetadata["video_download_attempted_count"] = 1
				content.PlatformMetadata["video_download_status"] = "saved"
			}
		}
		skillEnhancement := ocrRequested || in.IncludeASR || in.IncludeSummary
		out := iplatform.ApplyFormats(content, in.Format)
		if !returnMedia && !isCollect {
			// OCR needs resolved image references as process input, but an enabled
			// local OCR profile must not silently change a text-only MCP response.
			defer delete(out, "media")
		}
		if retainProviderVideos && name != ToolYouTubeVideoRead {
			paths, retainErr := retainResolvedProviderVideos(ctx, out, videoDownloadCfg, artStore)
			if retainErr != nil {
				if !returnMedia && !isCollect {
					delete(out, "media")
				}
				return out, artifacts, retainErr
			}
			if !isCollect && len(paths) > 0 {
				localVideoPaths = append(localVideoPaths, paths...)
			}
		}
		if retainProviderImages {
			inputs, retainErr := RetainResolvedProviderImages(ctx, out, imageDownloadCfg, artStore)
			if retainErr != nil {
				if !returnMedia && !isCollect {
					delete(out, "media")
				}
				return out, artifacts, retainErr
			}
			if !isCollect {
				localImages = append(localImages, inputs...)
			}
		}
		if !isCollect && skillEnhancement {
			if err := skill.Enhance(ctx, out, skillCfg, skill.EnhanceOptions{
				IncludeOCR: ocrRequested, IncludeASR: in.IncludeASR,
				IncludeSummary: in.IncludeSummary, SourceText: content.Text, ScreenshotBase64: content.ScreenshotBase64, ArtifactStore: artStore, LocalVideoPaths: localVideoPaths, LocalImages: localImages,
			}); err != nil {
				// Direct video understanding returns its partial text/metadata only;
				// the provider-owned MP4 is never an output artifact in this path.
				return out, artifacts, err
			}
		}
		if (retainProviderVideos || retainProviderImages) && !returnMedia && !isCollect {
			delete(out, "media")
		}
		if content.ScreenshotBase64 != "" {
			artID := artifact.NewID()
			if artStore == nil {
				return out, nil, fmt.Errorf("screenshot artifact store is unavailable")
			}
			art, err := artifact.PutBase64PNG(ctx, artStore, artID, content.ScreenshotBase64, runtimeCfg.Gateway.ArtifactMaxInlineBytes)
			if err != nil {
				return out, nil, fmt.Errorf("persist screenshot artifact: %w", err)
			}
			artifacts = append(artifacts, art)
			out["screenshot_artifact_id"] = art.ID
		}
		return out, artifacts, nil
	}
}

// retainedMediaRelativePath gives a host enough information to find a video
// the user chose to keep without disclosing the machine-specific media root.
// youtube.DownloadVideo already verifies that retained files are beneath this
// root; this second boundary check keeps that output contract true for every
// provider adapter and focused test injection.
func retainedMediaRelativePath(mediaRoot, path string) (string, error) {
	mediaRoot = strings.TrimSpace(mediaRoot)
	if mediaRoot == "" {
		return "", fmt.Errorf("retained media directory is not configured")
	}
	relativePath, err := filepath.Rel(mediaRoot, path)
	if err != nil {
		return "", err
	}
	if relativePath == "." || filepath.IsAbs(relativePath) || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("retained video is outside the configured media directory")
	}
	return filepath.ToSlash(relativePath), nil
}

// persistCollectedYouTubeVideo is a Collect delivery adapter. It is separate
// from video understanding: ASR/OCR receive a local path directly and never
// observe artifact identifiers or storage lifecycle.
func persistCollectedYouTubeVideo(ctx context.Context, store artifact.Store, path string, maxBytes int64) (protocol.Artifact, error) {
	if store == nil {
		return protocol.Artifact{}, fmt.Errorf("artifact store is unavailable")
	}
	if fileStore, ok := store.(artifact.FileStore); ok {
		return fileStore.PutFile(ctx, artifact.NewID(), "video/mp4", filepath.Base(path), path, maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return protocol.Artifact{}, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return protocol.Artifact{}, fmt.Errorf("video exceeds max bytes %d", maxBytes)
	}
	return store.Put(ctx, artifact.NewID(), "video/mp4", filepath.Base(path), data)
}

func validatePlatformURL(rawURL string, call *protocol.ToolCall, configDomains, defaultDomains []string) error {
	domains := policy.MergeDomains(&call.Policy, configDomains)
	if len(domains) == 0 {
		domains = defaultDomains
	}
	return policy.ValidateURLDomain(rawURL, domains, nil)
}

// activeSkillMediaRetentionConfig takes the local media settings used by a
// direct provider call. Injected configurations are immutable for Collect and
// focused tests.
func activeSkillMediaRetentionConfig(cfg *config.Config) (config.SkillConfig, config.VideoDownloadConfig, config.ImageDownloadConfig) {
	if cfg == config.Get() {
		skillCfg, videoCfg := config.SkillVideoDownload()
		_, imageCfg := config.SkillImageDownload()
		return skillCfg, videoCfg, imageCfg
	}
	return cfg.Skill, cfg.VideoDownload, cfg.ImageDownload
}

func shouldRetainProviderVideos(videoDownloadCfg config.VideoDownloadConfig, skillCfg config.SkillConfig, tool string) bool {
	provider := videoDownloadProviderForTool(tool)
	if provider == "" {
		return false
	}
	// A configured provider map is the source of truth for the new feature.
	// The legacy boolean remains a YouTube-only compatibility path for older
	// profiles that predate VideoDownload.
	if videoDownloadCfg.Providers != nil {
		return videoDownloadCfg.EnabledFor(provider)
	}
	return provider == "youtube" && skillCfg.Media.RetainDownloadedVideo
}

func shouldRetainProviderImages(imageDownloadCfg config.ImageDownloadConfig, tool string) bool {
	provider := imageDownloadProviderForTool(tool)
	return provider != "" && imageDownloadCfg.EnabledFor(provider)
}

func videoDownloadProviderForTool(tool string) string {
	provider, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(tool)), ".")
	if !ok {
		return ""
	}
	if config.IsSupportedVideoDownloadProvider(provider) {
		return provider
	}
	return ""
}

func imageDownloadProviderForTool(tool string) string {
	provider, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(tool)), ".")
	if !ok {
		return ""
	}
	if config.IsSupportedImageDownloadProvider(provider) {
		return provider
	}
	return ""
}

func providerVideoDownloadMaxBytes(cfg *config.Config, videoDownloadCfg config.VideoDownloadConfig, skillCfg config.SkillConfig, isCollect, retain bool) int64 {
	if retain && videoDownloadCfg.Providers != nil {
		return videoDownloadCfg.MaxBytesOrDefault()
	}
	return youtubeDownloadMaxBytes(cfg, skillCfg, isCollect)
}

func providerVideoDownloadContext(cfg *config.Config, videoDownloadCfg config.VideoDownloadConfig, skillCfg config.SkillConfig, ctx context.Context, isCollect, retain bool) (context.Context, context.CancelFunc) {
	if retain && videoDownloadCfg.Providers != nil {
		return context.WithTimeout(ctx, time.Duration(videoDownloadCfg.TimeoutSecOrDefault())*time.Second)
	}
	return youtubeDownloadContext(cfg, skillCfg, ctx, isCollect)
}

// retainResolvedProviderVideos persists only the video references a platform
// reader already returned. It deliberately uses the existing bounded media
// downloader instead of introducing a second downloader contract.
func retainResolvedProviderVideos(ctx context.Context, out map[string]any, cfg config.VideoDownloadConfig, artStore artifact.Store) ([]string, error) {
	meta := outputPlatformMetadata(out)
	video := outputPrimaryVideoMedia(out)
	if video == nil {
		meta["video_download_status"] = "not_available"
		return nil, nil
	}
	root := cfg.DirOrDefault()
	if !filepath.IsAbs(root) {
		return nil, providerVideoDownloadFailure(meta, 0, "local_storage_failed")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, providerVideoDownloadFailure(meta, 0, "local_storage_failed")
	}
	dir, err := os.MkdirTemp(root, "digcrawl-video-*")
	if err != nil {
		return nil, providerVideoDownloadFailure(meta, 0, "local_storage_failed")
	}

	downloadInput := map[string]any{
		"media":         []map[string]any{video},
		"platform":      out["platform"],
		"source_url":    out["source_url"],
		"canonical_url": out["canonical_url"],
	}
	// The HTTP client also observes this deadline, but the operation-level
	// context prevents many individually slow media items from extending one
	// requested provider retention without bound.
	downloadCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSecOrDefault())*time.Second)
	defer cancel()
	files, err := skill.DownloadAssets(downloadCtx, dir, downloadInput, skill.AssetDownloadOptions{
		MaxBytes:      cfg.MaxBytesOrDefault(),
		TimeoutSec:    cfg.TimeoutSecOrDefault(),
		ArtifactStore: artStore,
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, providerVideoDownloadFailure(meta, 1, "local_storage_failed")
	}
	summary, err := readRetainedVideoDownloadSummary(filepath.Join(dir, "media.json"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, providerVideoDownloadFailure(meta, 1, "local_storage_failed")
	}
	// DownloadAssets' manifest is useful while it is assembling the result, but
	// it records expiring source URLs. A retained-video directory is a durable
	// user artifact, so keep only the requested local media there.
	if err := os.Remove(filepath.Join(dir, "media.json")); err != nil && !os.IsNotExist(err) {
		_ = os.RemoveAll(dir)
		return nil, providerVideoDownloadFailure(meta, summary.attempted, "local_storage_failed")
	}

	paths := make([]string, 0, len(files))
	relativePaths := make([]string, 0, len(files))
	for _, path := range files {
		if filepath.Base(path) == "media.json" {
			continue
		}
		relativePath, relErr := retainedMediaRelativePath(root, path)
		if relErr != nil {
			_ = os.RemoveAll(dir)
			return nil, providerVideoDownloadFailure(meta, summary.attempted, "local_storage_failed")
		}
		paths = append(paths, path)
		relativePaths = append(relativePaths, relativePath)
	}

	if len(paths) == 0 {
		_ = os.RemoveAll(dir)
		return nil, providerVideoDownloadFailure(meta, summary.attempted, summary.failureReason)
	}
	meta["downloaded_video_retained"] = true
	meta["retained_media_relative_paths"] = relativePaths
	if len(relativePaths) == 1 {
		meta["retained_media_relative_path"] = relativePaths[0]
	}
	meta["video_download_count"] = len(relativePaths)
	meta["video_download_attempted_count"] = summary.attempted
	if summary.failureReason != "" {
		meta["video_download_status"] = "partial"
		meta["video_download_failure_reason"] = summary.failureReason
	} else {
		meta["video_download_status"] = "saved"
	}
	return paths, nil
}

// retainResolvedProviderImages mirrors the explicit video-retention boundary
// for the complete image set of one content item. It intentionally has no
// partial state: either every selected remote image is retained or the whole
// private directory is removed.
func RetainResolvedProviderImages(ctx context.Context, out map[string]any, cfg config.ImageDownloadConfig, artStore artifact.Store) ([]skill.LocalImageInput, error) {
	meta := outputPlatformMetadata(out)
	candidates := outputImageMedia(out)
	if len(candidates) == 0 {
		meta["image_download_status"] = "not_available"
		return nil, nil
	}
	if len(candidates) > cfg.MaxItemsOrDefault() {
		return nil, providerImageDownloadFailure(meta, 0, "item_limit_exceeded")
	}
	root := cfg.DirOrDefault()
	if !filepath.IsAbs(root) || os.MkdirAll(root, 0o750) != nil {
		return nil, providerImageDownloadFailure(meta, 0, "local_storage_failed")
	}
	dir, err := os.MkdirTemp(root, "digcrawl-image-*")
	if err != nil {
		return nil, providerImageDownloadFailure(meta, 0, "local_storage_failed")
	}
	fail := func(attempted int, reason string) ([]skill.LocalImageInput, error) {
		_ = os.RemoveAll(dir)
		return nil, providerImageDownloadFailure(meta, attempted, reason)
	}
	downloadInput := map[string]any{"media": candidates, "platform": out["platform"], "source_url": out["source_url"], "canonical_url": out["canonical_url"]}
	downloadCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSecOrDefault())*time.Second)
	defer cancel()
	if _, err := skill.DownloadAssets(downloadCtx, dir, downloadInput, skill.AssetDownloadOptions{MaxBytes: cfg.MaxBytesOrDefault(), TimeoutSec: cfg.TimeoutSecOrDefault(), ArtifactStore: artStore, ImagesOnly: true}); err != nil {
		return fail(len(candidates), "local_storage_failed")
	}
	summary, err := readRetainedImageDownloadSummary(filepath.Join(dir, "media.json"))
	if err != nil {
		return fail(len(candidates), "local_storage_failed")
	}
	if summary.attempted != len(candidates) || summary.saved != len(candidates) || summary.failureReason != "" {
		return fail(summary.attempted, firstNonEmpty(summary.failureReason, "download_failed"))
	}
	paths := make([]string, 0, len(summary.savedImages))
	relativePaths := make([]string, 0, len(summary.savedImages))
	inputs := make([]skill.LocalImageInput, 0, len(summary.savedImages))
	for _, saved := range summary.savedImages {
		if filepath.Base(saved.local) != saved.local || filepath.IsAbs(saved.local) {
			return fail(summary.attempted, "local_storage_failed")
		}
		path := filepath.Join(dir, saved.local)
		if !isRetainedImageFile(path) {
			return fail(summary.attempted, "download_failed")
		}
		relativePath, relErr := retainedMediaRelativePath(root, path)
		if relErr != nil {
			return fail(summary.attempted, "local_storage_failed")
		}
		paths = append(paths, path)
		relativePaths = append(relativePaths, relativePath)
		inputs = append(inputs, skill.LocalImageInput{Path: path, SourceURL: saved.url, Type: saved.mediaType})
	}
	if err := os.Remove(filepath.Join(dir, "media.json")); err != nil && !os.IsNotExist(err) {
		return fail(summary.attempted, "local_storage_failed")
	}
	meta["retained_image_relative_paths"] = relativePaths
	if len(relativePaths) == 1 {
		meta["retained_image_relative_path"] = relativePaths[0]
	}
	meta["image_download_count"] = len(paths)
	meta["image_download_attempted_count"] = summary.attempted
	meta["image_download_status"] = "saved"
	return inputs, nil
}

func outputImageMedia(out map[string]any) []map[string]any {
	if out == nil || out["media"] == nil {
		return nil
	}
	data, err := json.Marshal(out["media"])
	if err != nil {
		return nil
	}
	var media []map[string]any
	if json.Unmarshal(data, &media) != nil {
		return nil
	}
	seen := map[string]struct{}{}
	images := make([]map[string]any, 0, len(media))
	for _, item := range media {
		mediaType := strings.ToLower(strings.TrimSpace(fmt.Sprint(item["type"])))
		if mediaType != "image" && mediaType != "photo" {
			continue
		}
		rawURL, _ := item["url"].(string)
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		if _, ok := seen[rawURL]; ok {
			continue
		}
		seen[rawURL] = struct{}{}
		images = append(images, map[string]any{"type": mediaType, "url": rawURL})
	}
	return images
}

type retainedImageDownload struct{ mediaType, url, local string }
type retainedImageDownloadSummary struct {
	attempted, saved int
	failureReason    string
	savedImages      []retainedImageDownload
}

func readRetainedImageDownloadSummary(path string) (retainedImageDownloadSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return retainedImageDownloadSummary{}, err
	}
	var manifest struct {
		Downloaded []map[string]any `json:"downloaded"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return retainedImageDownloadSummary{}, err
	}
	summary := retainedImageDownloadSummary{}
	for _, item := range manifest.Downloaded {
		mediaType := strings.ToLower(strings.TrimSpace(fmt.Sprint(item["type"])))
		if mediaType != "image" && mediaType != "photo" {
			continue
		}
		summary.attempted++
		local, _ := item["local"].(string)
		url, _ := item["url"].(string)
		if local = strings.TrimSpace(local); local != "" {
			summary.saved++
			summary.savedImages = append(summary.savedImages, retainedImageDownload{mediaType: mediaType, url: strings.TrimSpace(url), local: local})
			continue
		}
		if raw, _ := item["error"].(string); strings.TrimSpace(raw) != "" && summary.failureReason == "" {
			summary.failureReason = retainedImageFailureReason(raw)
		}
	}
	if summary.saved == 0 && summary.failureReason == "" {
		summary.failureReason = "download_failed"
	}
	return summary, nil
}

func isRetainedImageFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(buf[:n]), ";")[0])) {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp":
		return true
	default:
		return isAVIFFile(buf[:n])
	}
}

// isAVIFFile recognizes the ISO-BMFF ftyp brands used by AVIF. net/http's
// content sniffer does not recognize AVIF, so retention must inspect the
// downloaded file rather than treating a valid image as an unknown binary.
func isAVIFFile(data []byte) bool {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return false
	}
	isAVIFBrand := func(brand []byte) bool {
		return string(brand) == "avif" || string(brand) == "avis"
	}
	if isAVIFBrand(data[8:12]) {
		return true
	}
	for offset := 16; offset+4 <= len(data); offset += 4 {
		if isAVIFBrand(data[offset : offset+4]) {
			return true
		}
	}
	return false
}

func retainedImageFailureReason(raw string) string {
	reason := retainedVideoFailureReason(raw)
	if reason == "not_single_file_video" {
		return "download_failed"
	}
	return reason
}

func providerImageDownloadFailure(meta map[string]any, attempted int, reason string) error {
	if attempted < 0 {
		attempted = 0
	}
	switch reason {
	case "item_limit_exceeded", "download_failed", "local_storage_failed", "size_limit_exceeded", "timeout", "rate_limited", "invalid_media_url", "http_client_error", "http_server_error":
	default:
		reason = "download_failed"
	}
	meta["image_download_status"] = "failed"
	meta["image_download_count"] = 0
	meta["image_download_attempted_count"] = attempted
	meta["image_download_failure_reason"] = reason
	return &providerImageDownloadError{reason: reason}
}

type providerImageDownloadError struct{ reason string }

func (e *providerImageDownloadError) Error() string {
	return fmt.Sprintf("provider image download failed: %s", e.reason)
}
func (e *providerImageDownloadError) ToolErrorCode() string    { return ProviderImageDownloadFailedCode }
func (e *providerImageDownloadError) ToolErrorMessage() string { return e.Error() }

// providerVideoDownloadFailure is the only failure boundary for explicit
// provider retention. Downloader and filesystem errors can contain signed
// URLs or machine-local paths, so callers receive only fixed reason codes.
func providerVideoDownloadFailure(meta map[string]any, attempted int, reason string) error {
	if attempted < 0 {
		attempted = 0
	}
	switch reason {
	case "download_failed", "local_storage_failed", "size_limit_exceeded", "timeout", "rate_limited", "invalid_media_url", "not_single_file_video", "http_client_error", "http_server_error":
	default:
		reason = "download_failed"
	}
	meta["video_download_status"] = "failed"
	meta["video_download_count"] = 0
	meta["video_download_attempted_count"] = attempted
	meta["video_download_failure_reason"] = reason
	return &providerVideoDownloadError{reason: reason}
}

// providerVideoDownloadError is the typed boundary for a failed optional
// retention operation. The platform result remains useful, but callers must
// identify this exact error rather than treating every partial tool result as
// exportable content.
type providerVideoDownloadError struct {
	reason string
}

func (e *providerVideoDownloadError) Error() string {
	return fmt.Sprintf("provider video download failed: %s", e.reason)
}

func (e *providerVideoDownloadError) ToolErrorCode() string {
	return ProviderVideoDownloadFailedCode
}

func (e *providerVideoDownloadError) ToolErrorMessage() string {
	return e.Error()
}

// outputPrimaryVideoMedia selects one provider-reported video candidate. A
// platform reader can expose alternate encodings, playlists, and network
// observations for the same content; provider retention is one user request
// to save that content, not a request to archive every rendition. Selecting
// one candidate keeps the existing MaxBytes limit as a complete per-request
// disk bound.
func outputPrimaryVideoMedia(out map[string]any) map[string]any {
	if out == nil || out["media"] == nil {
		return nil
	}
	data, err := json.Marshal(out["media"])
	if err != nil {
		return nil
	}
	var media []map[string]any
	if err := json.Unmarshal(data, &media); err != nil {
		return nil
	}
	for _, item := range media {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["type"])), "video") {
			continue
		}
		artifactID, _ := item["artifact_id"].(string)
		if strings.TrimSpace(artifactID) != "" {
			return item
		}
		rawURL, _ := item["url"].(string)
		if strings.TrimSpace(rawURL) != "" {
			return item
		}
	}
	return nil
}

type retainedVideoDownloadSummary struct {
	attempted     int
	saved         int
	failureReason string
}

func readRetainedVideoDownloadSummary(path string) (retainedVideoDownloadSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return retainedVideoDownloadSummary{}, err
	}
	var manifest struct {
		Downloaded []map[string]any `json:"downloaded"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return retainedVideoDownloadSummary{}, err
	}
	summary := retainedVideoDownloadSummary{}
	for _, item := range manifest.Downloaded {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(item["type"])), "video") {
			continue
		}
		summary.attempted++
		if local, _ := item["local"].(string); strings.TrimSpace(local) != "" {
			summary.saved++
			continue
		}
		if raw, _ := item["error"].(string); strings.TrimSpace(raw) != "" && summary.failureReason == "" {
			summary.failureReason = retainedVideoFailureReason(raw)
		}
	}
	if summary.attempted == 0 {
		summary.attempted = 1
	}
	if summary.saved == 0 && summary.failureReason == "" {
		summary.failureReason = "download_failed"
	}
	return summary, nil
}

func retainedVideoFailureReason(raw string) string {
	reason := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(reason, "exceeds max"), strings.Contains(reason, "exceeds allowed maximum"):
		return "size_limit_exceeded"
	case strings.Contains(reason, "deadline exceeded"), strings.Contains(reason, "client.timeout"):
		return "timeout"
	case strings.Contains(reason, "http 429"):
		return "rate_limited"
	case strings.Contains(reason, "private/local"), strings.Contains(reason, "unsupported remote media scheme"), strings.Contains(reason, "remote media url missing host"), strings.Contains(reason, "userinfo"):
		return "invalid_media_url"
	case strings.Contains(reason, "not a video"), strings.Contains(reason, "not a recognized video file"):
		return "not_single_file_video"
	case strings.Contains(reason, "http 4"):
		return "http_client_error"
	case strings.Contains(reason, "http 5"):
		return "http_server_error"
	default:
		return "download_failed"
	}
}

func outputPlatformMetadata(out map[string]any) map[string]any {
	if out == nil {
		return map[string]any{}
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		out["platform_metadata"] = meta
	}
	return meta
}

func shouldDownloadYouTubeMedia(cfg *config.Config, skillCfg config.SkillConfig, isCollect bool, in platformReadInput) bool {
	return shouldDownloadYouTubeMediaWithVideoDownload(cfg, cfg.VideoDownload, skillCfg, isCollect, in)
}

func shouldDownloadYouTubeMediaWithVideoDownload(cfg *config.Config, videoDownloadCfg config.VideoDownloadConfig, skillCfg config.SkillConfig, isCollect bool, in platformReadInput) bool {
	if !isCollect {
		if !in.IncludeASR && !in.IncludeSummary && !(in.IncludeOCR && skillCfg.OCR.VideoFrames.EnabledOrDefault()) {
			return false
		}
		return skill.ValidateVideoEnhancementRequest(skillCfg, skill.EnhanceOptions{
			IncludeOCR: in.IncludeOCR, IncludeASR: in.IncludeASR, IncludeSummary: in.IncludeSummary,
		}) == nil
	}
	if videoDownloadCfg.EnabledFor("youtube") {
		return false
	}
	return in.IncludeMedia && cfg.Collect.DownloadMedia != nil && *cfg.Collect.DownloadMedia
}

func youtubeDownloadMaxBytes(cfg *config.Config, skillCfg config.SkillConfig, isCollect bool) int64 {
	if isCollect {
		return cfg.Collect.MaxMediaBytes
	}
	if maxBytes := skillCfg.Media.MaxBytes; maxBytes > 0 {
		return maxBytes
	}
	return skill.DefaultSkillMediaMaxBytes
}

func youtubeDownloadContext(cfg *config.Config, skillCfg config.SkillConfig, ctx context.Context, isCollect bool) (context.Context, context.CancelFunc) {
	seconds := cfg.Collect.MediaTimeoutSec
	if !isCollect {
		seconds = skillCfg.Media.TimeoutSecOrDefault()
	}
	if seconds <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}
