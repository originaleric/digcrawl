package douyin

import (
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

func mapAwemeDetail(in platform.ReadInput, norm *platform.NormalizedURL, detail map[string]any) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		kind := norm.ContentKind
		if kind == "" {
			kind = "video"
		}
		canonical = fmt.Sprintf("https://www.douyin.com/%s/%s", kind, norm.PlatformID)
	}

	desc := platformMetaString(detail, "desc", "description")
	title := ""
	if share, ok := detail["share_info"].(map[string]any); ok {
		title = platformMetaString(share, "share_title", "shareTitle")
	}
	if title == "" {
		title = desc
	}

	meta := map[string]any{
		"extraction_source":  "api",
		"platform_id":        norm.PlatformID,
		"api_fetch_mode":     "browser_context_fetch",
		"api_signature_mode": "page_runtime",
	}
	if len(norm.RiskFlags) > 0 {
		meta["risk_flags"] = norm.RiskFlags
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      norm.ContentKind,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            title,
		Text:             desc,
		Markdown:         desc,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if author, ok := detail["author"].(map[string]any); ok {
		c.Author = &platform.Author{
			ID:   platformMetaString(author, "sec_uid", "secUid", "uid"),
			Name: platformMetaString(author, "nickname"),
		}
	}
	if stats, ok := detail["statistics"].(map[string]any); ok {
		c.Engagement = &platform.Engagement{
			Likes:    platformMetaInt(stats, "digg_count", "diggCount"),
			Comments: platformMetaInt(stats, "comment_count", "commentCount"),
			Shares:   platformMetaInt(stats, "share_count", "shareCount"),
		}
	}
	if in.IncludeMedia {
		c.Media = mediaFromAwemeDetail(detail, canonical)
	}
	return c
}

func platformMetaString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := nestedString(v); ok {
				return s
			}
		}
	}
	return ""
}

func nestedString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		return s, s != ""
	case map[string]any:
		for _, k := range []string{"share_title", "shareTitle", "title", "desc"} {
			if s := platform.MetaString(t, k); s != "" {
				return s, true
			}
		}
	}
	return "", false
}

func platformMetaInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case float64:
				return int(t)
			case int:
				return t
			case int64:
				return int(t)
			}
		}
	}
	return 0
}

func mediaFromAwemeDetail(detail map[string]any, canonical string) []platform.MediaItem {
	var items []platform.MediaItem
	if video, ok := detail["video"].(map[string]any); ok {
		if cover, ok := video["cover"].(map[string]any); ok {
			if urls, ok := cover["url_list"].([]any); ok && len(urls) > 0 {
				if u, ok := urls[0].(string); ok && u != "" {
					items = append(items, platform.MediaItem{Type: "image", URL: u})
				}
			}
		}
		if play, ok := video["play_addr"].(map[string]any); ok {
			if urls, ok := play["url_list"].([]any); ok && len(urls) > 0 {
				if u, ok := urls[0].(string); ok && u != "" {
					items = append(items, platform.MediaItem{Type: "video", URL: u})
				}
			}
		}
	}
	if len(items) == 0 && canonical != "" {
		items = append(items, platform.MediaItem{Type: "video", URL: canonical})
	}
	if len(items) == 0 {
		return nil
	}
	return items
}
