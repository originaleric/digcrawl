package reddit

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "reddit"
	ContentType  = "post"
	CookieDomain = "reddit.com"
)

// NormalizePostURL normalizes supported Reddit post URLs.
func NormalizePostURL(rawURL string) (*platform.NormalizedURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid reddit url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	pathParts := splitPath(u.Path)
	var id, subreddit string
	switch {
	case host == "redd.it":
		if len(pathParts) >= 1 {
			id = pathParts[0]
		}
	case isRedditHost(host):
		if len(pathParts) >= 4 && strings.EqualFold(pathParts[0], "r") && strings.EqualFold(pathParts[2], "comments") {
			subreddit = pathParts[1]
			id = pathParts[3]
		} else if len(pathParts) >= 2 && strings.EqualFold(pathParts[0], "comments") {
			id = pathParts[1]
		}
	default:
		return nil, fmt.Errorf("unsupported reddit host %q", host)
	}
	id = strings.TrimPrefix(strings.TrimSpace(id), "t3_")
	if id == "" {
		return nil, fmt.Errorf("unsupported reddit post url %q", rawURL)
	}
	canonical := "https://www.reddit.com/comments/" + id + "/"
	if subreddit != "" {
		canonical = fmt.Sprintf("https://www.reddit.com/r/%s/comments/%s/", subreddit, id)
	}
	return &platform.NormalizedURL{
		OriginalURL:  rawURL,
		ResolvedURL:  rawURL,
		CanonicalURL: canonical,
		BrowseURL:    canonical,
		PlatformID:   id,
		ContentKind:  ContentType,
		CookieDomain: CookieDomain,
		QueryParams:  platform.ExtractQueryParams(u.Query(), "context", "sort"),
	}, nil
}

func IsPostURL(rawURL string) bool {
	_, err := NormalizePostURL(rawURL)
	return err == nil
}

func IsRedditHost(rawURL string) bool {
	value := strings.TrimSpace(rawURL)
	u, err := url.Parse(value)
	if err == nil && u.Hostname() != "" {
		return isRedditHost(strings.ToLower(u.Hostname()))
	}
	return isRedditHost(strings.ToLower(value))
}

func isRedditHost(host string) bool {
	switch host {
	case "reddit.com", "www.reddit.com", "old.reddit.com", "new.reddit.com", "redd.it":
		return true
	default:
		return strings.HasSuffix(host, ".reddit.com")
	}
}

func splitPath(path string) []string {
	raw := strings.Split(strings.Trim(path, "/"), "/")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeSubredditName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "r/")
	v = strings.Trim(v, "/")
	return v
}

func normalizeUsername(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "u/")
	v = strings.TrimPrefix(v, "user/")
	v = strings.Trim(v, "/")
	return v
}
