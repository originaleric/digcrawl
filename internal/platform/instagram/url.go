package instagram

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "instagram"
	CookieDomain = "www.instagram.com"
)

var mediaPathPattern = regexp.MustCompile(`(?i)^/(p|reel|tv)/([A-Za-z0-9_-]+)`)

func NormalizeURL(rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid instagram url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host != "instagram.com" && host != "www.instagram.com" {
		return nil, fmt.Errorf("unsupported instagram host %q", host)
	}
	u.Scheme = "https"
	u.Host = CookieDomain
	u.Fragment = ""
	out := &platform.NormalizedURL{OriginalURL: rawURL, ResolvedURL: u.String(), CookieDomain: CookieDomain}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if m := mediaPathPattern.FindStringSubmatch(path); len(m) >= 3 {
		kind := strings.ToLower(m[1])
		code := m[2]
		out.PlatformID = code
		out.ContentKind = kind
		if kind == "tv" {
			kind = "p"
		}
		out.CanonicalURL = "https://www.instagram.com/" + kind + "/" + code + "/"
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 1 && isUsername(parts[0]) {
		user := parts[0]
		out.PlatformID = user
		out.ContentKind = "user"
		out.CanonicalURL = "https://www.instagram.com/" + user + "/"
		out.BrowseURL = out.CanonicalURL
		return out, nil
	}
	out.CanonicalURL = u.String()
	out.BrowseURL = out.CanonicalURL
	return out, nil
}

func IsMediaURL(rawURL string) bool {
	norm, err := NormalizeURL(rawURL)
	return err == nil && (norm.ContentKind == "p" || norm.ContentKind == "reel" || norm.ContentKind == "tv") && strings.TrimSpace(norm.PlatformID) != ""
}

func UsernameFromURL(rawURL string) string {
	norm, err := NormalizeURL(rawURL)
	if err != nil || norm.ContentKind != "user" {
		return ""
	}
	return norm.PlatformID
}

func isUsername(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "_") {
		return false
	}
	switch strings.ToLower(v) {
	case "p", "reel", "tv", "accounts", "explore", "direct", "stories":
		return false
	}
	return true
}
