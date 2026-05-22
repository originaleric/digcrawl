package x

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

var statusPattern = regexp.MustCompile(`(?i)/([^/]+)/status/(\d+)`)

// NormalizeURL 归一化 X/Twitter 帖子 URL（twitter.com → x.com canonical）。
func NormalizeURL(rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("无效 url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host != "x.com" && host != "www.x.com" && host != "twitter.com" && host != "www.twitter.com" && !strings.HasSuffix(host, ".twitter.com") {
		return nil, fmt.Errorf("非 X/Twitter 域名: %s", host)
	}

	out := &platform.NormalizedURL{
		OriginalURL:  rawURL,
		ResolvedURL:  rawURL,
		CookieDomain: CookieDomain,
		ContentKind:  ContentType,
	}
	if host == "www.x.com" || host == "twitter.com" || host == "www.twitter.com" || strings.HasSuffix(host, ".twitter.com") {
		out.RiskFlags = append(out.RiskFlags, "twitter_host_normalized")
	}

	m := statusPattern.FindStringSubmatch(u.Path)
	if len(m) >= 3 {
		user := m[1]
		id := m[2]
		out.PlatformID = id
		out.CanonicalURL = fmt.Sprintf("https://x.com/%s/status/%s", user, id)
	} else {
		out.RiskFlags = append(out.RiskFlags, "status_id_unknown")
		if host == "www.x.com" || host == "twitter.com" || host == "www.twitter.com" {
			u.Host = "x.com"
		}
		out.CanonicalURL = u.String()
	}
	return out, nil
}
