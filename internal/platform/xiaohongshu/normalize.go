package xiaohongshu

import (
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func normalizeNote(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = resp.URL
	}
	contentKind := ContentType
	if norm != nil && norm.ContentKind != "" {
		contentKind = norm.ContentKind
	}
	if resp.Metadata != nil {
		if k := strings.TrimSpace(platform.MetaString(resp.Metadata, "content_kind")); k != "" {
			contentKind = detectContentKind("", k)
		}
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      contentKind,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            resp.Title,
		Text:             resp.Text,
		Markdown:         resp.Markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: cloneMeta(resp.Metadata),
	}
	if norm != nil {
		c.PlatformMetadata = platform.EnsureMeta(c.PlatformMetadata)
		if norm.PlatformID != "" {
			c.PlatformMetadata["platform_id"] = norm.PlatformID
		}
		if len(norm.RiskFlags) > 0 {
			c.PlatformMetadata["risk_flags"] = norm.RiskFlags
		}
		if norm.ResolvedURL != "" {
			c.PlatformMetadata["resolved_url"] = norm.ResolvedURL
		}
	}

	if resp.Metadata != nil {
		if t := strings.TrimSpace(platform.MetaString(resp.Metadata, "title")); t != "" {
			c.Title = t
		}
		if txt := strings.TrimSpace(platform.MetaString(resp.Metadata, "text")); txt != "" {
			c.Text = txt
		}
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		c.Engagement = platform.EngagementFromMeta(resp.Metadata)
		c.Tags = platform.MetaStringSlice(resp.Metadata, "tags")
		if in.IncludeMedia {
			c.Media = mediaFromMeta(resp.Metadata, resp.MediaURLs)
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}

func mediaFromMeta(meta map[string]any, browserURLs []string) []platform.MediaItem {
	items := platform.MediaFromMeta(meta)
	videoURLs := browserURLs
	if len(videoURLs) == 0 {
		videoURLs = []string{strings.TrimSpace(platform.MetaString(meta, "video_url"))}
	}
	for _, videoURL := range videoURLs {
		if videoURL != "" {
			items = append(items, platform.MediaItem{Type: "video", URL: videoURL})
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func cloneMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}
