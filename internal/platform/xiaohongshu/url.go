package xiaohongshu

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

var (
	noteIDPattern = regexp.MustCompile(`(?i)(?:/explore/|/discovery/item/)([a-f0-9]{16,32})`)
	xhsHosts      = []string{"xiaohongshu.com", "www.xiaohongshu.com"}
	xhslinkHosts  = []string{"xhslink.com", "www.xhslink.com"}
)

// NormalizeURL 归一化小红书笔记 URL（含 xhslink 短链 resolve）。
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

	if platformHost(host, xhslinkHosts) {
		resolved, err := platform.ResolveHTTPFollow(ctx, rawURL, xhsHosts)
		if err != nil {
			return nil, fmt.Errorf("xhslink 解析失败: %w", err)
		}
		out.ResolvedURL = resolved
		out.RiskFlags = append(out.RiskFlags, "shortlink_resolved")
		u, err = url.Parse(resolved)
		if err != nil {
			return nil, err
		}
		host = strings.ToLower(u.Hostname())
	}

	if !platformHost(host, xhsHosts) {
		return nil, fmt.Errorf("非小红书域名: %s", host)
	}

	id := extractNoteID(u.Path)
	if id == "" {
		out.RiskFlags = append(out.RiskFlags, "note_id_unknown")
		out.CanonicalURL = out.ResolvedURL
	} else {
		out.PlatformID = id
		out.CanonicalURL = fmt.Sprintf("https://www.xiaohongshu.com/explore/%s", id)
	}
	out.ContentKind = detectContentKind(u.Path, "")
	return out, nil
}

func extractNoteID(path string) string {
	m := noteIDPattern.FindStringSubmatch(path)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func detectContentKind(path, metaKind string) string {
	if k := strings.TrimSpace(strings.ToLower(metaKind)); k != "" {
		if strings.Contains(k, "video") {
			return "video"
		}
	}
	if strings.Contains(strings.ToLower(path), "/video/") {
		return "video"
	}
	return ContentType
}

func platformHost(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
