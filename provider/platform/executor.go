package platformprov

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		URL:               in.URL,
		Format:            in.Format,
		IncludeMedia:      in.IncludeMedia || in.IncludeOCR || in.IncludeASR || in.IncludeSummary,
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
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:         name,
			Description:  description,
			InputSchema:  registry.MustSchema(platformInputSchema()),
			OutputSchema: registry.MustSchema(platformOutputSchema()),
			Capabilities: capabilities,
			Risk:         "network",
		},
		Handler: platformReadHandlerNamed(name, defaultDomains, configDomains, artStore, read),
	}
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
		isCollect := executor.IsCollectExecution(ctx)
		if name == ToolYouTubeVideoRead && shouldDownloadYouTubeMedia(runtimeCfg, isCollect, in) {
			if isCollect && artStore == nil {
				return nil, nil, fmt.Errorf("YouTube media delivery requires an enabled artifact store")
			}
			downloadCtx, cancelDownload := youtubeDownloadContext(runtimeCfg, ctx, isCollect)
			defer cancelDownload()
			retainVideo := !isCollect && runtimeCfg.Skill.Media.RetainDownloadedVideo
			videoPath, cleanupVideo, downloadErr := acquireYouTubeVideo(downloadCtx, in.URL, youtubeDownloadMaxBytes(runtimeCfg, isCollect), retainVideo, runtimeCfg.Skill.Media.Dir)
			if downloadErr != nil {
				return nil, nil, downloadErr
			}
			defer cleanupVideo()
			if isCollect {
				art, handoffErr := persistCollectedYouTubeVideo(ctx, artStore, videoPath, youtubeDownloadMaxBytes(runtimeCfg, true))
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
			if retainVideo {
				content.PlatformMetadata["downloaded_video_retained"] = true
			}
		}
		skillEnhancement := in.IncludeOCR || in.IncludeASR || in.IncludeSummary
		out := iplatform.ApplyFormats(content, in.Format)
		if !isCollect && skillEnhancement {
			if err := skill.Enhance(ctx, out, runtimeCfg.Skill, skill.EnhanceOptions{
				IncludeOCR: in.IncludeOCR, IncludeASR: in.IncludeASR,
				IncludeSummary: in.IncludeSummary, SourceText: content.Text, ScreenshotBase64: content.ScreenshotBase64, ArtifactStore: artStore, LocalVideoPaths: localVideoPaths,
			}); err != nil {
				// Direct video understanding returns its partial text/metadata only;
				// the provider-owned MP4 is never an output artifact in this path.
				return out, artifacts, err
			}
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

func shouldDownloadYouTubeMedia(cfg *config.Config, isCollect bool, in platformReadInput) bool {
	if !isCollect {
		if !in.IncludeASR && !in.IncludeSummary && !(in.IncludeOCR && cfg.Skill.OCR.VideoFrames.EnabledOrDefault()) {
			return false
		}
		return skill.ValidateVideoEnhancementRequest(cfg.Skill, skill.EnhanceOptions{
			IncludeOCR: in.IncludeOCR, IncludeASR: in.IncludeASR, IncludeSummary: in.IncludeSummary,
		}) == nil
	}
	return in.IncludeMedia && cfg.Collect.DownloadMedia != nil && *cfg.Collect.DownloadMedia
}

func youtubeDownloadMaxBytes(cfg *config.Config, isCollect bool) int64 {
	if isCollect {
		return cfg.Collect.MaxMediaBytes
	}
	if maxBytes := cfg.Skill.Media.MaxBytes; maxBytes > 0 {
		return maxBytes
	}
	return skill.DefaultSkillMediaMaxBytes
}

func youtubeDownloadContext(cfg *config.Config, ctx context.Context, isCollect bool) (context.Context, context.CancelFunc) {
	seconds := cfg.Collect.MediaTimeoutSec
	if !isCollect {
		seconds = cfg.Skill.Media.TimeoutSecOrDefault()
	}
	if seconds <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}
