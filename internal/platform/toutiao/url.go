package toutiao

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

var (
	articleIDPattern = regexp.MustCompile(`(?i)/article/(\d+)`)
	groupIDPattern   = regexp.MustCompile(`(?i)/group/(\d+)`)
	wIDPattern       = regexp.MustCompile(`(?i)/w/(\d+)`)
	shortPathPattern = regexp.MustCompile(`(?i)/is/([A-Za-z0-9_-]+)`)
	toutiaoHosts     = []string{"toutiao.com", "www.toutiao.com", "m.toutiao.com"}
)

// NormalizeURL 归一化头条文章 URL（含 m.toutiao.com 短链 resolve）。
func NormalizeURL(ctx context.Context, rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	out := &platform.NormalizedURL{
		OriginalURL:  rawURL,
		ResolvedURL:  rawURL,
		CookieDomain: CookieDomain,
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效 url: %w", err)
	}
	host := strings.ToLower(u.Hostname())

	if hostMatches(host, []string{"m.toutiao.com"}) && shortPathPattern.MatchString(u.Path) {
		resolved, err := platform.ResolveWithBrowserFallback(ctx, rawURL, toutiaoHosts)
		if err != nil {
			return nil, fmt.Errorf("头条短链解析失败: %w", err)
		}
		out.ResolvedURL = resolved
		out.RiskFlags = append(out.RiskFlags, "shortlink_resolved")
		u, err = url.Parse(resolved)
		if err != nil {
			return nil, err
		}
		host = strings.ToLower(u.Hostname())
	}

	if !hostMatches(host, toutiaoHosts) {
		return nil, fmt.Errorf("非头条域名: %s", host)
	}

	id, kind := extractArticleID(u.Path)
	out.ContentKind = kind
	if id != "" {
		out.PlatformID = id
		if kind == "group" {
			out.CanonicalURL = fmt.Sprintf("https://www.toutiao.com/group/%s/", id)
		} else {
			out.CanonicalURL = fmt.Sprintf("https://www.toutiao.com/article/%s/", id)
		}
		out.BrowseURL = out.ResolvedURL
		if out.BrowseURL == "" || out.BrowseURL == out.OriginalURL {
			out.BrowseURL = out.CanonicalURL
		}
	} else {
		out.RiskFlags = append(out.RiskFlags, "article_id_unknown")
		out.CanonicalURL = out.ResolvedURL
		out.BrowseURL = out.ResolvedURL
	}
	return out, nil
}

func extractArticleID(path string) (id, kind string) {
	if m := wIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], ContentType
	}
	if m := groupIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "group"
	}
	if m := articleIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], ContentType
	}
	return "", ContentType
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
