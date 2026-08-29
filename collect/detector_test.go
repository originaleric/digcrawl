package collect

import (
	"testing"

	browserprov "github.com/originaleric/digcrawl/provider/browser"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
)

func TestDetectWechat(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://mp.weixin.qq.com/s/abc")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tool != platformprov.ToolWechatArticleRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectWebFallback(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://example.com/page")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectXiaohongshu(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.xiaohongshu.com/explore/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tool != platformprov.ToolXiaohongshuNoteRead || m.Platform != "xiaohongshu" {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectXHSLink(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://xhslink.com/a/xyz")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tool != platformprov.ToolXiaohongshuNoteRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectDouyin(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.douyin.com/video/123")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tool != platformprov.ToolDouyinVideoRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectToutiaoVideo(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.toutiao.com/video/7667505035089101366/")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "toutiao" || m.Tool != platformprov.ToolToutiaoVideoRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectToutiaoArticle(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.toutiao.com/article/7667505035089101366/")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "toutiao" || m.Tool != platformprov.ToolToutiaoArticleRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectXPost(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://twitter.com/u/status/99")
	if err != nil {
		t.Fatal(err)
	}
	if m.Tool != platformprov.ToolXPostRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectRedditPost(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.reddit.com/r/golang/comments/abc123/title/")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "reddit" || m.Tool != platformprov.ToolRedditPostRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectThreadsPost(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.threads.com/@zuck/post/ABC_123")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "threads" || m.Tool != platformprov.ToolThreadsPostRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectInstagramMedia(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.instagram.com/reel/R123/")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "instagram" || m.Tool != platformprov.ToolInstagramContentRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectRedditPostRSSUsesFeedReader(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.reddit.com/r/golang/comments/abc123/title/.rss")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "feed" || m.Tool != platformprov.ToolFeedRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectRedditListingFallsBackToWeb(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.reddit.com/r/golang/new/")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectBilibiliVideo(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.bilibili.com/video/BV1xx411c7mD")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "bilibili" || m.Tool != platformprov.ToolBilibiliVideoRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectBilibiliUserFallsBackToWeb(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://space.bilibili.com/123456/video")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectWeiboPost(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://m.weibo.cn/detail/502233445566")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "weibo" || m.Tool != platformprov.ToolWeiboPostRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectWeiboUserFallsBackToWeb(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://m.weibo.cn/u/123456")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectKuaishouVideo(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.kuaishou.com/short-video/3xf6abc")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "kuaishou" || m.Tool != platformprov.ToolKuaishouVideoRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectKuaishouUserFallsBackToWeb(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.kuaishou.com/profile/3xuser")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectZhihuContent(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.zhihu.com/question/123/answer/456")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "zhihu" || m.Tool != platformprov.ToolZhihuContentRead {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectZhihuUserFallsBackToWeb(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.zhihu.com/people/alice")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectYouTubeVideoAndShort(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?t=10",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
	} {
		m, err := Detect(rawURL)
		if err != nil {
			t.Fatalf("Detect(%q): %v", rawURL, err)
		}
		if m.Platform != "youtube" || m.Tool != platformprov.ToolYouTubeVideoRead {
			t.Fatalf("Detect(%q) = %+v", rawURL, m)
		}
	}
}

func TestDetectYouTubeChannelFallsBackToWeb(t *testing.T) {
	t.Parallel()
	m, err := Detect("https://www.youtube.com/@OpenAI")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "web" || m.Tool != browserprov.ToolBrowserBrowse {
		t.Fatalf("got %+v", m)
	}
}

func TestDetectUnsupported(t *testing.T) {
	t.Parallel()
	_, err := Detect("ftp://example.com/page")
	if err == nil {
		t.Fatal("expected error for non-http(s) scheme")
	}
}
