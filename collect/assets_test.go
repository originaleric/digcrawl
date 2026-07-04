package collect

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
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

func TestNormalizeDownloadedAssetNameUsesResponseContentTypeForImageExtension(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	dir := t.TempDir()
	rawURL := "https://dummyimage.com/900x260/ffffff/000000.png&text=Invoice+No+20260704+Amount+USD+128.50"
	name := assetFilename(rawURL, "image", 0)
	if name != "001.50" {
		t.Fatalf("fixture should expose misleading URL extension, got %q", name)
	}
	dest := filepath.Join(dir, name)
	if err := os.WriteFile(dest, png, 0o644); err != nil {
		t.Fatal(err)
	}
	finalName, finalDest, err := normalizeDownloadedAssetName(name, dest, rawURL, "image", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if finalName != "001.png" {
		t.Fatalf("finalName=%q", finalName)
	}
	if finalDest != filepath.Join(dir, "001.png") {
		t.Fatalf("finalDest=%q", finalDest)
	}
	if _, err := os.Stat(filepath.Join(dir, "001.png")); err != nil {
		t.Fatalf("expected PNG asset name from response content type: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "001.50")); !os.IsNotExist(err) {
		t.Fatalf("unexpected misleading asset name 001.50, err=%v", err)
	}
}

func TestExportAppliesOCRWhenEnabled(t *testing.T) {
	t.Parallel()
	enabled := true
	dir := t.TempDir()
	result, err := Export(context.Background(), map[string]any{
		"title":      "OCR article",
		"source_url": "https://example.com/post",
		"markdown":   "body",
		"media": []any{
			map[string]any{"type": "image", "url": "https://example.com/a.jpg"},
		},
	}, ExportOptions{
		OutputDir:     dir,
		Formats:       []string{"md", "json"},
		Now:           time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
		DownloadMedia: false,
		OCR: config.OCRConfig{
			Enabled:  &enabled,
			Provider: "stub",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(result.Paths.Markdown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "## OCR") {
		t.Fatalf("markdown missing OCR: %s", string(md))
	}
	data, err := os.ReadFile(result.Paths.JSON)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	meta, _ := parsed["platform_metadata"].(map[string]any)
	if meta["ocr_count"] != float64(1) {
		t.Fatalf("metadata=%+v", meta)
	}
}

func TestExportRewritesOutputsWhenOCRUsesDownloadedAsset(t *testing.T) {
	t.Parallel()
	enabled := true
	appendText := true
	dir := t.TempDir()
	store := fakeArtifactStore{
		data:        []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
		contentType: "image/png",
	}
	result, err := Export(context.Background(), map[string]any{
		"title":                  "Downloaded OCR",
		"source_url":             "https://example.com/post",
		"text":                   "plain body",
		"markdown":               "body",
		"screenshot_artifact_id": "shot-1",
	}, ExportOptions{
		OutputDir:     dir,
		Formats:       []string{"md", "html", "json"},
		Now:           time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
		DownloadMedia: true,
		ArtifactStore: store,
		OCR: config.OCRConfig{
			Enabled:      &enabled,
			Provider:     "stub",
			AppendToText: &appendText,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{result.Paths.Markdown, result.Paths.HTML, result.Paths.JSON} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "stub OCR text from") {
			t.Fatalf("%s missing OCR output:\n%s", path, string(data))
		}
	}
}

type fakeArtifactStore struct {
	data        []byte
	contentType string
}

func (s fakeArtifactStore) Put(context.Context, string, string, string, []byte) (protocol.Artifact, error) {
	return protocol.Artifact{}, nil
}

func (s fakeArtifactStore) Get(context.Context, string) ([]byte, string, error) {
	return s.data, s.contentType, nil
}
