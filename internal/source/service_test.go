package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform/reddit"
)

func TestServiceReadsRSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a</link><guid>g1</guid><pubDate>Wed, 09 Jul 2026 10:00:00 +0800</pubDate></item></channel></rss>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "w1", Platform: "rss", Type: "rss", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "g1" || items[0].Title != "A" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestServiceExtractsToutiaoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.toutiao.com/article/z/">A</a><a href="https://www.toutiao.com/article/a/">B</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "w1", Platform: "toutiao", Type: "author", URL: srv.URL})
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

func TestServiceExtractsToutiaoHydrationLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<script>{"url":"https:\/\/www.toutiao.com\/group\/7658228232710144575\/","url2":"https:\/\/www.toutiao.com\/article\/7658204565203960362\/"}</script>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "w1", Platform: "toutiao", Type: "author", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 links, got %+v", items)
	}
	if items[0].URL != "https://www.toutiao.com/group/7658228232710144575/" {
		t.Fatalf("unexpected first URL: %+v", items)
	}
}

func TestServiceReadsFixedToutiaoArticle(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "tt1",
		Platform: "toutiao",
		Type:     "article",
		URL:      "https://www.toutiao.com/w/1870280855070795/",
		Name:     "DeepSeek article",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.toutiao.com/article/1870280855070795/" || items[0].ContentType != "article" {
		t.Fatalf("unexpected toutiao item: %+v", items[0])
	}
}

func TestServiceReadsFixedXPost(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "x1",
		Platform: "x",
		Type:     "x_post",
		URL:      "https://twitter.com/elonmusk/status/12345?s=20",
		Name:     "Elon post",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://x.com/elonmusk/status/12345" || items[0].ContentType != "x_post" || items[0].Author != "elonmusk" {
		t.Fatalf("unexpected x item: %+v", items[0])
	}
}

func TestServiceExtractsXStatusLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://x.com/elonmusk/status/12345">A</a><script>{"url":"https:\/\/x.com\/elonmusk\/status\/67890"}</script></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "x1", Platform: "x", Type: "x_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 x status links, got %+v", items)
	}
	if items[0].ContentType != "x_post" || items[0].URL != "https://x.com/elonmusk/status/12345" {
		t.Fatalf("unexpected first x item: %+v", items[0])
	}
}

func TestItemsFromRedditListingSupportsPostsAndComments(t *testing.T) {
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
	item := Source{Platform: "reddit", URL: "https://www.reddit.com/r/golang/comments/abc123/title/"}
	if got := inferRedditWatchlistType(item); got != "reddit_post_comments" {
		t.Fatalf("type = %q", got)
	}
}
