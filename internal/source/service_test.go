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
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a?utm_source=rss#comments</link><guid>g1</guid><pubDate>Wed, 09 Jul 2026 10:00:00 +0800</pubDate></item></channel></rss>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "w1", Platform: "rss", Type: "rss", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "g1" || items[0].Title != "A" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].URL != "https://example.com/a" {
		t.Fatalf("expected normalized feed URL, got %+v", items[0])
	}
}

func TestServiceExtractsToutiaoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.toutiao.com/article/7658204565203960362/?utm_source=a&spm=1">A</a><a href="https://www.toutiao.com/article/7658204565203960362/?utm_source=b">B</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "w1", Platform: "toutiao", Type: "toutiao_author", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected duplicate URL variants to collapse, got %+v", items)
	}
	if items[0].URL != "https://www.toutiao.com/article/7658204565203960362/" || items[0].ContentKey != "toutiao:7658204565203960362" {
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
	if items[0].URL != "https://www.toutiao.com/group/7658228232710144575/" || items[0].ContentKey != "toutiao:7658228232710144575" {
		t.Fatalf("unexpected first URL: %+v", items)
	}
}

func TestServiceExtractsToutiaoVideoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.toutiao.com/video/7667505035089101366/?log_from=a">V</a><script>{"url":"https:\/\/www.toutiao.com\/article\/7658204565203960362\/"}</script></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "w1", Platform: "toutiao", Type: "toutiao_author", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected article and video links, got %+v", items)
	}
	if items[0].URL != "https://www.toutiao.com/video/7667505035089101366/" || items[0].ContentKey != "toutiao:video:7667505035089101366" || items[0].ContentType != "video" {
		t.Fatalf("unexpected video item: %+v", items[0])
	}
}

func TestServiceReadsFixedToutiaoVideo(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "tt-video",
		Platform: "toutiao",
		Type:     "toutiao_video",
		URL:      "https://www.toutiao.com/video/7667505035089101366/",
		Name:     "Toutiao video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.toutiao.com/video/7667505035089101366/" || items[0].ContentKey != "toutiao:video:7667505035089101366" || items[0].ContentType != "video" {
		t.Fatalf("unexpected toutiao video item: %+v", items[0])
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
	if items[0].URL != "https://www.toutiao.com/article/1870280855070795/" || items[0].ContentKey != "toutiao:1870280855070795" || items[0].ContentType != "article" {
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
	if items[0].URL != "https://x.com/elonmusk/status/12345" || items[0].ContentKey != "x:12345" || items[0].ContentType != "x_post" || items[0].Author != "elonmusk" {
		t.Fatalf("unexpected x item: %+v", items[0])
	}
}

func TestServiceExtractsXStatusLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://x.com/elonmusk/status/12345?s=20&utm_source=a">A</a><a href="https://twitter.com/elonmusk/status/12345?spm=1">B</a><script>{"url":"https:\/\/x.com\/elonmusk\/status\/67890"}</script></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "x1", Platform: "x", Type: "x_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 x status links, got %+v", items)
	}
	if items[0].ContentType != "x_post" || items[0].URL != "https://x.com/elonmusk/status/12345" || items[0].ContentKey != "x:12345" {
		t.Fatalf("unexpected first x item: %+v", items[0])
	}
}

func TestServiceReadsFixedDouyinVideo(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "dy1",
		Platform: "douyin",
		Type:     "douyin_video",
		URL:      "https://www.douyin.com/video/7123456789012345678?utm_source=a",
		Name:     "Douyin video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.douyin.com/video/7123456789012345678" || items[0].ContentKey != "douyin:7123456789012345678" || items[0].ContentType != "video" {
		t.Fatalf("unexpected douyin item: %+v", items[0])
	}
}

func TestServiceExtractsDouyinWorkLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.douyin.com/video/7123456789012345678?utm_source=a">A</a><a href="https://www.iesdouyin.com/share/video/7123456789012345678/?spm=1">B</a><a href="https://www.douyin.com/note/8123456789012345678">C</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "dy-user", Platform: "douyin", Type: "douyin_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 douyin work links, got %+v", items)
	}
	if items[0].ContentKey != "douyin:7123456789012345678" || items[1].ContentKey != "douyin:8123456789012345678" {
		t.Fatalf("unexpected douyin keys: %+v", items)
	}
	if items[1].ContentType != "note" {
		t.Fatalf("expected douyin note content type, got %+v", items[1])
	}
}

func TestServiceReadsFixedXiaohongshuNote(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "xhs1",
		Platform: "xiaohongshu",
		Type:     "xiaohongshu_note",
		URL:      "https://www.xiaohongshu.com/explore/662f8b3a000000001e031234?xsec_token=tok",
		Name:     "XHS note",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.xiaohongshu.com/explore/662f8b3a000000001e031234" || items[0].ContentKey != "xiaohongshu:662f8b3a000000001e031234" || items[0].ContentType != "note" {
		t.Fatalf("unexpected xhs item: %+v", items[0])
	}
}

func TestServiceExtractsXiaohongshuNoteLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.xiaohongshu.com/explore/662f8b3a000000001e031234?xsec_token=a&utm_source=x">A</a><script>{"url":"https:\/\/www.xiaohongshu.com\/discovery\/item\/abc123def4567890"}</script></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "xhs-user", Platform: "xiaohongshu", Type: "xiaohongshu_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 xhs note links, got %+v", items)
	}
	if items[0].ContentKey != "xiaohongshu:662f8b3a000000001e031234" || items[1].ContentKey != "xiaohongshu:abc123def4567890" {
		t.Fatalf("unexpected xhs keys: %+v", items)
	}
}

func TestXiaohongshuPlatformLinkAllowsShortLinks(t *testing.T) {
	if !platformLink("xiaohongshu", nil, "https://xhslink.com/a/AbCdEf") {
		t.Fatal("expected xhslink short link to pass platform filter")
	}
}

func TestServiceExtractsWechatArticleLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://mp.weixin.qq.com/s/abc123?scene=1&utm_source=a">A</a><a href="https://mp.weixin.qq.com/s/abc123?scene=2&utm_source=b">B</a><a href="https://example.com/s/abc123">C</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "wx-list", Platform: "wechat", Type: "wechat_list", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected duplicate wechat links to collapse, got %+v", items)
	}
	if items[0].URL != "https://mp.weixin.qq.com/s/abc123" || items[0].ContentType != "article" || items[0].ContentKey == "" {
		t.Fatalf("unexpected wechat item: %+v", items[0])
	}
}

func TestCanonicalWechatArticleURLKeepsStableIdentityParams(t *testing.T) {
	got := canonicalWechatArticleURL("https://mp.weixin.qq.com/s?__biz=biz&mid=1&idx=2&sn=abc&scene=9&from=timeline&utm_source=x")
	want := "https://mp.weixin.qq.com/s?__biz=biz&idx=2&mid=1&sn=abc"
	if got != want {
		t.Fatalf("canonical wechat url = %q, want %q", got, want)
	}
}

func TestServiceReadsWebListLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="/posts/a?utm_source=feed">A</a><a href="/pages/b">B</a><a href="https://other.example/posts/c">C</a><a href="/posts/d">D</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID: "web1", Platform: "web", Type: "web_list", URL: srv.URL,
		Options: map[string]any{"include_pattern": `/posts/`, "limit": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected same-domain filtered and limited web link, got %+v", items)
	}
	if items[0].URL != srv.URL+"/posts/a" || items[0].ContentKey != srv.URL+"/posts/a" {
		t.Fatalf("unexpected web item: %+v", items[0])
	}
}

func TestServiceWebListNotePathRemainsArticle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="/note/a">A</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID: "web-note", Platform: "web", Type: "web_list", URL: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 web note-path item, got %+v", items)
	}
	if items[0].ContentType != "article" {
		t.Fatalf("expected web /note/ path to remain article, got %+v", items[0])
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

func TestToutiaoUserTokenFromURL(t *testing.T) {
	got := toutiaoUserTokenFromURL("https://www.toutiao.com/c/user/token/abc123=/?source=profile")
	if got != "abc123=" {
		t.Fatalf("token = %q", got)
	}
}
