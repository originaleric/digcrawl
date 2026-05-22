package toutiao

import (
	"fmt"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

// MapAPIResponse 将头条 API JSON 映射为 Content。
func MapAPIResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed map[string]any) (*platform.Content, error) {
	article := extractAPIArticle(parsed)
	if article == nil {
		return nil, fmt.Errorf("api response missing article payload")
	}
	return mapAPIArticle(in, norm, article), nil
}

func extractAPIArticle(parsed map[string]any) map[string]any {
	if data, ok := parsed["data"].(map[string]any); ok {
		if len(data) > 0 {
			return data
		}
	}
	if info, ok := parsed["article_info"].(map[string]any); ok {
		return info
	}
	if parsed != nil && platformMetaString(parsed, "title", "content") != "" {
		return parsed
	}
	return nil
}

func mapAPIArticle(in platform.ReadInput, norm *platform.NormalizedURL, article map[string]any) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = fmt.Sprintf("https://www.toutiao.com/article/%s/", norm.PlatformID)
	}
	title := platformMetaString(article, "title", "Title")
	text := platformMetaString(article, "content", "abstract", "summary", "text")
	if html := platformMetaString(article, "content_html"); html != "" && text == "" {
		text = stripHTMLTags(html)
	}
	author := platformMetaString(article, "source", "media_name", "author_name")
	if authorObj, ok := article["media_info"].(map[string]any); ok {
		if n := platformMetaString(authorObj, "name", "source"); n != "" {
			author = n
		}
	}
	published := platformMetaString(article, "publish_time", "publishTime", "datetime", "display_time")

	meta := map[string]any{
		"extraction_source":  "api",
		"article_id":         norm.PlatformID,
		"api_fetch_mode":     "browser_context_fetch",
		"api_signature_mode": "page_runtime",
	}
	if len(norm.RiskFlags) > 0 {
		meta["risk_flags"] = norm.RiskFlags
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            title,
		Text:             text,
		Markdown:         text,
		PublishedAt:      published,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if author != "" {
		c.Author = &platform.Author{Name: author}
	}
	if in.IncludeMedia {
		c.Media = mediaFromAPIArticle(article)
	}
	return c
}

func mediaFromAPIArticle(article map[string]any) []platform.MediaItem {
	var items []platform.MediaItem
	if imgs, ok := article["image_list"].([]any); ok {
		for _, img := range imgs {
			switch v := img.(type) {
			case string:
				if v != "" {
					items = append(items, platform.MediaItem{Type: "image", URL: v})
				}
			case map[string]any:
				if u := platformMetaString(v, "url", "src"); u != "" {
					items = append(items, platform.MediaItem{Type: "image", URL: u})
				}
			}
		}
	}
	if u := platformMetaString(article, "cover_image", "thumb_image", "image_url"); u != "" {
		items = append(items, platform.MediaItem{Type: "image", URL: u})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func platformMetaString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			case float64:
				return fmt.Sprintf("%.0f", t)
			}
		}
	}
	return ""
}

func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(b.String())
}
