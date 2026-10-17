package browser

import (
	"context"
	"fmt"
	"strings"
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

	// Both snapshot entry points use the same read, recovery and source boundary.
	resp, err := BrowserBrowse(ctx, &BrowserBrowseRequest{
		URL: req.URL, Mode: "snapshot", WaitSelector: req.WaitSelector,
		UseCookieDomain: req.UseCookieDomain, snapshotFilter: filter, snapshotMaxDepth: maxDepth,
	})
	if err != nil {
		return nil, err
	}
	return &BrowserSnapshotResponse{URL: resp.URL, Title: resp.Title, Elements: resp.Elements, Refs: resp.snapshotRefs}, nil
}
