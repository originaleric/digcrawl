package collect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFallbackMediaItemsFromMarkdown(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		"markdown": "hello ![](https://mmbiz.qpic.cn/a.jpg) and ![](https://mmbiz.qpic.cn/b.jpg)",
	}
	items := fallbackMediaItems(out)
	if len(items) != 2 {
		t.Fatalf("expected 2 fallback items, got %d", len(items))
	}
}

func TestRefererForWechatDownload(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		"platform":   "wechat",
		"source_url": "https://mp.weixin.qq.com/s/abc",
	}
	if got := refererForDownload(out); got != "https://mp.weixin.qq.com/" {
		t.Fatalf("referer=%q", got)
	}
}

func TestExtractMediaItemsFromTypedSlice(t *testing.T) {
	t.Parallel()
	out := map[string]any{
		"media": []any{
			map[string]any{"type": "image", "url": "https://example.com/a.jpg"},
		},
	}
	items := extractMediaItems(out)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestExportWritesManifestWhenDownloadMediaDisabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result, err := Export(context.Background(), map[string]any{
		"title": "No Download",
		"media": []any{
			map[string]any{"type": "image", "url": "https://example.com/a.jpg"},
		},
	}, ExportOptions{
		OutputDir:     dir,
		Formats:       []string{"md"},
		Now:           time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
		DownloadMedia: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.Paths.Assets, "media.json")); err != nil {
		t.Fatalf("expected media manifest: %v", err)
	}
}
