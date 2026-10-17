package browser

import (
	"context"
	"encoding/json"
	"errors"
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
	Status      int            `json:"status"`
	Body        string         `json:"body"`
	ContentType string         `json:"content_type,omitempty"`
	ParsedJSON  map[string]any `json:"-"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

// PageFetchJSON 打开 warm 页后在浏览器上下文执行 fetch，返回 JSON 响应体（若可解析）。
func PageFetchJSON(ctx context.Context, req *PageFetchRequest) (*PageFetchResult, error) {
	return pageFetchWithSessionRunner(ctx, req, getBrowserManager, nil)
}

func pageFetchWithSessionRunner(ctx context.Context, req *PageFetchRequest, managerProvider func() *browserManager, runner func(context.Context, *browserSession) (*PageFetchResult, error)) (*PageFetchResult, error) {
	ctx, state, cancel := browseInvocationContext(ctx)
	defer cancel()
	replaying := false
	for {
		state.beginAttempt(replaying)
		result, err := pageFetchAttempt(ctx, req, managerProvider, runner)
		if err == nil && ctx.Err() != nil {
			// As with DOM reads, independent target cleanup can finish after the
			// request ends. Cancellation never turns into success or a replay.
			err = newBrowseError(ErrorBrowserTimeout, "page_fetch", "浏览器 API 读取已取消或时间预算耗尽", ctx.Err(), false)
			err.(*BrowseError).Diagnostics["replay_safe"] = req == nil || req.Method == "" || strings.EqualFold(req.Method, "GET") || strings.EqualFold(req.Method, "HEAD")
			result = nil
		}
		if err == nil {
			state.succeeded()
			if result != nil {
				result.Diagnostics = BrowseDiagnostics(ctx)
			}
			recordBrowseDiagnostic(ctx, nil)
			return result, nil
		}
		if !state.claimRetry(ctx, err, replaying) {
			err = finishBrowseError(ctx, err)
			if result != nil {
				result.Diagnostics = BrowseDiagnostics(ctx)
			}
			recordBrowseDiagnostic(ctx, err)
			return result, err
		}
		replaying = true
		if !state.waitRetry(ctx) {
			err = finishBrowseError(ctx, err)
			if result != nil {
				result.Diagnostics = BrowseDiagnostics(ctx)
			}
			recordBrowseDiagnostic(ctx, err)
			return result, err
		}
	}
}

func pageFetchAttempt(ctx context.Context, req *PageFetchRequest, managerProvider func() *browserManager, runner func(context.Context, *browserSession) (*PageFetchResult, error)) (result *PageFetchResult, retErr error) {

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

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	manager := managerProvider()
	session, err := manager.acquireSession(ctx)
	if err != nil {
		return nil, newBrowseError(ErrorBrowserUnavailable, "acquire", "无法启动或连接本地浏览器", err, false)
	}
	fetchStarted := false
	defer func() {
		var be *BrowseError
		if errors.As(retErr, &be) && be.Stage == "page_fetch" {
			if be.Diagnostics == nil {
				be.Diagnostics = map[string]any{}
			}
			be.Diagnostics["replay_safe"] = method == "GET" || method == "HEAD"
		}
		if isConnectionFailure(retErr) {
			manager.retireConnection(session.conn)
		}
		if err := manager.releaseSession(session); err != nil && (retErr == nil || browseCauseReason(err) == "external_target_cleanup_required") {
			result = nil
			retErr = newBrowseError(ErrorBrowserUnavailable, "close_page", "浏览器页面清理失败", err, false)
		}
		if fetchStarted && method != "GET" && method != "HEAD" && errors.As(retErr, &be) {
			if be.Diagnostics == nil {
				be.Diagnostics = map[string]any{}
			}
			be.Diagnostics["replay_safe"] = false
		}
	}()
	if runner != nil {
		fetchStarted = true
		return runner(ctx, session)
	}

	page := session.page.Context(ctx).Timeout(time.Duration(cfg.TotalTimeoutSec) * time.Second)
	if cookieDomain := strings.TrimSpace(req.UseCookieDomain); cookieDomain != "" {
		if err := loadDomainCookies(page, cookieDomain, cfg.CookieStoreDir, targetWarm); err != nil {
			return nil, newBrowseError(ErrorBrowserCookieFailed, "load_cookies", "加载 Cookie 失败", err, false)
		}
	}

	if err := page.Timeout(time.Duration(cfg.NavigateTimeoutSec) * time.Second).Navigate(targetWarm.String()); err != nil {
		return nil, newBrowseError(ErrorBrowserNavigationFailed, "navigate", "warm 页面导航失败", err, true)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, newBrowseError(ErrorBrowserPageLoadFailed, "wait_load", "warm 页面加载失败", err, true)
	}

	body := req.Body
	referer := strings.TrimSpace(req.Referer)
	if referer == "" {
		referer = warmURL
	}

	fetchStarted = true
	result, err = pageFetchEval(page, fetchURL, method, body, referer)
	if err != nil {
		// A status-zero HTTP fetch has a known JS result and is a business/API
		// failure. A CDP failure has an unknown outcome and must retain its type.
		if result != nil && ctx.Err() == nil && !isConnectionFailure(err) && !isLocalBrowserFailure(nil, err) {
			return result, err
		}
		e := newBrowseError(ErrorBrowserUnavailable, "page_fetch", "浏览器 API 读取失败", err, false).(*BrowseError)
		e.Diagnostics["replay_safe"] = method == "GET" || method == "HEAD"
		return nil, e
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
  return fetch(url, opts).then(async (r) => JSON.stringify({
    status: r.status,
    content_type: r.headers.get('content-type') || '',
    body: await r.text()
  })).catch((e) => JSON.stringify({ status: 0, body: String(e), content_type: '' }));
}`
	eval, err := page.Eval(script, fetchURL, method, body, referer)
	if err != nil {
		return nil, fmt.Errorf("page fetch eval 失败: %w", err)
	}
	jsonVal, err := page.ObjectToJSON(eval)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(jsonVal.Str())
	var payload string
	if strings.HasPrefix(raw, "\"") {
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			raw = strings.TrimSpace(payload)
		}
	}
	var out PageFetchResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
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
