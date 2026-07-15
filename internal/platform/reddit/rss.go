package reddit

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/feed"
	"github.com/originaleric/digcrawl/internal/platform"
)

func readPostViaRSS(ctx context.Context, in ReadInput, norm *platform.NormalizedURL, opts PostReadOptions, apiAttempt *platform.APIAttempt) (*platform.Content, int64, error) {
	start := time.Now()
	doc, err := feed.Service{UserAgent: "Mozilla/5.0 DigCrawl Reddit RSS"}.Read(ctx, redditPostRSSURL(norm))
	if err != nil {
		return nil, time.Since(start).Milliseconds(), err
	}
	content, err := mapPostRSS(in, norm, doc, opts, apiAttempt)
	if err != nil {
		return nil, time.Since(start).Milliseconds(), err
	}
	return content, time.Since(start).Milliseconds(), nil
}

func mapPostRSS(in ReadInput, norm *platform.NormalizedURL, doc *feed.Document, opts PostReadOptions, apiAttempt *platform.APIAttempt) (*platform.Content, error) {
	if doc == nil || len(doc.Entries) == 0 {
		return nil, fmt.Errorf("CONTENT_NOT_FOUND: reddit rss response missing entries")
	}
	post := doc.Entries[0]
	title := strings.TrimSpace(post.Title)
	if title == "" {
		title = strings.TrimSpace(doc.Title)
	}
	text := feed.TextFromHTML(firstNonEmpty(post.ContentHTML, post.SummaryHTML))
	if title == "" || text == "" {
		return nil, fmt.Errorf("PARTIAL_CONTENT: reddit rss response missing title/text")
	}
	comments := commentsFromFeedEntries(doc.Entries[1:], opts)
	meta := map[string]any{
		"extraction_source": "rss",
		"platform_id":       norm.PlatformID,
		"fullname":          "t3_" + norm.PlatformID,
		"rss_url":           redditPostRSSURL(norm),
	}
	if len(comments) > 0 {
		meta["comments_tree"] = comments
	}
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        in.URL,
		CanonicalURL:     norm.CanonicalURL,
		Title:            title,
		Text:             text,
		Markdown:         markdownForPost(title, text, comments, nil),
		PublishedAt:      timeToRFC3339(post.PublishedAt),
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: platform.MergeAPIMeta(meta, apiAttempt),
	}
	if post.Author != "" {
		content.Author = &platform.Author{Name: post.Author, ProfileURL: firstNonEmpty(post.AuthorURL, "https://www.reddit.com/user/"+post.Author+"/")}
	}
	return content, nil
}

func commentsFromFeedEntries(entries []feed.Entry, opts PostReadOptions) []RedditComment {
	if !opts.IncludeComments || opts.CommentLimit <= 0 {
		return nil
	}
	limit := opts.CommentLimit
	if limit > len(entries) {
		limit = len(entries)
	}
	out := make([]RedditComment, 0, limit)
	for _, entry := range entries {
		if len(out) >= limit {
			break
		}
		body := feed.TextFromHTML(firstNonEmpty(entry.ContentHTML, entry.SummaryHTML))
		if body == "" {
			continue
		}
		out = append(out, RedditComment{
			ID:         firstNonEmpty(entry.ID, entry.URL),
			Fullname:   firstNonEmpty(entry.ID, entry.URL),
			Author:     entry.Author,
			Body:       body,
			CreatedUTC: unixSeconds(firstNonZeroTime(entry.PublishedAt, entry.UpdatedAt)),
		})
	}
	return out
}

func readSubredditViaRSS(ctx context.Context, opts ListingOptions) (*ListingResult, error) {
	sub := normalizeSubredditName(opts.Subreddit)
	if sub == "" {
		return nil, fmt.Errorf("subreddit is required")
	}
	doc, err := feed.Service{UserAgent: "Mozilla/5.0 DigCrawl Reddit RSS"}.Read(ctx, subredditRSSURL(opts))
	if err != nil {
		return nil, err
	}
	return listingFromFeedDocument(doc, "r/"+sub, normalizeLimit(opts.Limit)), nil
}

func readUserViaRSS(ctx context.Context, opts ListingOptions) (*ListingResult, error) {
	user := normalizeUsername(opts.Username)
	if user == "" {
		return nil, fmt.Errorf("username is required")
	}
	doc, err := feed.Service{UserAgent: "Mozilla/5.0 DigCrawl Reddit RSS"}.Read(ctx, userRSSURL(opts))
	if err != nil {
		return nil, err
	}
	return listingFromFeedDocument(doc, "u/"+user, normalizeLimit(opts.Limit)), nil
}

func listingFromFeedDocument(doc *feed.Document, source string, limit int) *ListingResult {
	res := &ListingResult{}
	if doc == nil {
		return res
	}
	for _, entry := range doc.Entries {
		if limit > 0 && len(res.Items) >= limit {
			break
		}
		link := strings.TrimSpace(entry.URL)
		if link == "" {
			continue
		}
		res.Items = append(res.Items, ListingItem{
			ID:           feedEntryID(entry),
			Kind:         "t3",
			Fullname:     "rss:" + feedEntryID(entry),
			Subreddit:    source,
			Author:       entry.Author,
			Title:        entry.Title,
			Body:         feed.TextFromHTML(firstNonEmpty(entry.ContentHTML, entry.SummaryHTML)),
			URL:          link,
			Permalink:    redditPermalink(link),
			CreatedUTC:   unixSeconds(firstNonZeroTime(entry.PublishedAt, entry.UpdatedAt)),
			ContentKey:   firstNonEmpty(entry.ID, link),
			CanonicalURL: link,
		})
	}
	return res
}

func redditPostRSSURL(norm *platform.NormalizedURL) string {
	return strings.TrimRight(norm.CanonicalURL, "/") + "/.rss"
}

func subredditRSSURL(opts ListingOptions) string {
	sub := normalizeSubredditName(opts.Subreddit)
	sort := normalizeListingSort(opts.Sort)
	u := url.URL{Scheme: "https", Host: "www.reddit.com"}
	u.Path = "/r/" + sub + "/" + sort + "/.rss"
	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", normalizeLimit(opts.Limit)))
	if t := normalizeTimeRange(opts.TimeRange); t != "" && (sort == "top" || sort == "controversial") {
		q.Set("t", t)
	}
	if after := strings.TrimSpace(opts.After); after != "" {
		q.Set("after", after)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func userRSSURL(opts ListingOptions) string {
	user := normalizeUsername(opts.Username)
	where := strings.ToLower(strings.TrimSpace(opts.Where))
	switch where {
	case "comments", "overview", "submitted":
	default:
		where = "submitted"
	}
	u := url.URL{Scheme: "https", Host: "www.reddit.com"}
	u.Path = "/user/" + user + "/" + where + "/.rss"
	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", normalizeLimit(opts.Limit)))
	if after := strings.TrimSpace(opts.After); after != "" {
		q.Set("after", after)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func redditPermalink(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	return u.Path
}

func feedEntryID(entry feed.Entry) string {
	id := firstNonEmpty(entry.ID, entry.URL)
	id = strings.TrimPrefix(id, "https://www.reddit.com")
	id = strings.Trim(id, "/")
	if id == "" {
		return "rss"
	}
	return id
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, v := range values {
		if !v.IsZero() {
			return v
		}
	}
	return time.Time{}
}

func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func timeToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
