package kuaishou

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "kuaishou"
	ContentType  = "video"
	CookieDomain = "www.kuaishou.com"
)

var (
	videoPattern = regexp.MustCompile(`(?i)/(?:short-video|photo)/([0-9A-Za-z_-]+)`)
	userPattern  = regexp.MustCompile(`(?i)/(?:profile|user/profile)/([0-9A-Za-z_-]+)`)
	ksHosts      = []string{"kuaishou.com", "www.kuaishou.com", "v.kuaishou.com"}
	ksShortHosts = []string{"v.kuaishou.com"}
)

func NormalizeURL(ctx context.Context, rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效 url: %w", err)
	}
	out := &platform.NormalizedURL{OriginalURL: rawURL, ResolvedURL: rawURL, CookieDomain: CookieDomain, ContentKind: ContentType}
	host := strings.ToLower(u.Hostname())

	if hostMatches(host, ksShortHosts) {
		resolved, err := platform.ResolveWithBrowserFallback(ctx, rawURL, []string{"kuaishou.com", "www.kuaishou.com"})
		if err != nil {
			return nil, fmt.Errorf("快手短链解析失败: %w", err)
		}
		out.ResolvedURL = resolved
		out.RiskFlags = append(out.RiskFlags, "shortlink_resolved")
		u, err = url.Parse(resolved)
		if err != nil {
			return nil, err
		}
		host = strings.ToLower(u.Hostname())
	}

	if !hostMatches(host, ksHosts) {
		return nil, fmt.Errorf("非快手域名: %s", host)
	}
	if id := extractVideoID(u.Path); id != "" {
		out.PlatformID = id
		out.CanonicalURL = "https://www.kuaishou.com/short-video/" + id
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	if id := extractUserID(u.Path); id != "" {
		out.PlatformID = id
		out.ContentKind = "user"
		out.CanonicalURL = "https://www.kuaishou.com/profile/" + id
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	out.ContentKind = "user"
	out.CanonicalURL = rawURL
	out.BrowseURL = rawURL
	out.RiskFlags = append(out.RiskFlags, "platform_id_unknown")
	return out, nil
}

func extractVideoID(path string) string {
	m := videoPattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func extractUserID(path string) string {
	m := userPattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
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
