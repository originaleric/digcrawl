package wechatchannels

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/media"
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	shareHost            = "weixin.qq.com"
	sharePathPrefix      = "/sph/"
	previewHost          = "channels.weixin.qq.com"
	previewPath          = "/finder-preview/pages/sph"
	shareWaitSelector    = "video, .page-feed, .warning-page"
	shareContentSelector = "body"
	SharePageTimeout     = 45 * time.Second
)

var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,256}$`)

// ReadShare reads a public WeChat Channels share URL. It deliberately does
// not load a WeChat cookie domain or use the desktop bridge: the public share
// page supplies text, a screenshot, and the optional public cover.
func ReadShare(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	rawURL := strings.TrimSpace(in.URL)
	if err := validateShareURL(rawURL); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.UseCookieDomain) != "" {
		return nil, platform.NewReadError(platform.ErrScopeViolation, "视频号分享链接读取不支持复用 Cookie", PlatformName, nil)
	}

	ctx, cancel := context.WithTimeout(ctx, SharePageTimeout)
	defer cancel()
	start := time.Now()
	resp, err := platform.Browse(ctx, rawURL, in, platform.BrowseSpec{
		WaitSelector:    shareWaitSelector,
		ContentSelector: shareContentSelector,
		MetadataScript:  shareMetadataScript(),
	})
	if err != nil {
		return nil, err
	}
	content, err := contentFromShareResponse(in, rawURL, resp)
	if err != nil {
		return nil, err
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		BrowserLatencyMs: time.Since(start).Milliseconds(),
	})
	return content, nil
}

func validateShareURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return platform.NewReadError(platform.ErrContentTypeMismatch, "无效的视频号分享链接", PlatformName, nil)
	}
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || !strings.EqualFold(u.Hostname(), shareHost) {
		return platform.NewReadError(platform.ErrScopeViolation, "视频号分享链接必须为 https://weixin.qq.com/sph/...", PlatformName, nil)
	}
	shareID := strings.TrimPrefix(u.EscapedPath(), sharePathPrefix)
	if !strings.HasPrefix(u.EscapedPath(), sharePathPrefix) || strings.Contains(shareID, "/") || !shareIDPattern.MatchString(shareID) {
		return platform.NewReadError(platform.ErrContentTypeMismatch, "当前 URL 不是视频号分享链接", PlatformName, nil)
	}
	return nil
}

func contentFromShareResponse(in platform.ReadInput, sourceURL string, resp *browser.BrowserBrowseResponse) (*platform.Content, error) {
	if resp == nil {
		return nil, platform.NewReadError(platform.ErrPartialContent, "视频号分享页结果为空", PlatformName, nil)
	}
	finalURL := strings.TrimSpace(resp.URL)
	if err := validatePreviewURL(finalURL); err != nil {
		return nil, err
	}
	meta := cloneShareMeta(resp.Metadata)
	delete(meta, "video_urls")
	if code := strings.TrimSpace(platform.MetaErrorCode(meta)); code != "" {
		return nil, platform.NewReadError(code, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	title := strings.TrimSpace(platform.MetaString(meta, "title"))
	description := strings.TrimSpace(platform.MetaString(meta, "description"))
	if title == "" || title == "视频号" {
		title = description
	}
	if title == "" {
		return nil, platform.NewReadError(platform.ErrSelectorStale, "未从视频号分享页提取到视频标题", PlatformName, meta)
	}

	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        sourceURL,
		CanonicalURL:     finalURL,
		Title:            title,
		Text:             description,
		Markdown:         description,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if authorName := strings.TrimSpace(platform.MetaString(meta, "author_name")); authorName != "" {
		content.Author = &platform.Author{Name: authorName}
	}
	if publishedAt := strings.TrimSpace(platform.MetaString(meta, "published_at")); publishedAt != "" {
		content.PublishedAt = publishedAt
	}
	if in.IncludeMedia {
		content.Media = shareCover(platform.MetaString(meta, "cover_url"))
	}
	if in.IncludeScreenshot {
		content.ScreenshotBase64 = resp.ScreenshotBase
	}
	return content, nil
}

func shareCover(rawURL string) []platform.MediaItem {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || media.ValidateRemoteURL(rawURL) != nil {
		return nil
	}
	return []platform.MediaItem{{Type: "image", URL: rawURL}}
}

func validatePreviewURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), previewHost) || u.Path != previewPath || !shareIDPattern.MatchString(u.Query().Get("id")) {
		return platform.NewReadError(platform.ErrScopeViolation, "视频号分享页跳转到了未允许的地址", PlatformName, nil)
	}
	return nil
}

func cloneShareMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(meta))
	for key, value := range meta {
		cloned[key] = value
	}
	return cloned
}

func shareMetadataScript() string {
	return `() => {
  const text = (selector) => (document.querySelector(selector)?.innerText || document.querySelector(selector)?.textContent || '').trim();
  const body = (document.body?.innerText || '').slice(0, 6000);
  const error = (code, message) => ({ error_code: code, error_message: message, extraction_source: 'page_state', final_url: String(location.href || '') });
  if (['内容不存在', '页面不存在', '链接已失效', '已被删除'].some(hint => body.includes(hint))) return error('CONTENT_NOT_FOUND', '视频号内容不存在或已失效');
  if (['暂时无法查看', '内容不可见', '仅在微信内查看'].some(hint => body.includes(hint)) && !document.querySelector('video')) return error('CONTENT_PRIVATE', '视频号内容当前不可公开查看');
  const video = document.querySelector('video');
  const playback_restricted = ['可扫码前往微信观看此内容', '前往微信观看此内容'].some(hint => body.includes(hint)) && !video;
  const title = text('.feed-desc-wrap') || text('[class*="feed-desc"]') || '';
  const description = title;
  const author_name = text('.author-name') || text('[class*="author-name"]') || '';
  const published_at = text('.feed-create-time-wrap') || text('[class*="create-time"]') || '';
  const has_video = Boolean(video && (video.currentSrc || video.src || video.querySelector('source[src]')));
  const cover_url = video?.poster || '';
  if (!title && !has_video) return error('SELECTOR_STALE', '视频号分享页未加载出视频内容');
  return {
    extraction_source: has_video ? 'dom_video' : 'dom',
    title,
    description,
    author_name,
    published_at,
    cover_url,
    playback_restricted,
    final_url: String(location.href || ''),
    body_text_length: (document.body?.innerText || '').length,
  };
}`
}
