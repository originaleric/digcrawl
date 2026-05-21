package wechat

import (
	"testing"

	"github.com/originaleric/diggate/internal/browser"
	"github.com/originaleric/diggate/internal/platform"
)

func TestNormalizeArticleIncludeMedia(t *testing.T) {
	t.Parallel()
	in := platform.ReadInput{IncludeMedia: true}
	resp := &browser.BrowserBrowseResponse{
		URL:      "https://mp.weixin.qq.com/s/abc",
		Title:    "Test Article",
		Markdown: "![](https://mmbiz.qpic.cn/img.jpg)",
		Metadata: map[string]any{
			"author_name":  "Author",
			"published_at": "2026-01-01",
			"image_urls":   []any{"https://mmbiz.qpic.cn/a.jpg", "https://mmbiz.qpic.cn/b.jpg"},
		},
	}
	c := normalizeArticle(in, resp)
	if len(c.Media) != 2 {
		t.Fatalf("expected 2 media items, got %d", len(c.Media))
	}
	if c.Media[0].Type != "image" || c.Media[0].URL == "" {
		t.Fatalf("unexpected media: %+v", c.Media[0])
	}
}

func TestNormalizeArticleSkipMediaWhenDisabled(t *testing.T) {
	t.Parallel()
	in := platform.ReadInput{IncludeMedia: false}
	resp := &browser.BrowserBrowseResponse{
		Metadata: map[string]any{"image_urls": []any{"https://mmbiz.qpic.cn/a.jpg"}},
	}
	c := normalizeArticle(in, resp)
	if len(c.Media) != 0 {
		t.Fatalf("expected no media when IncludeMedia=false, got %d", len(c.Media))
	}
}
