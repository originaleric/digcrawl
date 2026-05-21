package douyin

import (
	"strings"

	"github.com/originaleric/diggate/internal/platform"
	"github.com/originaleric/diggate/internal/browser"
)

func normalizeVideo(in platform.ReadInput, resp *browser.BrowserBrowseResponse) *platform.Content {
	text := resp.Text
	if resp.Metadata != nil {
		if desc := strings.TrimSpace(platform.MetaString(resp.Metadata, "description")); desc != "" && text == "" {
			text = desc
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
		if t := strings.TrimSpace(platform.MetaString(resp.Metadata, "title")); t != "" {
			c.Title = t
		}
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		c.Engagement = platform.EngagementFromMeta(resp.Metadata)
		if in.IncludeMedia {
			if cover := platform.MetaString(resp.Metadata, "cover_url"); cover != "" {
				c.Media = []platform.MediaItem{{Type: "image", URL: cover}}
			}
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}
