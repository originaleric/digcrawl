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
	videoIDPattern   = regexp.MustCompile(`(?i)/video/(\d+)`)
	noteIDPattern    = regexp.MustCompile(`(?i)/note/(\d+)`)
	sharePattern     = regexp.MustCompile(`(?i)/share/video/(\d+)`)
	pureNumericID    = regexp.MustCompile(`^\d{10,25}$`)
	douyinHosts      = []string{"douyin.com", "www.douyin.com", "iesdouyin.com", "www.iesdouyin.com"}
	shortHosts       = []string{"v.douyin.com"}
)

// NormalizeURL 归一化抖音作品 URL（含短链 resolve、modal_id、纯数字 ID）。
func NormalizeURL(ctx context.Context, rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	if pureNumericID.MatchString(rawURL) {
		return &platform.NormalizedURL{
			OriginalURL:  rawURL,
			ResolvedURL:  rawURL,
			CanonicalURL: fmt.Sprintf("https://www.douyin.com/video/%s", rawURL),
			BrowseURL:    fmt.Sprintf("https://www.douyin.com/video/%s", rawURL),
			PlatformID:   rawURL,
			ContentKind:  ContentType,
			CookieDomain: CookieDomain,
			RiskFlags:    []string{"numeric_id_input"},
		}, nil
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
		resolved, err := platform.ResolveWithBrowserFallback(ctx, rawURL, douyinHosts)
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

	id, kind, fromModal := extractWorkID(u.Path, u.Query())
	out.ContentKind = kind
	if id != "" {
		out.PlatformID = id
		out.CanonicalURL = fmt.Sprintf("https://www.douyin.com/%s/%s", kind, id)
		if fromModal {
			out.BrowseURL = out.ResolvedURL
			if out.BrowseURL == "" {
				out.BrowseURL = out.OriginalURL
			}
		} else {
			out.BrowseURL = out.CanonicalURL
			if out.BrowseURL == "" {
				out.BrowseURL = out.ResolvedURL
			}
		}
		if fromModal {
			out.RiskFlags = append(out.RiskFlags, "modal_id_resolved")
		}
	} else {
		out.RiskFlags = append(out.RiskFlags, "work_id_unknown")
		out.CanonicalURL = out.ResolvedURL
		out.BrowseURL = out.ResolvedURL
	}
	return out, nil
}

func extractWorkID(path string, query url.Values) (id, kind string, fromModal bool) {
	if modalID := strings.TrimSpace(query.Get("modal_id")); modalID != "" && pureNumericID.MatchString(modalID) {
		return modalID, "video", true
	}
	if m := sharePattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "video", false
	}
	if m := videoIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "video", false
	}
	if m := noteIDPattern.FindStringSubmatch(path); len(m) >= 2 {
		return m[1], "note", false
	}
	return "", ContentType, false
}

// extractWorkIDFromURL 从完整 URL 提取作品 ID。
func extractWorkIDFromURL(raw string) (id, kind string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ContentType
	}
	id, kind, _ = extractWorkID(u.Path, u.Query())
	return id, kind
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
