package kuaishou

import (
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func normalizeVideo(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	meta := map[string]any{}
	title := ""
	text := ""
	markdown := ""
	canonical := norm.CanonicalURL
	if resp != nil {
		meta = resp.Metadata
		title = strings.TrimSpace(resp.Title)
		text = strings.TrimSpace(resp.Text)
		markdown = resp.Markdown
		if canonical == "" {
			canonical = resp.URL
		}
	}
	meta = platform.EnsureMeta(meta)
	if t := platform.MetaString(meta, "title"); t != "" {
		title = t
	}
	if desc := platform.MetaString(meta, "description"); desc != "" {
		text = desc
	}
	c := &platform.Content{Platform: PlatformName, ContentType: ContentType, SourceURL: norm.OriginalURL, CanonicalURL: canonical, Title: title, Text: text, Markdown: markdown, CapturedAt: platform.NowRFC3339(), PlatformMetadata: meta}
	if norm.PlatformID != "" {
		c.PlatformMetadata["platform_id"] = norm.PlatformID
		c.PlatformMetadata["video_id"] = norm.PlatformID
	}
	if in.IncludeMedia {
		c.Media = platform.MediaFromMeta(meta)
		for _, u := range platform.MetaStringSlice(meta, "video_urls") {
			c.Media = append(c.Media, platform.MediaItem{Type: "video", URL: u})
		}
	}
	c.Author = platform.AuthorFromMeta(meta)
	if pub := platform.MetaString(meta, "published_at"); pub != "" {
		c.PublishedAt = pub
	}
	if in.IncludeScreenshot && resp != nil {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}
