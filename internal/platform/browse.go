package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
)

// Browse 使用本地浏览器按平台规格采集页面。
func Browse(ctx context.Context, url string, in ReadInput, spec BrowseSpec) (*browser.BrowserBrowseResponse, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	action := strings.TrimSpace(spec.Action)
	if action == "" {
		action = "read"
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = spec.CookieDomain
	}
	waitSelector := strings.TrimSpace(in.WaitSelector)
	if waitSelector == "" {
		waitSelector = spec.WaitSelector
	}
	return browser.BrowserBrowse(ctx, &browser.BrowserBrowseRequest{
		URL:               url,
		Action:            action,
		CaptureScreenshot: in.IncludeScreenshot,
		Mode:              "full",
		WaitSelector:      waitSelector,
		ContentSelector:   spec.ContentSelector,
		UseCookieDomain:   cookieDomain,
		MetadataScript:    spec.MetadataScript,
		CaptureMedia:      spec.CaptureMedia,
	})
}
