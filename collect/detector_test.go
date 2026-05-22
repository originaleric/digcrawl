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

func TestDetectUnsupported(t *testing.T) {
	t.Parallel()
	_, err := Detect("ftp://example.com/page")
	if err == nil {
		t.Fatal("expected error for non-http(s) scheme")
	}
}
