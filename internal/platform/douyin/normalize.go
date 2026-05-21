package douyin

import (
	"strings"

	"github.com/originaleric/diggate/internal/browser"
	"github.com/originaleric/diggate/internal/platform"
)

func normalizeVideo(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = resp.URL
	}
	kind := ContentType
	if norm != nil && norm.ContentKind != "" {
		kind = norm.ContentKind
	}

	text := resp.Text
	if resp.Metadata != nil {
		if desc := strings.TrimSpace(platform.MetaString(resp.Metadata, "description")); desc != "" && text == "" {
			text = desc
		}
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      kind,
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
		if norm.ResolvedURL != "" {
			c.PlatformMetadata["resolved_url"] = norm.ResolvedURL
		}
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
			c.Media = append(c.Media, platform.MediaItem{Type: "video", URL: canonical})
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}
