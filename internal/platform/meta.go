package platform

import (
	"fmt"
	"strconv"
	"strings"
)

// MetaString reads a string field from page metadata JSON.
func MetaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func metaInt(meta map[string]any, key string) int {
	s := MetaString(meta, key)
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(strings.ReplaceAll(s, ",", ""))
	return n
}

// MetaStringSlice reads a string slice from metadata.
func MetaStringSlice(meta map[string]any, key string) []string {
	if meta == nil {
		return nil
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}

// AuthorFromMeta builds Author from metadata fields.
func AuthorFromMeta(meta map[string]any) *Author {
	name := MetaString(meta, "author_name")
	if name == "" {
		name = MetaString(meta, "author")
	}
	if name == "" {
		return nil
	}
	return &Author{
		ID:         MetaString(meta, "author_id"),
		Name:       name,
		ProfileURL: MetaString(meta, "author_profile_url"),
		AvatarURL:  MetaString(meta, "author_avatar_url"),
	}
}

// EngagementFromMeta builds Engagement from metadata fields.
func EngagementFromMeta(meta map[string]any) *Engagement {
	e := &Engagement{
		Likes:     metaInt(meta, "likes"),
		Comments:  metaInt(meta, "comments"),
		Shares:    metaInt(meta, "shares"),
		Reposts:   metaInt(meta, "reposts"),
		Bookmarks: metaInt(meta, "bookmarks"),
	}
	if e.Likes == 0 && e.Comments == 0 && e.Shares == 0 && e.Reposts == 0 && e.Bookmarks == 0 {
		return nil
	}
	return e
}

// MediaFromMeta builds media items from metadata image URLs.
func MediaFromMeta(meta map[string]any) []MediaItem {
	urls := MetaStringSlice(meta, "image_urls")
	if len(urls) == 0 {
		if u := MetaString(meta, "cover_url"); u != "" {
			urls = []string{u}
		}
	}
	if len(urls) == 0 {
		return nil
	}
	items := make([]MediaItem, 0, len(urls))
	for _, u := range urls {
		items = append(items, MediaItem{Type: "image", URL: u})
	}
	return items
}
