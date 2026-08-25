package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
	kuaishouplatform "github.com/originaleric/digcrawl/internal/platform/kuaishou"
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

func TestServiceRanksToutiaoLinksBeforeApplyingLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>
<a href="https://www.toutiao.com/article/7404301050942997043/">old-a</a>
<a href="https://www.toutiao.com/article/7590162030313603590/">old-b</a>
<script>{"url":"https:\/\/www.toutiao.com\/video\/7667703322668745258\/","url2":"https:\/\/www.toutiao.com\/video\/7667693400077894178\/"}</script>
</body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "w1",
		Platform: "toutiao",
		Type:     "toutiao_author",
		URL:      srv.URL,
		Options:  map[string]any{"limit": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected limited items, got %+v", items)
	}
	if items[0].ContentKey != "toutiao:video:7667703322668745258" || items[1].ContentKey != "toutiao:video:7667693400077894178" {
		t.Fatalf("expected newest toutiao IDs before older page links, got %+v", items)
	}
}

func TestServiceExtractsToutiaoMicroPostLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.toutiao.com/w/1870280855070795/?log_from=a">micro</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "w1",
		Platform: "toutiao",
		Type:     "toutiao_author",
		URL:      srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected micro post link, got %+v", items)
	}
	if items[0].ContentKey != "toutiao:1870280855070795" {
		t.Fatalf("unexpected micro post item: %+v", items[0])
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

func TestServiceReadsFixedYouTubeVideo(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "youtube-video", Platform: "youtube", Type: "youtube_video", URL: "https://youtu.be/dQw4w9WgXcQ?t=42", Name: "Video"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "youtube:dQw4w9WgXcQ" || items[0].URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" || items[0].ContentType != "video" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestServiceExtractsYouTubeVideoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="https://www.youtube.com/watch?v=dQw4w9WgXcQ&utm_source=a">watch</a><a href="https://www.youtube.com/shorts/dQw4w9WgXcQ">short duplicate</a><a href="https://www.youtube.com/@OpenAI">channel</a>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "youtube-channel", Platform: "youtube", Type: "youtube_channel", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "youtube:dQw4w9WgXcQ" || items[0].ContentType != "video" {
		t.Fatalf("unexpected items: %+v", items)
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

func TestServiceReadsFixedBilibiliVideo(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "bili1",
		Platform: "bilibili",
		Type:     "bilibili_video",
		URL:      "https://www.bilibili.com/video/BV1xx411c7mD?spm_id_from=333",
		Name:     "Bili video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.bilibili.com/video/BV1xx411c7mD" || items[0].ContentKey != "bilibili:BV1xx411c7mD" || items[0].ContentType != "video" {
		t.Fatalf("unexpected bilibili item: %+v", items[0])
	}
}

func TestServiceExtractsBilibiliVideoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.bilibili.com/video/BV1xx411c7mD?spm_id_from=333">A</a><script>{"url":"https:\/\/www.bilibili.com\/video\/av170001"}</script></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "bili-user", Platform: "bilibili", Type: "bilibili_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 bilibili video links, got %+v", items)
	}
	if items[0].ContentKey != "bilibili:BV1xx411c7mD" || items[1].ContentKey != "bilibili:170001" {
		t.Fatalf("unexpected bilibili keys: %+v", items)
	}
}

func TestServiceReadsBilibiliUserViaAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/space/wbi/arc/search" || r.URL.Query().Get("mid") != "123456" {
			t.Fatalf("unexpected API request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"list":{"vlist":[{"bvid":"BV1api111","aid":1001,"title":"API视频1","author":"UP主","mid":123456,"created":1785231600},{"aid":1002,"title":"API视频2","author":"UP主","mid":123456,"created":1785231700}]}}}`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "bili-user",
		Platform: "bilibili",
		Type:     "bilibili_user",
		URL:      "https://space.bilibili.com/123456",
		Options:  map[string]any{"api_base_url": srv.URL, "limit": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 API items, got %+v", items)
	}
	if items[0].ContentKey != "bilibili:BV1api111" || items[1].ContentKey != "bilibili:1002" {
		t.Fatalf("unexpected API bilibili keys: %+v", items)
	}
	if items[0].Raw["extraction_source"] != "api" {
		t.Fatalf("expected api extraction source, got %+v", items[0].Raw)
	}
}

func TestServiceFallsBackToBilibiliHTMLWhenAPIUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x/space/wbi/arc/search" {
			_, _ = w.Write([]byte(`{"code":-352,"message":"风控校验失败","data":{"list":{"vlist":[]}}}`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><a href="https://www.bilibili.com/video/BV1fallback">A</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "bili-user",
		Platform: "bilibili",
		Type:     "bilibili_user",
		URL:      srv.URL + "/space",
		Options:  map[string]any{"api_base_url": srv.URL, "platform_id": "123456", "browser_fallback": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "bilibili:BV1fallback" {
		t.Fatalf("expected HTML fallback item, got %+v", items)
	}
}

func TestServiceReadsFixedWeiboPost(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "wb1",
		Platform: "weibo",
		Type:     "weibo_post",
		URL:      "https://m.weibo.cn/detail/502233445566",
		Name:     "Weibo post",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://m.weibo.cn/detail/502233445566" || items[0].ContentKey != "weibo:502233445566" || items[0].ContentType != "post" {
		t.Fatalf("unexpected weibo item: %+v", items[0])
	}
}

func TestServiceExtractsWeiboPostLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://m.weibo.cn/detail/502233445566?foo=1">A</a><a href="https://weibo.com/123456/NabcDEF">B</a><a href="https://m.weibo.cn/u/123456">user</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "wb-user", Platform: "weibo", Type: "weibo_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 weibo links, got %+v", items)
	}
	if items[0].ContentKey != "weibo:502233445566" || items[1].ContentKey != "weibo:NabcDEF" {
		t.Fatalf("unexpected weibo keys: %+v", items)
	}
}

func TestServiceReadsWeiboUserViaAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/container/getIndex" || r.URL.Query().Get("value") != "123456" {
			t.Fatalf("unexpected API request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"ok":1,"data":{"cards":[{"mblog":{"id":"502233445566","mid":"NabcDEF","text":"<p>微博正文&nbsp;1</p>","created_at":"Wed Jul 29 09:00:00 +0800 2026","user":{"id":123456,"screen_name":"博主"}}},{"mblog":{"id":"502233445567","text_raw":"微博正文2","user":{"id":123456,"screen_name":"博主"}}}]}}`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "wb-user",
		Platform: "weibo",
		Type:     "weibo_user",
		URL:      srv.URL + "/u/123456",
		Options:  map[string]any{"api_base_url": srv.URL, "platform_id": "123456", "limit": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 API items, got %+v", items)
	}
	if items[0].ContentKey != "weibo:502233445566" || items[1].ContentKey != "weibo:502233445567" {
		t.Fatalf("unexpected API weibo keys: %+v", items)
	}
	if items[0].Title != "微博正文 1" || items[0].Raw["extraction_source"] != "api" {
		t.Fatalf("unexpected API weibo item: %+v", items[0])
	}
}

func TestServiceFallsBackToWeiboHTMLWhenAPIUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/container/getIndex" {
			_, _ = w.Write([]byte(`{"ok":0,"msg":"登录后查看更多","data":{"cards":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><a href="https://m.weibo.cn/detail/502233445566">A</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "wb-user",
		Platform: "weibo",
		Type:     "weibo_user",
		URL:      srv.URL + "/u/123456",
		Options:  map[string]any{"api_base_url": srv.URL, "platform_id": "123456", "browser_fallback": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "weibo:502233445566" {
		t.Fatalf("expected HTML fallback item, got %+v", items)
	}
}

func TestServiceReadsFixedKuaishouVideo(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "ks1",
		Platform: "kuaishou",
		Type:     "kuaishou_video",
		URL:      "https://www.kuaishou.com/short-video/3xf6abc",
		Name:     "Kuaishou video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.kuaishou.com/short-video/3xf6abc" || items[0].ContentKey != "kuaishou:3xf6abc" || items[0].ContentType != "video" {
		t.Fatalf("unexpected kuaishou item: %+v", items[0])
	}
}

func TestServiceExtractsKuaishouVideoLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.kuaishou.com/short-video/3xf6abc">A</a><script>{"url":"https:\/\/www.kuaishou.com\/photo\/3xf6def"}</script></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "ks-user", Platform: "kuaishou", Type: "kuaishou_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 kuaishou links, got %+v", items)
	}
	if items[0].ContentKey != "kuaishou:3xf6abc" || items[1].ContentKey != "kuaishou:3xf6def" {
		t.Fatalf("unexpected kuaishou keys: %+v", items)
	}
}

func TestServiceStopsKuaishouUserListOnFatalAPIError(t *testing.T) {
	old := listKuaishouUserVideosViaAPI
	listKuaishouUserVideosViaAPI = func(context.Context, *platform.NormalizedURL, platform.ReadInput, int) ([]kuaishouplatform.APIListItem, *platform.APIAttempt, int64, error) {
		return nil, nil, 0, platform.NewReadError(platform.ErrCaptchaRequired, "快手 GraphQL 触发风控，需要登录态或安全验证", "kuaishou", nil)
	}
	defer func() { listKuaishouUserVideosViaAPI = old }()

	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "ks-user",
		Platform: "kuaishou",
		Type:     "kuaishou_user",
		URL:      "https://www.kuaishou.com/profile/u1",
		Options:  map[string]any{"browser_fallback": false},
	})
	if err == nil {
		t.Fatalf("expected fatal API error, got items %+v", items)
	}
	var re *platform.ReadError
	if !errors.As(err, &re) || re.Code != platform.ErrCaptchaRequired {
		t.Fatalf("expected CAPTCHA_REQUIRED ReadError, got %v", err)
	}
}

func TestServiceReadsFixedZhihuContent(t *testing.T) {
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "zh1",
		Platform: "zhihu",
		Type:     "zhihu_answer",
		URL:      "https://www.zhihu.com/question/123/answer/456",
		Name:     "Zhihu answer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].URL != "https://www.zhihu.com/question/123/answer/456" || items[0].ContentKey != "zhihu:answer:456" || items[0].ContentType != "answer" {
		t.Fatalf("unexpected zhihu item: %+v", items[0])
	}
}

func TestServiceExtractsZhihuContentLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.zhihu.com/question/123/answer/456">A</a><a href="https://zhuanlan.zhihu.com/p/789">B</a><a href="https://www.zhihu.com/zvideo/101112">C</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{ID: "zh-user", Platform: "zhihu", Type: "zhihu_user", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 zhihu links, got %+v", items)
	}
	if items[0].ContentKey != "zhihu:answer:456" || items[1].ContentKey != "zhihu:article:789" || items[2].ContentKey != "zhihu:video:101112" {
		t.Fatalf("unexpected zhihu keys: %+v", items)
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

func TestServiceExtractsThreadsPostLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.threads.com/@zuck/post/ABC_123?x=y">thread</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "threads-user",
		Platform: "threads",
		Type:     "threads_user",
		URL:      srv.URL,
		Options:  map[string]any{"browser_fallback": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "threads:ABC_123" || items[0].ContentType != "thread" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if got := items[0].Raw["auth_mode"]; got != "public" {
		t.Fatalf("auth_mode=%v", got)
	}
	if got := items[0].Raw["use_cookie_domain"]; got != nil {
		t.Fatalf("unexpected cookie domain: %v", got)
	}
}

func TestServiceExtractsInstagramMediaLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><a href="https://www.instagram.com/p/CODE_123/?utm_source=x">post</a><a href="https://www.instagram.com/reel/R123/">reel</a></body></html>`))
	}))
	defer srv.Close()
	items, err := Service{}.ListLatest(context.Background(), Source{
		ID:       "ig-user",
		Platform: "instagram",
		Type:     "instagram_user",
		URL:      srv.URL,
		Options:  map[string]any{"browser_fallback": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].ContentKey != "instagram:CODE_123" || items[0].ContentType != "instagram_post" {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[1].ContentKey != "instagram:R123" || items[1].ContentType != "video" {
		t.Fatalf("unexpected second item: %+v", items[1])
	}
}

func TestSocialLocalSessionRequiresGlobalAndSourceOptIn(t *testing.T) {
	origThreads := config.Get().Social.Threads
	enabled := true
	config.Get().Social.Threads = config.SocialPlatformConfig{
		AuthMode:        "local_session",
		BrowserFallback: &enabled,
	}
	defer func() {
		config.Get().Social.Threads = origThreads
	}()

	item := Source{
		Platform: "threads",
		Type:     "threads_post",
		URL:      "https://www.threads.net/@zuck/post/ABC_123",
		Options:  map[string]any{"auth_mode": "local_session"},
	}
	items, err := Service{}.ListLatest(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].Raw["auth_mode"]; got != "local_session" {
		t.Fatalf("auth_mode=%v", got)
	}
	if got := items[0].Raw["use_cookie_domain"]; got != "www.threads.com" {
		t.Fatalf("use_cookie_domain=%v", got)
	}

	item.Options = map[string]any{"auth_mode": "public"}
	items, err = Service{}.ListLatest(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].Raw["use_cookie_domain"]; got != nil {
		t.Fatalf("public source should not pass cookie domain, got %v", got)
	}
}

func TestToutiaoUserTokenFromURL(t *testing.T) {
	got := toutiaoUserTokenFromURL("https://www.toutiao.com/c/user/token/abc123=/?source=profile")
	if got != "abc123=" {
		t.Fatalf("token = %q", got)
	}
}
