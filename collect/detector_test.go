package collect

import (
	"testing"

	browserprov "github.com/originaleric/diggate/provider/browser"
	platformprov "github.com/originaleric/diggate/provider/platform"
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

func TestDetectUnsupported(t *testing.T) {
	t.Parallel()
	_, err := Detect("ftp://example.com/page")
	if err == nil {
		t.Fatal("expected error for non-http(s) scheme")
	}
}
