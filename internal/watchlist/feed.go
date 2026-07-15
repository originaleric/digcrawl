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
	"strconv"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/reddit"
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
	case platform == "reddit":
		return r.readReddit(ctx, item)
	case platform == "web":
		if strings.Contains(strings.ToLower(item.URL), "sitemap") {
			return r.readSitemap(ctx, item)
		}
		return r.readRSS(ctx, item)
	default:
		return nil, fmt.Errorf("unsupported watchlist platform %q", item.Platform)
	}
}

func (r FeedRegistry) readReddit(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	switch inferRedditWatchlistType(item) {
	case "reddit_subreddit", "subreddit", "community":
		return r.readRedditSubreddit(ctx, item)
	case "reddit_user", "user":
		return r.readRedditUser(ctx, item)
	case "reddit_post_comments", "post_comments", "comments":
		return r.readRedditPostComments(ctx, item)
	default:
		return nil, fmt.Errorf("unsupported reddit watchlist type %q", item.Type)
	}
}

func inferRedditWatchlistType(item WatchItem) string {
	typ := strings.ToLower(strings.TrimSpace(item.Type))
	if typ != "" {
		return typ
	}
	urlLower := strings.ToLower(item.URL)
	switch {
	case reddit.IsPostURL(item.URL):
		return "reddit_post_comments"
	case optionString(item.Options, "subreddit") != "" || strings.Contains(urlLower, "/r/"):
		return "reddit_subreddit"
	case optionString(item.Options, "username") != "" || strings.Contains(urlLower, "/user/") || strings.Contains(urlLower, "/u/"):
		return "reddit_user"
	default:
		return ""
	}
}

func (r FeedRegistry) readRedditSubreddit(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	sub := firstNonEmpty(optionString(item.Options, "subreddit"), redditSubredditFromURL(item.URL))
	if sub == "" {
		return nil, fmt.Errorf("subreddit or subreddit url is required")
	}
	res, err := reddit.ReadSubreddit(ctx, reddit.ListingOptions{
		Subreddit: sub,
		Sort:      optionString(item.Options, "sort"),
		TimeRange: optionString(item.Options, "time_range"),
		Limit:     optionInt(item.Options, "limit"),
		After:     optionString(item.Options, "after"),
	})
	if err != nil {
		return nil, err
	}
	return feedItemsFromRedditListing(res), nil
}

func (r FeedRegistry) readRedditUser(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	user := firstNonEmpty(optionString(item.Options, "username"), redditUsernameFromURL(item.URL))
	if user == "" {
		return nil, fmt.Errorf("username or reddit user url is required")
	}
	res, err := reddit.ReadUser(ctx, reddit.ListingOptions{
		Username: user,
		Where:    firstNonEmpty(optionString(item.Options, "where"), optionString(item.Options, "sort")),
		Limit:    optionInt(item.Options, "limit"),
		After:    optionString(item.Options, "after"),
	})
	if err != nil {
		return nil, err
	}
	return feedItemsFromRedditListing(res), nil
}

func (r FeedRegistry) readRedditPostComments(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	if !reddit.IsPostURL(item.URL) {
		return nil, fmt.Errorf("reddit post url is required")
	}
	limit := optionInt(item.Options, "comment_limit")
	if limit <= 0 {
		limit = optionInt(item.Options, "limit")
	}
	if limit <= 0 {
		limit = 1
	}
	depth := optionInt(item.Options, "comment_depth")
	content, err := reddit.ReadPost(ctx, reddit.ReadInput{
		ReadInput:       platform.ReadInput{URL: item.URL},
		IncludeComments: true,
		CommentLimit:    limit,
		CommentDepth:    depth,
		CommentSort:     firstNonEmpty(optionString(item.Options, "comment_sort"), optionString(item.Options, "sort"), "new"),
		ExpandMore:      optionBool(item.Options, "expand_more"),
		ExpandMoreLimit: optionInt(item.Options, "expand_more_limit"),
	})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: reddit post response is empty")
	}
	key := "reddit:comments:" + firstNonEmpty(platform.MetaString(content.PlatformMetadata, "fullname"), content.CanonicalURL, item.URL)
	latest := latestRedditComment(content.PlatformMetadata)
	if latest.ID != "" {
		key += ":t1_" + latest.ID
	}
	return []FeedItem{{
		ID:           key,
		ContentKey:   key,
		URL:          item.URL,
		CanonicalURL: firstNonEmpty(content.CanonicalURL, item.URL),
		Title:        content.Title,
		Author:       commentAuthorOrPostAuthor(latest, content),
		PublishedAt:  timeFromUnix(latest.CreatedUTC),
		ContentType:  "reddit_post_comments",
		Raw: map[string]any{
			"latest_comment_id": latest.ID,
			"latest_comment":    latest.Body,
			"post_title":        content.Title,
		},
	}}, nil
}

func feedItemsFromRedditListing(res *reddit.ListingResult) []FeedItem {
	if res == nil {
		return nil
	}
	out := make([]FeedItem, 0, len(res.Items))
	for _, it := range res.Items {
		link := firstNonEmpty(it.CanonicalURL, it.URL)
		if link == "" {
			continue
		}
		contentType := "reddit_post"
		if it.Kind == "t1" {
			contentType = "reddit_comment"
		}
		out = append(out, FeedItem{
			ID:           firstNonEmpty(it.Fullname, it.ID, hashKey(link)),
			ContentKey:   redditListingContentKey(it, link),
			URL:          link,
			CanonicalURL: link,
			Title:        it.Title,
			Author:       it.Author,
			PublishedAt:  timeFromUnix(it.CreatedUTC),
			ContentType:  contentType,
			Raw: map[string]any{
				"subreddit":    it.Subreddit,
				"score":        it.Score,
				"num_comments": it.NumComments,
				"kind":         it.Kind,
				"body":         it.Body,
			},
		})
	}
	return out
}

func redditListingContentKey(it reddit.ListingItem, link string) string {
	if it.ContentKey != "" {
		return it.ContentKey
	}
	if it.Fullname != "" {
		return "reddit:" + it.Fullname
	}
	return link
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

func optionString(opts map[string]any, key string) string {
	if opts == nil {
		return ""
	}
	v, ok := opts[key]
	if !ok {
		return ""
	}
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func optionInt(opts map[string]any, key string) int {
	if opts == nil {
		return 0
	}
	v, ok := opts[key]
	if !ok {
		return 0
	}
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		return 0
	}
}

func optionBool(opts map[string]any, key string) bool {
	if opts == nil {
		return false
	}
	v, ok := opts[key]
	if !ok {
		return false
	}
	switch value := v.(type) {
	case bool:
		return value
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(value))
		return b
	default:
		return false
	}
}

func redditSubredditFromURL(rawURL string) string {
	parts := redditPathParts(rawURL)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "r") {
		return parts[1]
	}
	return ""
}

func redditUsernameFromURL(rawURL string) string {
	parts := redditPathParts(rawURL)
	if len(parts) >= 2 && (strings.EqualFold(parts[0], "user") || strings.EqualFold(parts[0], "u")) {
		return parts[1]
	}
	return ""
}

func redditPathParts(rawURL string) []string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func latestRedditComment(meta map[string]any) reddit.RedditComment {
	if meta == nil {
		return reddit.RedditComment{}
	}
	comments, ok := meta["comments_tree"].([]reddit.RedditComment)
	if !ok || len(comments) == 0 {
		return reddit.RedditComment{}
	}
	var latest reddit.RedditComment
	var walk func([]reddit.RedditComment)
	walk = func(items []reddit.RedditComment) {
		for _, c := range items {
			if c.CreatedUTC > latest.CreatedUTC || latest.ID == "" {
				latest = c
			}
			walk(c.Replies)
		}
	}
	walk(comments)
	return latest
}

func commentAuthorOrPostAuthor(comment reddit.RedditComment, content *platform.Content) string {
	if comment.Author != "" {
		return comment.Author
	}
	if content != nil && content.Author != nil {
		return content.Author.Name
	}
	return ""
}

func timeFromUnix(ts int64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0).UTC()
}
