package weibo

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "weibo"
	ContentType  = "post"
	CookieDomain = "m.weibo.cn"
)

var (
	detailIDPattern = regexp.MustCompile(`(?i)/(?:detail|status)/([0-9A-Za-z]+)`)
	webPostPattern  = regexp.MustCompile(`(?i)^/([0-9A-Za-z_]+)/([0-9A-Za-z]+)`)
	uIDPattern      = regexp.MustCompile(`(?i)/(?:u|profile)/([0-9]+)`)
	weiboHosts      = []string{"weibo.com", "www.weibo.com", "m.weibo.cn"}
)

func NormalizeURL(_ context.Context, rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效 url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if !hostMatches(host, weiboHosts) {
		return nil, fmt.Errorf("非微博域名: %s", host)
	}
	out := &platform.NormalizedURL{OriginalURL: rawURL, ResolvedURL: rawURL, CookieDomain: CookieDomain, ContentKind: ContentType}
	if uid := extractUserID(u.Path); uid != "" {
		out.PlatformID = uid
		out.ContentKind = "user"
		out.CanonicalURL = "https://m.weibo.cn/u/" + uid
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	if id := extractPostID(u.Path); id != "" {
		out.PlatformID = id
		out.CanonicalURL = "https://m.weibo.cn/detail/" + id
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	out.ContentKind = "user"
	out.CanonicalURL = rawURL
	out.BrowseURL = rawURL
	out.RiskFlags = append(out.RiskFlags, "platform_id_unknown")
	return out, nil
}

func extractPostID(path string) string {
	if m := detailIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1]
	}
	if m := webPostPattern.FindStringSubmatch(path); len(m) >= 3 {
		return m[2]
	}
	return ""
}

func extractUserID(path string) string {
	if m := uIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1]
	}
	return ""
}

func hostMatches(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
