package toutiao

import (
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func normalizeArticle(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = resp.URL
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
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
			c.PlatformMetadata["article_id"] = norm.PlatformID
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
		if pub := strings.TrimSpace(platform.MetaString(resp.Metadata, "published_at")); pub != "" {
			c.PublishedAt = pub
		}
		if in.IncludeMedia {
			c.Media = platform.MediaFromMeta(resp.Metadata)
		}
		if src := strings.TrimSpace(platform.MetaString(resp.Metadata, "extraction_source")); src != "" {
			c.PlatformMetadata["extraction_source"] = src
		}
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
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
