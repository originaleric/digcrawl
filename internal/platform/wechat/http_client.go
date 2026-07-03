package wechat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
	xhtml "golang.org/x/net/html"
)

const (
	wechatHTTPTimeout       = 20 * time.Second
	wechatHTTPMaxBodyBytes  = 12 * 1024 * 1024
	wechatDesktopUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	wechatHTTPParseOK       = "ok"
	wechatHTTPParsePartial  = "partial"
	wechatHTTPUnclassified  = "unclassified"
	wechatHTTPPageArticle   = "article"
	wechatHTTPPageCaptcha   = "captcha_required"
	wechatHTTPPageRateLimit = "rate_limited"
)

var wechatHTTPAllowedHosts = []string{"mp.weixin.qq.com", "weixin.qq.com"}

var newWechatHTTPClient = func(redirects *[]string) *http.Client {
	return &http.Client{
		Timeout: wechatHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !wechatHTTPHostAllowed(req.URL.Hostname()) {
				return newWechatScopeError(req.URL.String(), "微信 HTTP 跳转目标 %s 不在允许域名列表", req.URL.Hostname())
			}
			*redirects = append(*redirects, req.URL.String())
			return nil
		},
	}
}

type wechatScopeError struct {
	URL     string
	Message string
}

func (e *wechatScopeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func newWechatScopeError(rawURL, format string, args ...any) error {
	return &wechatScopeError{URL: strings.TrimSpace(rawURL), Message: fmt.Sprintf(format, args...)}
}

type httpFallbackError struct {
	Reason string
	Meta   map[string]any
}

func (e *httpFallbackError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return e.Reason
	}
	return "wechat http fallback"
}

func readArticleHTTP(ctx context.Context, in platform.ReadInput, rawURL string) (*platform.Content, map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, wechatHTTPTimeout)
	defer cancel()

	if err := validateWechatHTTPURL(rawURL); err != nil {
		meta := httpMeta(0, rawURL, nil)
		readErr := wechatScopeReadError(meta, err)
		return nil, readErr.Meta, readErr
	}

	var redirects []string
	client := newWechatHTTPClient(&redirects)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		meta := httpMeta(0, rawURL, redirects)
		meta["http_error"] = err.Error()
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: "build request: " + err.Error(), Meta: meta}
	}
	req.Header.Set("User-Agent", wechatDesktopUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		meta := httpMeta(0, rawURL, redirects)
		if isWechatScopeError(err) {
			readErr := wechatScopeReadError(meta, err)
			return nil, readErr.Meta, readErr
		}
		meta["http_error"] = err.Error()
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: "http fetch: " + err.Error(), Meta: meta}
	}
	defer resp.Body.Close()

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	meta := httpMeta(resp.StatusCode, finalURL, redirects)
	if err := validateWechatHTTPURL(finalURL); err != nil {
		readErr := wechatScopeReadError(meta, err)
		return nil, readErr.Meta, readErr
	}
	if code, msg, state := classifyWechatHTTPPage("", finalURL, redirects); code != "" {
		meta["page_state"] = state
		meta = platform.SetMetaError(meta, code, msg)
		return nil, meta, platform.NewReadError(code, msg, PlatformName, meta)
	}
	if resp.StatusCode >= 500 {
		meta["http_error"] = fmt.Sprintf("http %d", resp.StatusCode)
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: meta["http_error"].(string), Meta: meta}
	}

	body, truncated, err := readLimited(resp.Body, wechatHTTPMaxBodyBytes)
	if err != nil {
		meta["http_error"] = err.Error()
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: "read body: " + err.Error(), Meta: meta}
	}
	rawHTML := string(body)
	if truncated {
		meta["http_error"] = "response_too_large"
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: "response too large", Meta: meta}
	}
	if resp.StatusCode >= 400 {
		if code, msg, state := classifyWechatHTTPPage(rawHTML, finalURL, redirects); code != "" {
			meta["page_state"] = state
			meta = platform.SetMetaError(meta, code, msg)
			return nil, meta, platform.NewReadError(code, msg, PlatformName, meta)
		}
		meta["http_error"] = fmt.Sprintf("http %d", resp.StatusCode)
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: meta["http_error"].(string), Meta: meta}
	}

	parsed, err := parseArticleHTML(rawHTML, finalURL)
	if err != nil {
		if code, msg, state := classifyWechatHTTPPage(rawHTML, finalURL, redirects); code != "" {
			meta["page_state"] = state
			meta = platform.SetMetaError(meta, code, msg)
			return nil, meta, platform.NewReadError(code, msg, PlatformName, meta)
		}
		meta["http_error"] = err.Error()
		meta["http_parse_state"] = wechatHTTPUnclassified
		return nil, meta, &httpFallbackError{Reason: "parse article html: " + err.Error(), Meta: meta}
	}
	meta["page_state"] = wechatHTTPPageArticle
	meta["http_parse_state"] = wechatHTTPParseOK
	content := articleContentFromParsed(in, rawURL, finalURL, parsed, meta)
	if err := validateArticleContent(content, "#js_content"); err != nil {
		if code, msg, state := classifyWechatHTTPPage(rawHTML, finalURL, redirects); code != "" {
			meta["page_state"] = state
			meta = platform.SetMetaError(meta, code, msg)
			return nil, meta, platform.NewReadError(code, msg, PlatformName, meta)
		}
		if parsed.BodyTextLength > 0 || strings.TrimSpace(parsed.Title) != "" {
			meta["http_parse_state"] = wechatHTTPParsePartial
		} else {
			meta["http_parse_state"] = wechatHTTPUnclassified
		}
		meta["http_error"] = err.Error()
		return nil, meta, &httpFallbackError{Reason: "validate http article: " + err.Error(), Meta: meta}
	}
	return content, meta, nil
}

func validateWechatHTTPURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("无效 url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return newWechatScopeError(rawURL, "仅支持 http/https URL")
	}
	if !wechatHTTPHostAllowed(u.Hostname()) {
		return newWechatScopeError(rawURL, "微信 HTTP 目标 %s 不在允许域名列表", u.Hostname())
	}
	return nil
}

func isWechatScopeError(err error) bool {
	var scopeErr *wechatScopeError
	return errors.As(err, &scopeErr)
}

func wechatHTTPHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, allowed := range wechatHTTPAllowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func httpMeta(status int, finalURL string, redirects []string) map[string]any {
	meta := map[string]any{
		"read_channel":      "http",
		"extraction_source": "html",
		"final_url":         strings.TrimSpace(finalURL),
	}
	if status > 0 {
		meta["http_status"] = status
	}
	if len(redirects) > 0 {
		meta["redirect_chain"] = append([]string(nil), redirects...)
	}
	return meta
}

func classifyWechatHTTPPage(rawHTML, finalURL string, redirects []string) (string, string, string) {
	for _, u := range append(append([]string{}, redirects...), finalURL) {
		if strings.Contains(u, "/mp/wappoc_appmsgcaptcha") {
			return platform.ErrCaptchaRequired, "公众号页面需要完成安全验证", wechatHTTPPageCaptcha
		}
	}
	body := visibleTextForClassification(rawHTML)
	if body == "" {
		return "", "", ""
	}
	if containsAny(body, "安全验证", "验证码") {
		return platform.ErrCaptchaRequired, "公众号页面需要完成安全验证", wechatHTTPPageCaptcha
	}
	if containsAny(body, "环境异常", "访问过于频繁", "操作过快") {
		return platform.ErrRateLimited, "公众号访问过于频繁", wechatHTTPPageRateLimit
	}
	if containsAny(body, "请在微信客户端打开链接", "请使用微信打开", "请在手机微信中打开", "请登录") {
		return platform.ErrLoginRequired, "需要在微信内或登录态下查看文章", "login_required"
	}
	if containsAny(body, "该内容已被发布者删除", "此内容因违规无法查看", "该内容已被投诉且无法查看", "文章已删除", "内容已删除") {
		return platform.ErrContentDeleted, "公众号文章不存在或已删除", "content_deleted"
	}
	if containsAny(body, "以下内容需要关注后才能查看", "作者已设置关注后可见", "内容不可见") {
		return platform.ErrContentPrivate, "公众号文章不可见", "content_private"
	}
	if containsAny(body, "页面不存在", "访问的页面不存在", "链接已失效") {
		return platform.ErrContentNotFound, "公众号文章页面未找到", "content_not_found"
	}
	return "", "", ""
}

func visibleTextForClassification(rawHTML string) string {
	doc, err := xhtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return normalizeSpace(rawHTML)
	}
	return normalizeSpace(textOfNode(doc))
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
