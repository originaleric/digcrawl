package watchlist

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type FeedRegistry struct {
	Client *http.Client
}

func (r FeedRegistry) ListLatest(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	switch {
	case platform == "rss" || typ == "rss" || typ == "feed":
		return r.readRSS(ctx, item)
	case typ == "sitemap" || platform == "sitemap":
		return r.readSitemap(ctx, item)
	case platform == "toutiao":
		return r.readHTMLLinks(ctx, item, "toutiao")
	case platform == "douyin":
		return r.readHTMLLinks(ctx, item, "douyin")
	case platform == "web":
		if strings.Contains(strings.ToLower(item.URL), "sitemap") {
			return r.readSitemap(ctx, item)
		}
		return r.readRSS(ctx, item)
	default:
		return nil, fmt.Errorf("unsupported watchlist platform %q", item.Platform)
	}
}

func (r FeedRegistry) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (r FeedRegistry) get(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 DigCrawl Watchlist")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", fmt.Errorf("RATE_LIMITED: feed request returned 429")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, "", fmt.Errorf("LOGIN_REQUIRED: feed request returned %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("feed request returned %d", resp.StatusCode)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (r FeedRegistry) readRSS(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	data, _, err := r.get(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	var rss struct {
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
				Link  string `xml:"link"`
				GUID  string `xml:"guid"`
				Date  string `xml:"pubDate"`
			} `xml:"item"`
		} `xml:"channel"`
		Entries []struct {
			Title string `xml:"title"`
			ID    string `xml:"id"`
			Link  []struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
			Updated string `xml:"updated"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	var out []FeedItem
	for _, it := range rss.Channel.Items {
		link := strings.TrimSpace(it.Link)
		if link == "" {
			continue
		}
		out = append(out, FeedItem{
			ID: it.GUID, ContentKey: firstNonEmpty(it.GUID, link), URL: link, CanonicalURL: link,
			Title: strings.TrimSpace(it.Title), PublishedAt: parseTime(it.Date), ContentType: "article",
		})
	}
	for _, it := range rss.Entries {
		link := ""
		if len(it.Link) > 0 {
			link = strings.TrimSpace(it.Link[0].Href)
		}
		if link == "" {
			continue
		}
		out = append(out, FeedItem{
			ID: it.ID, ContentKey: firstNonEmpty(it.ID, link), URL: link, CanonicalURL: link,
			Title: strings.TrimSpace(it.Title), PublishedAt: parseTime(it.Updated), ContentType: "article",
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no rss items found")
	}
	return out, nil
}

func (r FeedRegistry) readSitemap(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	data, _, err := r.get(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	var sm struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}
	var out []FeedItem
	for _, u := range sm.URLs {
		link := strings.TrimSpace(u.Loc)
		if link == "" {
			continue
		}
		out = append(out, FeedItem{ContentKey: link, URL: link, CanonicalURL: link, PublishedAt: parseTime(u.LastMod), ContentType: "web"})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no sitemap urls found")
	}
	return out, nil
}

var hrefRe = regexp.MustCompile(`(?i)href=["']([^"']+)["']`)

func (r FeedRegistry) readHTMLLinks(ctx context.Context, item WatchItem, platform string) ([]FeedItem, error) {
	data, _, err := r.get(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	html := string(data)
	lower := strings.ToLower(html)
	if strings.Contains(lower, "captcha") || strings.Contains(lower, "验证") {
		return nil, fmt.Errorf("CAPTCHA_REQUIRED: platform page requires verification")
	}
	if strings.Contains(lower, "login") && strings.Contains(lower, "password") {
		return nil, fmt.Errorf("LOGIN_REQUIRED: platform page requires login")
	}
	base, _ := url.Parse(item.URL)
	seen := map[string]bool{}
	var out []FeedItem
	for _, m := range hrefRe.FindAllStringSubmatch(html, -1) {
		link := normalizeLink(base, m[1])
		if link == "" || seen[link] {
			continue
		}
		if !platformLink(platform, link) {
			continue
		}
		seen[link] = true
		out = append(out, FeedItem{
			ID: hashKey(link), ContentKey: link, URL: link, CanonicalURL: link,
			Title: item.Name, ContentType: contentTypeFor(platform, link),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: no %s content links found", platform)
	}
	return out, nil
}

func normalizeLink(base *url.URL, raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\u0026", "&"))
	if raw == "" || strings.HasPrefix(raw, "javascript:") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	u.Fragment = ""
	return u.String()
}

func platformLink(platform, link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	switch platform {
	case "toutiao":
		return strings.Contains(host, "toutiao.com") && (strings.Contains(path, "/article/") || strings.Contains(path, "/group/"))
	case "douyin":
		return strings.Contains(host, "douyin.com") && strings.Contains(path, "/video/")
	default:
		return true
	}
}

func contentTypeFor(platform, link string) string {
	if platform == "douyin" || strings.Contains(link, "/video/") {
		return "video"
	}
	return "article"
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func hashKey(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
