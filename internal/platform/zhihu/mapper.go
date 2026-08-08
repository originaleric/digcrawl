package zhihu

import (
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func normalizeContent(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
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
	contentType := strings.TrimSpace(norm.ContentKind)
	if contentType == "" || contentType == "question" {
		contentType = "article"
	}
	c := &platform.Content{Platform: PlatformName, ContentType: contentType, SourceURL: norm.OriginalURL, CanonicalURL: canonical, Title: title, Text: text, Markdown: markdown, CapturedAt: platform.NowRFC3339(), PlatformMetadata: meta}
	if norm.PlatformID != "" {
		c.PlatformMetadata["platform_id"] = norm.PlatformID
	}
	c.Author = platform.AuthorFromMeta(meta)
	if pub := platform.MetaString(meta, "published_at"); pub != "" {
		c.PublishedAt = pub
	}
	if in.IncludeMedia {
		c.Media = platform.MediaFromMeta(meta)
		videoURLs := platform.MetaStringSlice(meta, "video_urls")
		if resp != nil && len(resp.MediaURLs) > 0 {
			videoURLs = resp.MediaURLs
		}
		for _, u := range videoURLs {
			c.Media = append(c.Media, platform.MediaItem{Type: "video", URL: u})
		}
	}
	if in.IncludeScreenshot && resp != nil {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}
