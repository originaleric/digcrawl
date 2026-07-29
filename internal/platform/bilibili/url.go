package bilibili

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "bilibili"
	ContentType  = "video"
	CookieDomain = "www.bilibili.com"
)

var (
	videoBVPattern = regexp.MustCompile(`(?i)/video/(BV[0-9A-Za-z]+)`)
	videoAVPattern = regexp.MustCompile(`(?i)/video/av(\d+)`)
	spacePattern   = regexp.MustCompile(`(?i)^/([0-9]+)(?:/.*)?$`)
	biliHosts      = []string{"bilibili.com", "www.bilibili.com", "m.bilibili.com", "space.bilibili.com", "b23.tv"}
	biliShortHosts = []string{"b23.tv"}
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
	out := &platform.NormalizedURL{
		OriginalURL:  rawURL,
		ResolvedURL:  rawURL,
		CookieDomain: CookieDomain,
		ContentKind:  ContentType,
	}
	host := strings.ToLower(u.Hostname())

	if hostMatches(host, biliShortHosts) {
		resolved, err := platform.ResolveWithBrowserFallback(ctx, rawURL, []string{"bilibili.com", "www.bilibili.com", "m.bilibili.com", "space.bilibili.com"})
		if err != nil {
			return nil, fmt.Errorf("B站短链解析失败: %w", err)
		}
		out.ResolvedURL = resolved
		out.RiskFlags = append(out.RiskFlags, "shortlink_resolved")
		u, err = url.Parse(resolved)
		if err != nil {
			return nil, err
		}
		host = strings.ToLower(u.Hostname())
	}

	if !hostMatches(host, biliHosts) {
		return nil, fmt.Errorf("非 B站域名: %s", host)
	}
	out.QueryParams = platform.ExtractQueryParams(u.Query(), "spm_id_from")
	if strings.EqualFold(host, "space.bilibili.com") {
		if mid := extractSpaceMID(u.Path); mid != "" {
			out.PlatformID = mid
			out.ContentKind = "user"
			out.CanonicalURL = "https://space.bilibili.com/" + mid
			out.BrowseURL = out.CanonicalURL
			return out, nil
		}
	}
	if id, kind := extractVideoID(u.Path); id != "" {
		out.PlatformID = id
		out.ContentKind = kind
		if kind == "av" {
			out.CanonicalURL = "https://www.bilibili.com/video/av" + id
		} else {
			out.CanonicalURL = "https://www.bilibili.com/video/" + id
		}
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	out.ContentKind = "user"
	out.CanonicalURL = rawURL
	out.BrowseURL = rawURL
	out.RiskFlags = append(out.RiskFlags, "platform_id_unknown")
	return out, nil
}

func extractVideoID(path string) (id, kind string) {
	if m := videoBVPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "bv"
	}
	if m := videoAVPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "av"
	}
	return "", ""
}

func extractSpaceMID(path string) string {
	m := spacePattern.FindStringSubmatch(strings.TrimSpace(path))
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
