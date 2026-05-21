package platform

import "strings"

// ApplyFormats 按 format 列表裁剪输出字段；空 format 表示返回全部文本字段。
func ApplyFormats(c *Content, formats []string) map[string]any {
	if c == nil {
		return nil
	}
	out := c.ToMap()
	formats = normalizeFormats(formats)
	if len(formats) == 0 {
		return out
	}
	if !containsFormat(formats, "text") {
		delete(out, "text")
	}
	if !containsFormat(formats, "markdown") {
		delete(out, "markdown")
	}
	if !containsFormat(formats, "html") {
		delete(out, "html")
	}
	return out
}

// ToMap 将 Content 转为 gateway 输出 map。
func (c *Content) ToMap() map[string]any {
	out := map[string]any{
		"platform":     c.Platform,
		"content_type": c.ContentType,
		"source_url":   c.SourceURL,
		"captured_at":  c.CapturedAt,
	}
	if c.CanonicalURL != "" {
		out["canonical_url"] = c.CanonicalURL
	}
	if c.Title != "" {
		out["title"] = c.Title
	}
	if c.Text != "" {
		out["text"] = c.Text
	}
	if c.Markdown != "" {
		out["markdown"] = c.Markdown
	}
	if c.HTML != "" {
		out["html"] = c.HTML
	}
	if c.Author != nil {
		out["author"] = c.Author
	}
	if c.PublishedAt != "" {
		out["published_at"] = c.PublishedAt
	}
	if len(c.Media) > 0 {
		out["media"] = c.Media
	}
	if c.Engagement != nil {
		out["engagement"] = c.Engagement
	}
	if len(c.Tags) > 0 {
		out["tags"] = c.Tags
	}
	if len(c.PlatformMetadata) > 0 {
		out["platform_metadata"] = c.PlatformMetadata
	}
	return out
}

func normalizeFormats(formats []string) []string {
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		f = strings.ToLower(strings.TrimSpace(f))
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func containsFormat(formats []string, want string) bool {
	for _, f := range formats {
		if f == want {
			return true
		}
	}
	return false
}
