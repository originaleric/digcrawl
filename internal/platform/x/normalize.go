package x

import (
	"strings"

	"github.com/originaleric/diggate/internal/platform"
	"github.com/originaleric/diggate/internal/browser"
)

func normalizePost(in platform.ReadInput, resp *browser.BrowserBrowseResponse) *platform.Content {
	text := resp.Text
	if resp.Metadata != nil {
		if t := strings.TrimSpace(platform.MetaString(resp.Metadata, "text")); t != "" {
			text = t
		}
	}
	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        resp.URL,
		CanonicalURL:     resp.URL,
		Title:            resp.Title,
		Text:             text,
		Markdown:         resp.Markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: resp.Metadata,
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
