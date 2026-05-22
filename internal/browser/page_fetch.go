package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/originaleric/digcrawl/config"
)

// PageFetchRequest 在已登录页面上下文中发起 fetch（自动携带 Cookie）。
type PageFetchRequest struct {
	WarmURL         string `json:"warm_url"`
	FetchURL        string `json:"fetch_url"`
	Method          string `json:"method,omitempty"`
	Body            string `json:"body,omitempty"`
	UseCookieDomain string `json:"use_cookie_domain,omitempty"`
	Referer         string `json:"referer,omitempty"`
}

// PageFetchResult fetch 执行结果。
type PageFetchResult struct {
	Status     int            `json:"status"`
	Body       string         `json:"body"`
	ParsedJSON map[string]any `json:"-"`
}

// PageFetchJSON 打开 warm 页后在浏览器上下文执行 fetch，返回 JSON 响应体（若可解析）。
func PageFetchJSON(ctx context.Context, req *PageFetchRequest) (*PageFetchResult, error) {
	if req == nil || strings.TrimSpace(req.FetchURL) == "" {
		return nil, fmt.Errorf("fetch url 不能为空")
	}
	warmURL := strings.TrimSpace(req.WarmURL)
	if warmURL == "" {
		return nil, fmt.Errorf("warm url 不能为空")
	}

	cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
	targetWarm, err := validateURL(warmURL)
	if err != nil {
		return nil, err
	}
	fetchURL := strings.TrimSpace(req.FetchURL)
	if err := checkAllowedDomain(targetWarm.Hostname(), cfg.AllowedDomains); err != nil {
		return nil, err
	}
	if u, parseErr := url.Parse(fetchURL); parseErr == nil && u.Host != "" {
		if err := checkAllowedDomain(u.Hostname(), cfg.AllowedDomains); err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TotalTimeoutSec)*time.Second)
	defer cancel()

	manager := getBrowserManager()
	session, err := manager.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer manager.release(session)

	page := session.page.Timeout(time.Duration(cfg.TotalTimeoutSec) * time.Second)
	cookieDomain := strings.TrimSpace(req.UseCookieDomain)
	if cookieDomain != "" {
		if err := loadDomainCookies(page, cookieDomain, cfg.CookieStoreDir, targetWarm); err != nil {
			return nil, err
		}
	}

	if err := page.Timeout(time.Duration(cfg.NavigateTimeoutSec) * time.Second).Navigate(targetWarm.String()); err != nil {
		return nil, fmt.Errorf("warm 页面导航失败: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("warm 页面加载失败: %w", err)
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	body := req.Body
	referer := strings.TrimSpace(req.Referer)
	if referer == "" {
		referer = warmURL
	}

	result, err := pageFetchEval(page, fetchURL, method, body, referer)
	if err != nil {
		return nil, err
	}
	if cookieDomain != "" {
		_ = saveDomainCookies(page, cookieDomain, cfg.CookieStoreDir, targetWarm)
	}
	return result, nil
}

func pageFetchEval(page *rod.Page, fetchURL, method, body, referer string) (*PageFetchResult, error) {
	script := `(url, method, body, referer) => {
  const headers = { 'Accept': 'application/json, text/plain, */*' };
  if (referer) headers['Referer'] = referer;
  const opts = { method, credentials: 'include', headers };
  if (body && body.length > 0) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = body;
  }
  return fetch(url, opts).then(async (r) => ({
    status: r.status,
    body: await r.text()
  })).catch((e) => ({ status: 0, body: String(e) }));
}`
	eval, err := page.Eval(script, fetchURL, method, body, referer)
	if err != nil {
		return nil, fmt.Errorf("page fetch eval 失败: %w", err)
	}
	jsonVal, err := page.ObjectToJSON(eval)
	if err != nil {
		return nil, err
	}
	var out PageFetchResult
	if err := json.Unmarshal([]byte(jsonVal.Str()), &out); err != nil {
		return nil, fmt.Errorf("解析 fetch 结果失败: %w", err)
	}
	if out.Status == 0 {
		return &out, fmt.Errorf("fetch 失败: %s", out.Body)
	}
	bodyTrim := strings.TrimSpace(out.Body)
	if bodyTrim != "" && (strings.HasPrefix(bodyTrim, "{") || strings.HasPrefix(bodyTrim, "[")) {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(bodyTrim), &parsed); err == nil {
			out.ParsedJSON = parsed
		}
	}
	return &out, nil
}
