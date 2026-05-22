package browser

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
)

// BrowserSnapshotRequest 浏览器快照请求
type BrowserSnapshotRequest struct {
	URL             string `json:"url" jsonschema:"required,description=需要访问的目标网页 URL"`
	Filter          string `json:"filter,omitempty" jsonschema:"description=过滤类型：interactive（仅可交互元素）、visible（仅可见元素）、all（全部），默认 interactive"`
	MaxDepth        int    `json:"max_depth,omitempty" jsonschema:"description=最大深度，-1表示不限制，默认-1"`
	WaitSelector    string `json:"wait_selector,omitempty" jsonschema:"description=可选 CSS 选择器，等待目标元素出现后再提取"`
	UseCookieDomain string `json:"use_cookie_domain,omitempty" jsonschema:"description=可选 Cookie 域名（如 weixin.qq.com），用于加载并复用该域的 Cookie"`
}

// BrowserSnapshotResponse 浏览器快照响应
type BrowserSnapshotResponse struct {
	URL      string           `json:"url" jsonschema:"description=最终访问的 URL"`
	Title    string           `json:"title" jsonschema:"description=页面标题"`
	Elements []AXNode         `json:"elements" jsonschema:"description=可交互元素列表"`
	Refs     map[string]int64 `json:"refs,omitempty" jsonschema:"description=引用ID到NodeID的映射（内部使用）"`
}

// BrowserSnapshot 获取页面结构化快照
func BrowserSnapshot(ctx context.Context, req *BrowserSnapshotRequest) (*BrowserSnapshotResponse, error) {
	if req == nil || strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
	filter := strings.ToLower(strings.TrimSpace(req.Filter))
	if filter == "" {
		filter = FilterInteractive // 默认仅返回可交互元素
	}
	if filter != FilterInteractive && filter != FilterVisible && filter != FilterAll {
		return nil, fmt.Errorf("filter 仅支持 interactive、visible 或 all")
	}

	maxDepth := req.MaxDepth
	if maxDepth == 0 {
		maxDepth = -1 // 默认不限制深度
	}

	targetURL, err := validateURL(req.URL)
	if err != nil {
		return nil, err
	}
	if err := checkAllowedDomain(targetURL.Hostname(), cfg.AllowedDomains); err != nil {
		return nil, err
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

	// 加载Cookie（如果指定）
	if req.UseCookieDomain != "" {
		if err := checkAllowedDomain(req.UseCookieDomain, cfg.AllowedDomains); err != nil {
			return nil, fmt.Errorf("cookie 域名不在白名单: %w", err)
		}
		if err := loadDomainCookies(page, req.UseCookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
			return nil, err
		}
	}

	// 导航到目标URL
	if err := page.Timeout(time.Duration(cfg.NavigateTimeoutSec) * time.Second).Navigate(targetURL.String()); err != nil {
		return nil, fmt.Errorf("页面导航失败: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("页面加载失败: %w", err)
	}

	// 等待选择器（如果指定）
	if req.WaitSelector != "" {
		if _, err := page.Timeout(time.Duration(cfg.WaitSelectorTimeoutSec) * time.Second).Element(req.WaitSelector); err != nil {
			return nil, fmt.Errorf("等待选择器失败: %w", err)
		}
	}

	// 获取页面标题
	title, _ := pageTitle(page)

	// 获取无障碍树快照
	elements, refs, err := FetchAXTree(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("获取无障碍树失败: %w", err)
	}

	// 应用过滤器
	filteredElements := filterElements(elements, filter)

	// 保存Cookie（如果指定）
	cookieDomain := strings.TrimSpace(req.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = targetURL.Hostname()
	}
	if err := saveDomainCookies(page, cookieDomain, cfg.CookieStoreDir, targetURL); err != nil {
		// Cookie保存失败不影响快照结果
		_ = err
	}

	return &BrowserSnapshotResponse{
		URL:      targetURL.String(),
		Title:    title,
		Elements: filteredElements,
		Refs:     refs,
	}, nil
}
