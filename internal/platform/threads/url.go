package threads

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "threads"
	CookieDomain = "www.threads.com"
)

var (
	usernamePattern = regexp.MustCompile(`^@([A-Za-z0-9._]+)$`)
	postPathPattern = regexp.MustCompile(`(?i)^/@([A-Za-z0-9._]+)/post/([A-Za-z0-9_-]+)`)
)

func NormalizeURL(rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid threads url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if !IsThreadsHost(host) {
		return nil, fmt.Errorf("unsupported threads host %q", host)
	}
	u.Scheme = "https"
	u.Host = CookieDomain
	u.Fragment = ""
	out := &platform.NormalizedURL{
		OriginalURL:  rawURL,
		ResolvedURL:  u.String(),
		CookieDomain: CookieDomain,
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if m := postPathPattern.FindStringSubmatch(path); len(m) >= 3 {
		user := strings.TrimPrefix(m[1], "@")
		code := m[2]
		out.PlatformID = code
		out.ContentKind = "post"
		out.CanonicalURL = "https://www.threads.com/@" + user + "/post/" + code
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 1 && strings.TrimSpace(parts[0]) != "" {
		first := strings.TrimSpace(parts[0])
		if m := usernamePattern.FindStringSubmatch(first); len(m) >= 2 {
			user := m[1]
			out.PlatformID = user
			out.ContentKind = "user"
			out.CanonicalURL = "https://www.threads.com/@" + user
			out.BrowseURL = out.CanonicalURL
			return out, nil
		}
	}
	out.CanonicalURL = u.String()
	out.BrowseURL = out.CanonicalURL
	return out, nil
}

func IsThreadsHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "threads.net", "www.threads.net", "threads.com", "www.threads.com":
		return true
	default:
		return false
	}
}

func IsPostURL(rawURL string) bool {
	norm, err := NormalizeURL(rawURL)
	return err == nil && norm.ContentKind == "post" && strings.TrimSpace(norm.PlatformID) != ""
}

func UsernameFromURL(rawURL string) string {
	norm, err := NormalizeURL(rawURL)
	if err != nil || norm.ContentKind != "user" {
		return ""
	}
	return norm.PlatformID
}
