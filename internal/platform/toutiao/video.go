package toutiao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

// ReadVideo reads a Toutiao video page through the local browser.
func ReadVideo(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	norm, err := NormalizeURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind != VideoContentType {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "当前 URL 不是头条视频页", PlatformName, map[string]any{
			"page_kind": norm.ContentKind,
		})
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}

	start := time.Now()
	content, retryCount, err := readVideoBrowser(ctx, in, norm, cookieDomain)
	if err != nil {
		return nil, err
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		BrowserLatencyMs: time.Since(start).Milliseconds(),
		RetryCount:       retryCount,
	})
	return content, nil
}

func readVideoBrowser(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, cookieDomain string) (*platform.Content, int, error) {
	browseURL := norm.EffectiveBrowseURL()
	if browseURL == "" {
		browseURL = norm.ResolvedURL
	}
	base := platform.BrowseSpec{
		WaitSelector:    VideoWaitSelector,
		ContentSelector: VideoContentSelector,
		CookieDomain:    cookieDomain,
		MetadataScript:  VideoMetadataScript(),
		CaptureMedia:    in.IncludeMedia,
	}
	var resp *browser.BrowserBrowseResponse
	err := platform.WithBrowseRetry(ctx, platform.BrowseRetryPolicy(), func(ctx context.Context) error {
		r, browseErr := platform.BrowseWithFallbacks(ctx, browseURL, in, base, VideoBrowseAttempts)
		if browseErr != nil {
			return browseErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if resp == nil {
		return nil, 0, fmt.Errorf("未能读取头条视频页面")
	}
	if resp != nil && resp.URL != "" {
		norm.ResolvedURL = resp.URL
	}
	code := platform.MetaErrorCode(resp.Metadata)
	if platform.IsFatalErrorCode(code) {
		return nil, 0, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}
	content := normalizeVideo(in, norm, resp)
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	if err := validateVideoContent(content, in.IncludeMedia); err != nil {
		return nil, 0, err
	}
	return content, 0, nil
}

func normalizeVideo(in platform.ReadInput, norm *platform.NormalizedURL, resp *browser.BrowserBrowseResponse) *platform.Content {
	canonical := norm.CanonicalURL
	if canonical == "" && resp != nil {
		canonical = resp.URL
	}
	text := ""
	title := ""
	markdown := ""
	meta := map[string]any{}
	if resp != nil {
		title = resp.Title
		text = strings.TrimSpace(resp.Text)
		markdown = resp.Markdown
		meta = cloneMeta(resp.Metadata)
	}
	if meta == nil {
		meta = map[string]any{}
	}
	if t := strings.TrimSpace(platform.MetaString(meta, "title")); t != "" {
		title = t
	}
	if desc := strings.TrimSpace(platform.MetaString(meta, "description")); desc != "" {
		text = desc
	}
	if title == "" && resp != nil {
		title = strings.TrimSpace(strings.TrimSuffix(resp.Title, "- 今日头条"))
	}
	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      VideoContentType,
		SourceURL:        norm.OriginalURL,
		CanonicalURL:     canonical,
		Title:            title,
		Text:             text,
		Markdown:         markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	c.PlatformMetadata = platform.EnsureMeta(c.PlatformMetadata)
	c.PlatformMetadata["page_kind"] = VideoContentType
	if norm.PlatformID != "" {
		c.PlatformMetadata["platform_id"] = norm.PlatformID
		c.PlatformMetadata["video_id"] = norm.PlatformID
	}
	if norm.ResolvedURL != "" {
		c.PlatformMetadata["resolved_url"] = norm.ResolvedURL
	}
	if len(norm.RiskFlags) > 0 {
		c.PlatformMetadata["risk_flags"] = norm.RiskFlags
	}
	c.Author = platform.AuthorFromMeta(meta)
	c.Engagement = platform.EngagementFromMeta(meta)
	if pub := cleanVideoPublishedAt(platform.MetaString(meta, "published_at")); pub != "" {
		c.PublishedAt = pub
		meta["published_at"] = pub
	} else {
		delete(meta, "published_at")
	}
	if in.IncludeMedia {
		if cover := platform.MetaString(meta, "cover_url"); cover != "" {
			c.Media = append(c.Media, platform.MediaItem{Type: "image", URL: cover})
		}
		videoURLs := platform.MetaStringSlice(meta, "video_urls")
		if resp != nil && len(resp.MediaURLs) > 0 {
			videoURLs = resp.MediaURLs
			meta["media_extraction_source"] = "browser_resource"
		}
		for _, u := range videoURLs {
			c.Media = append(c.Media, platform.MediaItem{Type: "video", URL: u})
		}
	}
	if in.IncludeScreenshot && resp != nil {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}

func validateVideoContent(content *platform.Content, requireVideoMedia bool) error {
	if content == nil {
		return platform.NewReadError(platform.ErrPartialContent, "头条视频结果为空", PlatformName, nil)
	}
	meta := platform.EnsureMeta(content.PlatformMetadata)
	if code := platform.MetaErrorCode(meta); code != "" && platform.IsFatalErrorCode(code) {
		return platform.NewReadError(code, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	if strings.TrimSpace(content.Title) == "" || isShellTitle(content.Title) {
		return platform.NewReadError(platform.ErrSelectorStale, "未提取到真实头条视频标题", PlatformName, meta)
	}
	if requireVideoMedia && !hasVideoMedia(content.Media) {
		return platform.NewReadError(platform.ErrPartialContent, "未提取到头条视频媒体地址", PlatformName, meta)
	}
	if strings.TrimSpace(content.Text) == "" && len(content.Media) == 0 {
		return platform.NewReadError(platform.ErrPartialContent, "头条视频正文和媒体为空", PlatformName, meta)
	}
	content.PlatformMetadata = meta
	return nil
}

func hasVideoMedia(items []platform.MediaItem) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Type), "video") && strings.TrimSpace(item.URL) != "" {
			return true
		}
	}
	return false
}

func cleanVideoPublishedAt(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if s == "直播" || s == "正在直播" || strings.Contains(lower, "live") {
		return ""
	}
	if strings.Contains(s, "直播") && strings.Contains(s, ":") {
		return ""
	}
	if strings.Contains(s, "/") && looksLikeVideoClock(s) {
		return ""
	}
	if looksLikeVideoClock(s) {
		return ""
	}
	return s
}

func looksLikeVideoClock(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	parts := strings.Split(s, "/")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		cols := strings.Split(part, ":")
		if len(cols) < 2 || len(cols) > 3 {
			return false
		}
		for _, col := range cols {
			if col == "" {
				return false
			}
			for _, r := range col {
				if r < '0' || r > '9' {
					return false
				}
			}
		}
	}
	return true
}
