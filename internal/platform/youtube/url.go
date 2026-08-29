package youtube

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "youtube"
	ContentType  = "video"
	CookieDomain = "www.youtube.com"
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// NormalizeURL accepts public YouTube video, Shorts, embed, live and youtu.be links.
// Channel, playlist, search, and other non-video pages intentionally remain outside
// this detail reader's boundary.
func NormalizeURL(rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme %q", u.Scheme)
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if !isYouTubeHost(host) {
		return nil, fmt.Errorf("not a YouTube domain: %s", host)
	}

	out := &platform.NormalizedURL{
		OriginalURL:  rawURL,
		ResolvedURL:  rawURL,
		CookieDomain: CookieDomain,
		ContentKind:  "non_video",
		BrowseURL:    rawURL,
		CanonicalURL: rawURL,
	}
	id := extractVideoID(host, u)
	if id == "" {
		out.RiskFlags = append(out.RiskFlags, "video_id_unknown")
		return out, nil
	}
	out.PlatformID = id
	out.ContentKind = ContentType
	out.CanonicalURL = "https://www.youtube.com/watch?v=" + id
	out.BrowseURL = out.CanonicalURL
	return out, nil
}

func IsVideoURL(rawURL string) bool {
	norm, err := NormalizeURL(rawURL)
	return err == nil && norm.ContentKind == ContentType && norm.PlatformID != ""
}

func isYouTubeHost(host string) bool {
	return host == "youtu.be" || host == "www.youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
}

func extractVideoID(host string, u *url.URL) string {
	if u == nil {
		return ""
	}
	var id string
	if host == "youtu.be" || host == "www.youtu.be" {
		id = strings.Split(strings.Trim(u.Path, "/"), "/")[0]
	} else {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case strings.EqualFold(u.Path, "/watch"):
			id = u.Query().Get("v")
		case len(parts) >= 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live"):
			id = parts[1]
		}
	}
	id = strings.TrimSpace(id)
	if !videoIDPattern.MatchString(id) {
		return ""
	}
	return id
}
