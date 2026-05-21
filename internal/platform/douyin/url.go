package douyin

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

var (
	videoIDPattern = regexp.MustCompile(`(?i)/video/(\d+)`)
	noteIDPattern  = regexp.MustCompile(`(?i)/note/(\d+)`)
	sharePattern   = regexp.MustCompile(`(?i)/share/video/(\d+)`)
	douyinHosts    = []string{"douyin.com", "www.douyin.com", "iesdouyin.com", "www.iesdouyin.com"}
	shortHosts     = []string{"v.douyin.com"}
)

// NormalizeURL 归一化抖音作品 URL（含短链 resolve）。
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

	if hostMatches(host, shortHosts) {
		resolved, err := platform.ResolveHTTPFollow(ctx, rawURL, douyinHosts)
		if err != nil {
			return nil, fmt.Errorf("抖音短链解析失败: %w", err)
		}
		out.ResolvedURL = resolved
		out.RiskFlags = append(out.RiskFlags, "shortlink_resolved")
		u, err = url.Parse(resolved)
		if err != nil {
			return nil, err
		}
		host = strings.ToLower(u.Hostname())
	}

	if !hostMatches(host, douyinHosts) {
		return nil, fmt.Errorf("非抖音域名: %s", host)
	}

	id, kind := extractWorkID(u.Path)
	out.ContentKind = kind
	if id != "" {
		out.PlatformID = id
		out.CanonicalURL = fmt.Sprintf("https://www.douyin.com/%s/%s", kind, id)
	} else {
		out.RiskFlags = append(out.RiskFlags, "work_id_unknown")
		out.CanonicalURL = out.ResolvedURL
	}
	return out, nil
}

func extractWorkID(path string) (id, kind string) {
	if m := sharePattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "video"
	}
	if m := videoIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "video"
	}
	if m := noteIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "note"
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
