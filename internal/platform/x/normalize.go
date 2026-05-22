package x

import (
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func normalizePost(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = resp.URL
	}

	text := resp.Text
	if resp.Metadata != nil {
		if t := strings.TrimSpace(platform.MetaString(resp.Metadata, "text")); t != "" {
			text = t
		}
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            resp.Title,
		Text:             text,
		Markdown:         resp.Markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: resp.Metadata,
	}
	if norm != nil {
		c.PlatformMetadata = platform.EnsureMeta(c.PlatformMetadata)
		if norm.PlatformID != "" {
			c.PlatformMetadata["platform_id"] = norm.PlatformID
		}
		if len(norm.RiskFlags) > 0 {
			c.PlatformMetadata["risk_flags"] = norm.RiskFlags
		}
	}
	if resp.Metadata != nil {
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		c.Engagement = platform.EngagementFromMeta(resp.Metadata)
		c.PublishedAt = platform.MetaString(resp.Metadata, "published_at")
		if in.IncludeMedia {
			c.Media = platform.MediaFromMeta(resp.Metadata)
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}
