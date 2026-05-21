package xiaohongshu

import (
	"strings"

	"github.com/originaleric/diggate/internal/platform"
	"github.com/originaleric/diggate/internal/browser"
)

func normalizeNote(in platform.ReadInput, resp *browser.BrowserBrowseResponse) *platform.Content {
	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        resp.URL,
		CanonicalURL:     resp.URL,
		Title:            resp.Title,
		Text:             resp.Text,
		Markdown:         resp.Markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: resp.Metadata,
	}
	if resp.Metadata != nil {
		if t := strings.TrimSpace(platform.MetaString(resp.Metadata, "title")); t != "" {
			c.Title = t
		}
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		c.Engagement = platform.EngagementFromMeta(resp.Metadata)
		c.Tags = platform.MetaStringSlice(resp.Metadata, "tags")
		if in.IncludeMedia {
			c.Media = platform.MediaFromMeta(resp.Metadata)
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}
