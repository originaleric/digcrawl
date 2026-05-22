package session

import (
	"context"

	"github.com/originaleric/digcrawl/internal/browser"
)

// FetchJSON 在浏览器会话中执行 API fetch（签名/Cookie 由页面上下文提供）。
func FetchJSON(ctx context.Context, req *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
	return browser.PageFetchJSON(ctx, req)
}
