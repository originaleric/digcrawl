package platform

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultResolveTimeout = 15 * time.Second

// ResolveHTTPFollow 跟随 HTTP 重定向解析短链，并校验最终 host 在 allowed 列表中。
func ResolveHTTPFollow(ctx context.Context, rawURL string, allowedHosts []string) (resolved string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url 不能为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("无效 url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("仅支持 http/https")
	}

	client := &http.Client{
		Timeout: defaultResolveTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			return nil
		},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "DigCrawl/1.0 (+https://github.com/originaleric/digcrawl)")

	resp, err := client.Do(req)
	if err != nil {
		// 部分站点不支持 HEAD，回退 GET（只读首包由 Client 处理完整 body，可接受）
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "DigCrawl/1.0 (+https://github.com/originaleric/digcrawl)")
		resp, err = client.Do(req)
		if err != nil {
			return "", fmt.Errorf("短链解析失败: %w", err)
		}
	}
	defer resp.Body.Close()

	final := resp.Request.URL.String()
	if final == "" {
		final = rawURL
	}
	host := strings.ToLower(resp.Request.URL.Hostname())
	if !hostAllowed(host, allowedHosts) {
		return "", fmt.Errorf("短链跳转目标 %s 不在允许域名列表", host)
	}
	return final, nil
}

func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
