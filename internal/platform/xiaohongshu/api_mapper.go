package xiaohongshu

import (
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

func mapNoteCard(in platform.ReadInput, norm *platform.NormalizedURL, card map[string]any) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = fmt.Sprintf("https://www.xiaohongshu.com/explore/%s", norm.PlatformID)
	}

	title := platformMetaString(card, "title", "display_title")
	desc := platformMetaString(card, "desc", "description")
	user, _ := card["user"].(map[string]any)
	interact, _ := card["interact_info"].(map[string]any)
	if interact == nil {
		interact, _ = card["interactInfo"].(map[string]any)
	}

	contentKind := ContentType
	if t := strings.ToLower(platformMetaString(card, "type", "note_type")); strings.Contains(t, "video") {
		contentKind = "video"
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
		ContentType:      contentKind,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            title,
		Text:             desc,
		Markdown:         desc,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
		Tags:             extractTagNames(card),
	}
	if user != nil {
		c.Author = &platform.Author{
			ID:   platformMetaString(user, "user_id", "userId", "id"),
			Name: platformMetaString(user, "nickname", "name"),
		}
	}
	if interact != nil {
		c.Engagement = &platform.Engagement{
			Likes:     platformMetaInt(interact, "liked_count", "likedCount"),
			Comments:  platformMetaInt(interact, "comment_count", "commentCount"),
			Bookmarks: platformMetaInt(interact, "collected_count", "collectedCount"),
		}
	}
	if in.IncludeMedia {
		c.Media = mediaFromNoteCard(card)
	}
	return c
}

func platformMetaString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := platform.MetaString(m, k); v != "" {
			return v
		}
	}
	return ""
}

func platformMetaInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if platform.MetaString(m, k) != "" {
			return metaIntAny(m, k)
		}
	}
	return 0
}

func metaIntAny(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return 0
	}
}

func extractTagNames(card map[string]any) []string {
	raw, ok := card["tag_list"].([]any)
	if !ok {
		raw, _ = card["tagList"].([]any)
	}
	var tags []string
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name := platformMetaString(m, "name"); name != "" {
			tags = append(tags, name)
		}
	}
	return tags
}

func mediaFromNoteCard(card map[string]any) []platform.MediaItem {
	var items []platform.MediaItem
	if imgs, ok := card["image_list"].([]any); ok {
		for _, img := range imgs {
			m, ok := img.(map[string]any)
			if !ok {
				continue
			}
			u := platformMetaString(m, "url_default", "urlDefault", "url")
			if u != "" {
				items = append(items, platform.MediaItem{Type: "image", URL: u})
			}
		}
	}
	if video, ok := card["video"].(map[string]any); ok {
		if media, ok := video["media"].(map[string]any); ok {
			if stream, ok := media["stream"].(map[string]any); ok {
				if h264, ok := stream["h264"].([]any); ok && len(h264) > 0 {
					if first, ok := h264[0].(map[string]any); ok {
						if u := platformMetaString(first, "master_url", "masterUrl"); u != "" {
							items = append(items, platform.MediaItem{Type: "video", URL: u})
						}
					}
				}
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}
