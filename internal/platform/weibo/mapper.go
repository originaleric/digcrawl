package weibo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func mapAPIResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed *statusResponse) (*platform.Content, error) {
	if parsed == nil || strings.TrimSpace(firstNonEmpty(parsed.ID, parsed.MID, parsed.TextRaw, parsed.Text)) == "" {
		return nil, fmt.Errorf("api response missing post payload")
	}
	text := firstNonEmpty(parsed.TextRaw, stripHTML(parsed.Text))
	canonical := norm.CanonicalURL
	if canonical == "" {
		canonical = "https://m.weibo.cn/detail/" + firstNonEmpty(parsed.ID, parsed.MID, norm.PlatformID)
	}
	meta := map[string]any{
		"extraction_source": "api",
		"platform_id":       firstNonEmpty(parsed.ID, parsed.MID, norm.PlatformID),
		"mid":               parsed.MID,
		"source":            parsed.Source,
		"api_ok":            parsed.OK,
	}
	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            firstNonEmpty(text, parsed.PageInfo.Title),
		Text:             text,
		Markdown:         text,
		PublishedAt:      strings.TrimSpace(parsed.CreatedAt),
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if parsed.User.ScreenName != "" || parsed.User.ID > 0 {
		profileURL := parsed.User.ProfileURL
		if profileURL != "" && !strings.HasPrefix(profileURL, "http") {
			profileURL = "https://m.weibo.cn" + profileURL
		}
		c.Author = &platform.Author{ID: fmt.Sprint(parsed.User.ID), Name: parsed.User.ScreenName, ProfileURL: profileURL, AvatarURL: parsed.User.ProfileImageURL}
	}
	c.Engagement = &platform.Engagement{Likes: parsed.Attitudes, Comments: parsed.CommentsCount, Reposts: parsed.RepostsCount}
	if in.IncludeMedia {
		c.Media = mediaFromStatus(parsed)
	}
	return c, nil
}

func mediaFromStatus(parsed *statusResponse) []platform.MediaItem {
	if parsed == nil {
		return nil
	}
	var out []platform.MediaItem
	for _, pic := range parsed.Pics {
		if u := firstNonEmpty(pic.Original, pic.URL, mediaURLFromAny(pic.Large), mediaURLFromAny(pic.Geo)); u != "" {
			out = append(out, platform.MediaItem{Type: "image", URL: u})
		}
	}
	if u := firstNonEmpty(parsed.PageInfo.MediaURL, parsed.PageInfo.PageURL); u != "" {
		out = append(out, platform.MediaItem{Type: "video", URL: u})
	}
	if parsed.PageInfo.PagePic != "" {
		out = append(out, platform.MediaItem{Type: "image", URL: parsed.PageInfo.PagePic})
	}
	return out
}

func mediaURLFromAny(v any) string {
	if m, ok := v.(map[string]any); ok {
		return firstNonEmpty(platform.MetaString(m, "url"), platform.MetaString(m, "src"))
	}
	return ""
}

func normalizePost(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	meta := map[string]any{}
	title := ""
	text := ""
	markdown := ""
	canonical := norm.CanonicalURL
	if resp != nil {
		meta = resp.Metadata
		title = strings.TrimSpace(resp.Title)
		text = strings.TrimSpace(resp.Text)
		markdown = resp.Markdown
		if canonical == "" {
			canonical = resp.URL
		}
	}
	meta = platform.EnsureMeta(meta)
	if t := platform.MetaString(meta, "title"); t != "" {
		title = t
	}
	if desc := platform.MetaString(meta, "description"); desc != "" {
		text = desc
	}
	c := &platform.Content{Platform: PlatformName, ContentType: ContentType, SourceURL: norm.OriginalURL, CanonicalURL: canonical, Title: title, Text: text, Markdown: markdown, CapturedAt: platform.NowRFC3339(), PlatformMetadata: meta}
	if norm.PlatformID != "" {
		c.PlatformMetadata["platform_id"] = norm.PlatformID
	}
	c.Author = platform.AuthorFromMeta(meta)
	c.Engagement = platform.EngagementFromMeta(meta)
	if pub := platform.MetaString(meta, "published_at"); pub != "" {
		c.PublishedAt = pub
	}
	if in.IncludeMedia {
		c.Media = platform.MediaFromMeta(meta)
		for _, u := range platform.MetaStringSlice(meta, "video_urls") {
			c.Media = append(c.Media, platform.MediaItem{Type: "video", URL: u})
		}
	}
	if in.IncludeScreenshot && resp != nil {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}

func validatePostContent(c *platform.Content) error {
	if c == nil {
		return platform.NewReadError(platform.ErrPartialContent, "微博帖子结果为空", PlatformName, nil)
	}
	meta := platform.EnsureMeta(c.PlatformMetadata)
	if code := platform.MetaErrorCode(meta); platform.IsFatalErrorCode(code) {
		return platform.NewReadError(code, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	title := strings.ToLower(strings.TrimSpace(c.Title))
	if strings.Contains(title, "sina visitor system") || strings.Contains(strings.ToLower(platform.MetaString(meta, "title")), "sina visitor system") {
		meta = platform.SetMetaError(meta, platform.ErrLoginRequired, "微博页面进入访客系统，需要登录态或安全验证")
		return platform.NewReadError(platform.ErrLoginRequired, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	if strings.TrimSpace(c.Text) == "" && len(c.Media) == 0 {
		meta = platform.SetMetaError(meta, platform.ErrPartialContent, "微博帖子正文和媒体为空")
		return platform.NewReadError(platform.ErrPartialContent, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	return nil
}

func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	replacer := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&#39;", "'", "&quot;", `"`)
	return strings.TrimSpace(replacer.Replace(s))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
