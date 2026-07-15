package watchlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform/reddit"
)

func TestFeedRegistryReadsRSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a</link><guid>g1</guid><pubDate>Wed, 09 Jul 2026 10:00:00 +0800</pubDate></item></channel></rss>`))
	}))
	defer srv.Close()
	items, err := FeedRegistry{}.ListLatest(context.Background(), WatchItem{ID: "w1", Platform: "rss", Type: "rss", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "g1" || items[0].Title != "A" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestFeedRegistryExtractsToutiaoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.toutiao.com/article/z/">A</a><a href="https://www.toutiao.com/article/a/">B</a></body></html>`))
	}))
	defer srv.Close()
	items, err := FeedRegistry{}.ListLatest(context.Background(), WatchItem{ID: "w1", Platform: "toutiao", Type: "author", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("expected links")
	}
	if items[0].URL != "https://www.toutiao.com/article/z/" {
		t.Fatalf("feed reader should preserve source order, got %+v", items)
	}
}

func TestFeedItemsFromRedditListingSupportsPostsAndComments(t *testing.T) {
	items := feedItemsFromRedditListing(&reddit.ListingResult{Items: []reddit.ListingItem{
		{Kind: "t3", Fullname: "t3_post", Title: "Post", CanonicalURL: "https://www.reddit.com/r/golang/comments/post/", CreatedUTC: 1783576800},
		{Kind: "t1", Fullname: "t1_comment", Title: "Comment", CanonicalURL: "https://www.reddit.com/r/golang/comments/post/title/comment/", CreatedUTC: 1783576900},
	}})
	if len(items) != 2 {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].ContentType != "reddit_post" || items[1].ContentType != "reddit_comment" {
		t.Fatalf("unexpected content types: %+v", items)
	}
	if items[1].ContentKey != "reddit:t1_comment" {
		t.Fatalf("unexpected comment key: %+v", items[1])
	}
}

func TestRedditWatchlistURLParsers(t *testing.T) {
	if got := redditSubredditFromURL("https://www.reddit.com/r/LocalLLaMA/new/"); got != "LocalLLaMA" {
		t.Fatalf("subreddit = %q", got)
	}
	if got := redditUsernameFromURL("https://www.reddit.com/user/example/submitted/"); got != "example" {
		t.Fatalf("username = %q", got)
	}
}

func TestInferRedditWatchlistTypePrefersPostURL(t *testing.T) {
	item := WatchItem{Platform: "reddit", URL: "https://www.reddit.com/r/golang/comments/abc123/title/"}
	if got := inferRedditWatchlistType(item); got != "reddit_post_comments" {
		t.Fatalf("type = %q", got)
	}
}
