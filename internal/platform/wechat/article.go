package wechat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "wechat"
	ContentType  = "article"
	CookieDomain = "mp.weixin.qq.com"
)

const (
	DefaultWaitSelector    = "#js_article, #js_content, #img-content, .rich_media_content, #activity-name"
	DefaultContentSelector = "#js_content, #js_article, #img-content, .rich_media_content, .rich_media_area_primary"
	browserTotalTimeout    = 90 * time.Second
)

var ArticleBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: "#js_article", ContentSelector: "#js_article"},
	{WaitSelector: "#js_content", ContentSelector: "#js_content"},
	{WaitSelector: "#img-content", ContentSelector: "#img-content"},
}

var articleBrowseFn = browseArticleWithFallbacks

// ReadArticle 读取微信公众号文章。
func ReadArticle(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	ctx = browser.WithBrowseRetryContext(ctx)
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	start := time.Now()
	content, httpMeta, err := readArticleHTTP(ctx, in, url)
	if err == nil {
		platform.MergeContentMetrics(content, platform.ReadMetrics{
			Platform:         PlatformName,
			Channel:          "http",
			ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
			APILatencyMs:     time.Since(start).Milliseconds(),
		})
		return content, nil
	}
	var re *platform.ReadError
	if errors.As(err, &re) {
		return nil, re
	}

	browserStart := time.Now()
	content, retryCount, err := readArticleBrowser(ctx, in, url)
	if err != nil {
		var readErr *platform.ReadError
		if errors.As(err, &readErr) {
			readErr.Meta = attachHTTPProbe(readErr.Meta, httpMeta)
			return nil, readErr
		}
		return nil, err
	}
	content.PlatformMetadata = attachHTTPProbe(content.PlatformMetadata, httpMeta)
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		BrowserLatencyMs: time.Since(browserStart).Milliseconds(),
		RetryCount:       retryCount,
	})
	return content, nil
}

func readArticleBrowser(ctx context.Context, in platform.ReadInput, url string) (*platform.Content, int, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, browserTotalTimeout)
	defer cancel()
	return readArticleBrowserWithOptions(ctx, in, url, articleBrowseFn, platform.DefaultBrowseRetry)
}

func readArticleBrowserWithBrowseFn(ctx context.Context, in platform.ReadInput, url string, browseFn func(context.Context, string, platform.ReadInput, platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error)) (*platform.Content, int, error) {
	return readArticleBrowserWithOptions(ctx, in, url, browseFn, platform.DefaultBrowseRetry)
}

func readArticleBrowserWithOptions(ctx context.Context, in platform.ReadInput, url string, browseFn func(context.Context, string, platform.ReadInput, platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error), retryPolicy platform.RetryPolicy) (*platform.Content, int, error) {
	base := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    CookieDomain,
		MetadataScript:  metadataScript(),
	}

	var content *platform.Content
	attempts := 0
	err := platform.WithBrowseRetry(ctx, retryPolicy, func(ctx context.Context) error {
		attempts++
		r, matched, browseErr := browseFn(ctx, url, in, base)
		if browseErr != nil {
			return browseErr
		}
		content = normalizeArticle(in, withBrowseDiagnostics(r, matched))
		return validateArticleContent(content, matched)
	})
	if err != nil {
		return nil, nonNegativeRetryCount(attempts), err
	}
	return content, nonNegativeRetryCount(attempts), nil
}

func browseArticleWithFallbacks(ctx context.Context, url string, in platform.ReadInput, base platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
	if len(ArticleBrowseAttempts) == 0 {
		resp, err := platform.Browse(ctx, url, in, base)
		return resp, strings.TrimSpace(base.ContentSelector), err
	}

	var lastErr error
	for _, att := range ArticleBrowseAttempts {
		spec := base
		if strings.TrimSpace(att.WaitSelector) != "" {
			spec.WaitSelector = att.WaitSelector
		}
		if strings.TrimSpace(att.ContentSelector) != "" {
			spec.ContentSelector = att.ContentSelector
		}
		resp, err := platform.Browse(ctx, url, in, spec)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil || !browser.CanBrowseFallback(err) {
				return nil, "", err
			}
			continue
		}
		return resp, spec.ContentSelector, nil
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("未能读取公众号文章")
}

func normalizeArticle(in platform.ReadInput, resp *browser.BrowserBrowseResponse) *platform.Content {
	meta := cloneMeta(resp.Metadata)
	title := strings.TrimSpace(resp.Title)
	if t := strings.TrimSpace(platform.MetaString(meta, "title")); t != "" {
		title = t
	}
	text := strings.TrimSpace(resp.Text)
	if t := strings.TrimSpace(platform.MetaString(meta, "text")); t != "" && text == "" {
		text = t
	}
	markdown := strings.TrimSpace(resp.Markdown)
	if markdown == "" && text != "" {
		markdown = text
	}
	sourceURL := strings.TrimSpace(resp.URL)
	if finalURL := strings.TrimSpace(platform.MetaString(meta, "final_url")); finalURL != "" {
		sourceURL = finalURL
	}

	c := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        sourceURL,
		CanonicalURL:     sourceURL,
		Title:            title,
		Text:             text,
		Markdown:         markdown,
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if resp.Metadata != nil {
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		if pub := platform.MetaString(resp.Metadata, "published_at"); pub != "" {
			c.PublishedAt = pub
		}
	}
	if in.IncludeMedia && resp.Metadata != nil {
		c.Media = platform.MediaFromMeta(resp.Metadata)
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}

func validateArticleContent(content *platform.Content, matchedSelector string) error {
	if content == nil {
		return platform.NewReadError(platform.ErrPartialContent, "公众号文章结果为空", PlatformName, nil)
	}

	meta := cloneMeta(content.PlatformMetadata)
	if matchedSelector != "" && strings.TrimSpace(platform.MetaString(meta, "matched_selector")) == "" {
		meta["matched_selector"] = matchedSelector
	}
	if textLen := strings.TrimSpace(platform.MetaString(meta, "body_text_length")); textLen == "" {
		meta["body_text_length"] = len([]rune(strings.TrimSpace(content.Text)))
	}

	if code := strings.TrimSpace(platform.MetaErrorCode(meta)); code != "" {
		return platform.NewReadError(code, metaErrorMessage(code, meta), PlatformName, meta)
	}

	if err := validateWechatContentScope(content, meta); err != nil {
		return wechatScopeReadError(meta, err)
	}

	title := strings.TrimSpace(content.Title)
	if title == "" || isPlaceholderTitle(title) {
		meta = platform.SetMetaError(meta, platform.ErrSelectorStale, "未提取到真实文章标题")
		return platform.NewReadError(platform.ErrSelectorStale, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}

	if strings.TrimSpace(content.Text) == "" {
		meta = platform.SetMetaError(meta, platform.ErrPartialContent, "正文为空")
		return platform.NewReadError(platform.ErrPartialContent, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}

	content.PlatformMetadata = meta
	return nil
}

func validateWechatContentScope(content *platform.Content, meta map[string]any) error {
	candidates := []string{
		platform.MetaString(meta, "final_url"),
		content.SourceURL,
		content.CanonicalURL,
	}
	for _, rawURL := range candidates {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		if err := validateWechatHTTPURL(rawURL); err != nil {
			return err
		}
	}
	return nil
}

func withBrowseDiagnostics(resp *browser.BrowserBrowseResponse, matchedSelector string) *browser.BrowserBrowseResponse {
	if resp == nil {
		return nil
	}
	meta := cloneMeta(resp.Metadata)
	if strings.TrimSpace(platform.MetaString(meta, "final_url")) == "" && strings.TrimSpace(resp.URL) != "" {
		meta["final_url"] = strings.TrimSpace(resp.URL)
	}
	if strings.TrimSpace(platform.MetaString(meta, "page_title")) == "" && strings.TrimSpace(resp.Title) != "" {
		meta["page_title"] = strings.TrimSpace(resp.Title)
	}
	if strings.TrimSpace(matchedSelector) != "" {
		meta["matched_selector"] = strings.TrimSpace(matchedSelector)
	}
	if strings.TrimSpace(platform.MetaString(meta, "body_text_length")) == "" {
		meta["body_text_length"] = len([]rune(strings.TrimSpace(resp.Text)))
	}
	resp.Metadata = meta
	return resp
}

func cloneMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func nonNegativeRetryCount(attempts int) int {
	if attempts <= 1 {
		return 0
	}
	return attempts - 1
}

func attachHTTPProbe(meta map[string]any, httpMeta map[string]any) map[string]any {
	meta = cloneMeta(meta)
	if len(httpMeta) == 0 {
		return meta
	}
	meta["http_probe"] = cloneMeta(httpMeta)
	return meta
}

func metaErrorMessage(code string, meta map[string]any) string {
	if msg := strings.TrimSpace(platform.MetaString(meta, "error_message")); msg != "" {
		return msg
	}
	switch code {
	case platform.ErrLoginRequired:
		return "需要登录后查看公众号文章"
	case platform.ErrCaptchaRequired:
		return "需要完成安全验证后查看公众号文章"
	case platform.ErrContentDeleted:
		return "公众号文章不存在或已删除"
	case platform.ErrContentPrivate:
		return "公众号文章不可见"
	case platform.ErrContentNotFound:
		return "公众号文章页面未找到"
	case platform.ErrRateLimited:
		return "公众号访问过于频繁"
	case platform.ErrScopeViolation:
		return "公众号文章最终 URL 不在允许域名列表"
	case platform.ErrSelectorStale:
		return "未命中有效的公众号文章正文结构"
	case platform.ErrPartialContent:
		return "公众号文章正文提取不完整"
	default:
		return code
	}
}

func wechatScopeReadError(meta map[string]any, err error) *platform.ReadError {
	meta = platform.SetMetaError(cloneMeta(meta), platform.ErrScopeViolation, metaErrorMessage(platform.ErrScopeViolation, meta))
	if err != nil {
		meta["scope_error"] = err.Error()
		if platform.MetaString(meta, "read_channel") == "http" {
			meta["http_error"] = err.Error()
		}
	}
	if strings.TrimSpace(platform.MetaString(meta, "http_parse_state")) == "" {
		meta["http_parse_state"] = wechatHTTPUnclassified
	}
	return platform.NewReadError(platform.ErrScopeViolation, platform.MetaString(meta, "error_message"), PlatformName, meta)
}

func isPlaceholderTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return true
	}
	switch strings.ToLower(trimmed) {
	case "weixin official accounts platform", "微信公众平台":
		return true
	}
	return false
}

func metadataScript() string {
	return `(() => {
  const warnings = [];
  const textOf = (node) => (node?.innerText || node?.textContent || '').replace(/\u00a0/g, ' ').trim();
  const attr = (selector, name) => document.querySelector(selector)?.getAttribute(name)?.trim() || '';
  const pickText = (selectors) => {
    for (const selector of selectors) {
      const value = textOf(document.querySelector(selector));
      if (value) return value;
    }
    return '';
  };
  const uniq = (items) => {
    const seen = new Set();
    const out = [];
    for (const item of items) {
      const value = (item || '').trim();
      if (!value || value.startsWith('data:') || seen.has(value)) continue;
      seen.add(value);
      out.push(value);
    }
    return out;
  };
  const pageTitle = (document.title || '').trim();
  const finalUrl = (location.href || '').trim();
  const bodyText = textOf(document.querySelector('#js_content') || document.querySelector('.rich_media_content') || document.querySelector('#js_article'));
  const bodyTextLength = bodyText.length;
  const detectPageState = () => {
    const body = textOf(document.body).slice(0, 4000);
    const loginHints = ['请在微信客户端打开链接', '请使用微信打开', '请在手机微信中打开', '请登录'];
    const captchaHints = ['环境异常', '访问过于频繁', '安全验证', '验证码', '操作过快'];
    const deletedHints = ['该内容已被发布者删除', '此内容因违规无法查看', '该内容已被投诉且无法查看', '文章已删除', '内容已删除'];
    const privateHints = ['以下内容需要关注后才能查看', '作者已设置关注后可见', '内容不可见'];
    const notFoundHints = ['页面不存在', '访问的页面不存在', '链接已失效'];
    if (loginHints.some(h => body.includes(h))) {
      return { error_code: 'LOGIN_REQUIRED', error_message: '需要在微信内或登录态下查看文章' };
    }
    if (captchaHints.some(h => body.includes(h))) {
      return { error_code: 'RATE_LIMITED', error_message: '公众号页面触发频控或安全验证' };
    }
    if (deletedHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_DELETED', error_message: '公众号文章不存在或已删除' };
    }
    if (privateHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '公众号文章不可见' };
    }
    if (notFoundHints.some(h => body.includes(h)) || /404|not found/i.test(pageTitle)) {
      return { error_code: 'CONTENT_NOT_FOUND', error_message: '公众号文章页面未找到' };
    }
    return {};
  };

  const titleSelectors = ['#activity-name', 'h1', '.rich_media_title', '.article-title'];
  const metaTitle = attr('meta[property="og:title"]', 'content')
    || attr('meta[name="twitter:title"]', 'content')
    || attr('meta[property="twitter:title"]', 'content');
  const title = pickText(titleSelectors) || metaTitle;
  if (!title) warnings.push('article title missing');
  if (!bodyText) warnings.push('article body empty');
  if (!document.querySelector('#js_content, .rich_media_content, #js_article')) warnings.push('article root missing');
  if (/^weixin official accounts platform$/i.test(pageTitle) || pageTitle === '微信公众平台') warnings.push('page title is platform shell');

  const state = detectPageState();
  if (state.error_code) {
    return {
      ...state,
      title,
      text: bodyText,
      author_name: pickText(['#js_name', '.profile_nickname']) || attr('meta[property="og:article:author"]', 'content'),
      published_at: pickText(['#publish_time']) || attr('meta[property="og:article:published_time"]', 'content'),
      image_urls: uniq(Array.from((document.querySelector('#js_content') || document).querySelectorAll('img')).map(img => img.getAttribute('data-src') || img.getAttribute('src') || '')),
      extraction_source: 'page_state',
      warnings,
      body_text_length: bodyTextLength,
      page_title: pageTitle,
      final_url: finalUrl,
    };
  }

  return {
    title,
    text: bodyText,
    author_name: pickText(['#js_name', '.profile_nickname']) || attr('meta[property="og:article:author"]', 'content'),
    published_at: pickText(['#publish_time']) || attr('meta[property="og:article:published_time"]', 'content'),
    image_urls: uniq(Array.from((document.querySelector('#js_content') || document).querySelectorAll('img')).map(img => img.getAttribute('data-src') || img.getAttribute('src') || '')),
    extraction_source: title || bodyText ? 'dom' : 'none',
    warnings,
    body_text_length: bodyTextLength,
    page_title: pageTitle,
    final_url: finalUrl,
  };
})()`
}
