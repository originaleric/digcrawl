package watchlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
