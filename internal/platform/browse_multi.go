package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/browser"
)

// BrowseAttempt 单次浏览器采集参数。
type BrowseAttempt struct {
	WaitSelector    string
	ContentSelector string
}

// BrowseWithFallbacks 按多组 selector 依次尝试 Browse，返回首个成功结果。
func BrowseWithFallbacks(
	ctx context.Context,
	url string,
	in ReadInput,
	base BrowseSpec,
	attempts []BrowseAttempt,
) (*browser.BrowserBrowseResponse, error) {
	if len(attempts) == 0 {
		resp, err := Browse(ctx, url, in, base)
		return resp, err
	}
	var lastErr error
	for _, att := range attempts {
		spec := base
		if strings.TrimSpace(att.WaitSelector) != "" {
			spec.WaitSelector = att.WaitSelector
		}
		if strings.TrimSpace(att.ContentSelector) != "" {
			spec.ContentSelector = att.ContentSelector
		}
		resp, err := Browse(ctx, url, in, spec)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("未能读取页面内容")
}
