package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/diggate/config"
	"github.com/originaleric/diggate/internal/browser"
)

const defaultBrowserResolveTimeout = 45 * time.Second

// ResolveWithBrowserFallback 先 HTTP follow，失败时用浏览器导航解析最终 URL。
func ResolveWithBrowserFallback(ctx context.Context, rawURL string, allowedHosts []string) (string, error) {
	resolved, err := ResolveHTTPFollow(ctx, rawURL, allowedHosts)
	if err == nil {
		return resolved, nil
	}
	browserResolved, berr := resolveViaBrowser(ctx, rawURL, allowedHosts)
	if berr != nil {
		return "", fmt.Errorf("http: %v; browser: %w", err, berr)
	}
	return browserResolved, nil
}

func resolveViaBrowser(ctx context.Context, rawURL string, allowedHosts []string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url 不能为空")
	}

	timeout := defaultBrowserResolveTimeout
	if cfg := config.Get(); cfg != nil && cfg.LocalBrowser.NavigateTimeoutSec > 0 {
		timeout = time.Duration(cfg.LocalBrowser.NavigateTimeoutSec)*time.Second + 15*time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := browser.BrowserBrowse(ctx, &browser.BrowserBrowseRequest{
		URL:    rawURL,
		Action: "read",
		Mode:   "summary",
	})
	if err != nil {
		return "", err
	}
	final := strings.TrimSpace(resp.URL)
	if final == "" {
		return "", fmt.Errorf("浏览器未返回最终 URL")
	}
	u, err := parseURLHost(final)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Hostname())
	if !hostAllowed(host, allowedHosts) {
		return "", fmt.Errorf("浏览器跳转目标 %s 不在允许域名列表", host)
	}
	return final, nil
}

func parseURLHost(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
