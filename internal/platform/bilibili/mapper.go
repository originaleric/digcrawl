package bilibili

import (
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func mapAPIResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed *viewResponse) (*platform.Content, error) {
	if parsed == nil || strings.TrimSpace(parsed.Data.Title) == "" {
		return nil, fmt.Errorf("api response missing video payload")
	}
	data := parsed.Data
	id := firstNonEmpty(data.BVID, norm.PlatformID)
	canonical := norm.CanonicalURL
	if canonical == "" {
		if strings.HasPrefix(strings.ToUpper(id), "BV") {
			canonical = "https://www.bilibili.com/video/" + id
		} else {
			canonical = fmt.Sprintf("https://www.bilibili.com/video/av%d", data.AID)
		}
	}
	meta := map[string]any{
		"extraction_source": "api",
		"platform_id":       id,
		"video_id":          id,
		"aid":               data.AID,
		"cid":               data.CID,
		"page_kind":         norm.ContentKind,
		"api_code":          parsed.Code,
	}
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            strings.TrimSpace(data.Title),
		Text:             strings.TrimSpace(data.Desc),
		Markdown:         strings.TrimSpace(data.Desc),
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if data.PubDate > 0 {
		content.PublishedAt = time.Unix(data.PubDate, 0).Format(time.RFC3339)
	}
	if data.Owner.Name != "" || data.Owner.MID > 0 {
		content.Author = &platform.Author{
			ID:         fmt.Sprint(data.Owner.MID),
			Name:       strings.TrimSpace(data.Owner.Name),
			ProfileURL: fmt.Sprintf("https://space.bilibili.com/%d", data.Owner.MID),
			AvatarURL:  strings.TrimSpace(data.Owner.Face),
		}
	}
	content.Engagement = &platform.Engagement{Likes: data.Stat.Like, Comments: data.Stat.Reply, Shares: data.Stat.Share, Bookmarks: data.Stat.Favorite}
	if in.IncludeMedia {
		if data.Pic != "" {
			content.Media = append(content.Media, platform.MediaItem{Type: "image", URL: data.Pic})
		}
	}
	return content, nil
}

func normalizeBrowserContent(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := strings.TrimSpace(norm.CanonicalURL)
	if canonical == "" && resp != nil {
		canonical = strings.TrimSpace(resp.URL)
	}
	meta := map[string]any{}
	title := ""
	text := ""
	markdown := ""
	if resp != nil {
		meta = resp.Metadata
		title = strings.TrimSpace(resp.Title)
		text = strings.TrimSpace(resp.Text)
		markdown = resp.Markdown
	}
	meta = platform.EnsureMeta(meta)
	if t := platform.MetaString(meta, "title"); t != "" {
		title = t
	}
	if desc := platform.MetaString(meta, "description"); desc != "" {
		text = desc
	}
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            strings.TrimSpace(strings.TrimSuffix(title, "_哔哩哔哩_bilibili")),
		Text:             text,
		Markdown:         markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if norm.PlatformID != "" {
		content.PlatformMetadata["platform_id"] = norm.PlatformID
		content.PlatformMetadata["video_id"] = norm.PlatformID
	}
	if norm.ContentKind != "" {
		content.PlatformMetadata["page_kind"] = norm.ContentKind
	}
	content.Author = platform.AuthorFromMeta(meta)
	if pub := platform.MetaString(meta, "published_at"); pub != "" {
		content.PublishedAt = pub
	}
	if in.IncludeMedia {
		content.Media = platform.MediaFromMeta(meta)
		for _, u := range platform.MetaStringSlice(meta, "video_urls") {
			content.Media = append(content.Media, platform.MediaItem{Type: "video", URL: u})
		}
	}
	if in.IncludeScreenshot && resp != nil {
		content.ScreenshotBase64 = resp.ScreenshotBase
	}
	return content
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
