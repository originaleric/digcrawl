package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	coremedia "github.com/originaleric/digcrawl/internal/media"
	"github.com/originaleric/digcrawl/internal/testutil"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
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

func TestDownloadAssetsCopiesMediaArtifact(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	validMP4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	if _, err := store.Put(context.Background(), "youtube-video", "video/mp4", "video.mp4", validMP4); err != nil {
		t.Fatal(err)
	}
	assets := t.TempDir()
	saved, err := DownloadAssets(context.Background(), assets, map[string]any{
		"media": []any{map[string]any{"type": "video", "artifact_id": "youtube-video"}},
	}, AssetDownloadOptions{ArtifactStore: store, MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 {
		t.Fatalf("saved=%v", saved)
	}
	data, err := os.ReadFile(filepath.Join(assets, "001.mp4"))
	if err != nil || string(data) != string(validMP4) {
		t.Fatalf("artifact media=%q err=%v", data, err)
	}
}

func TestWithoutRetainedMediaComposesVideoAndImageSkips(t *testing.T) {
	out := map[string]any{"media": []any{
		map[string]any{"type": "video", "url": "https://example.com/a.mp4"},
		map[string]any{"type": "image", "url": "https://example.com/a.jpg"},
		map[string]any{"type": "photo", "url": "https://example.com/b.jpg"},
		map[string]any{"type": "audio", "url": "https://example.com/a.mp3"},
	}}
	filtered := withoutRetainedMedia(out, true, true)
	items := extractMediaItems(filtered)
	if len(items) != 1 || items[0]["type"] != "audio" {
		t.Fatalf("filtered=%+v", items)
	}
	if len(extractMediaItems(out)) != 4 {
		t.Fatalf("filter must not mutate original output: %+v", out)
	}
}

func TestExportDownloadsHiddenMediaWithoutPublishingMediaField(t *testing.T) {
	includeMedia := false
	dir := t.TempDir()
	store := fakeArtifactStore{
		data:        []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
		contentType: "image/png",
	}
	result, err := Export(context.Background(), map[string]any{
		"title": "Hidden media",
		"media": []any{map[string]any{"type": "image", "artifact_id": "image-1"}},
	}, ExportOptions{
		OutputDir:     dir,
		Formats:       []string{"json"},
		Now:           time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		DownloadMedia: true,
		IncludeMedia:  &includeMedia,
		ArtifactStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.Paths.Assets, "001.png")); err != nil {
		t.Fatalf("hidden media must remain available to DownloadAssets: %v", err)
	}
	data, err := os.ReadFile(result.Paths.JSON)
	if err != nil {
		t.Fatal(err)
	}
	var exported map[string]any
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatal(err)
	}
	if _, ok := exported["media"]; ok {
		t.Fatalf("media must not be published when disabled: %s", data)
	}
}

func TestDownloadAssetsRejectsNonVideoArtifact(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "invalid-video", "video/mp4", "video.mp4", []byte("not a video")); err != nil {
		t.Fatal(err)
	}
	assets := t.TempDir()
	saved, err := DownloadAssets(context.Background(), assets, map[string]any{
		"media": []any{map[string]any{"type": "video", "artifact_id": "invalid-video"}},
	}, AssetDownloadOptions{ArtifactStore: store, MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || filepath.Base(saved[0]) != "media.json" {
		t.Fatalf("invalid artifact must not be saved as media: %v", saved)
	}
	if _, err := os.Stat(filepath.Join(assets, "001.mp4")); !os.IsNotExist(err) {
		t.Fatalf("invalid artifact file must be removed, stat err=%v", err)
	}
}

func TestWithoutVideoMediaKeepsOtherAssetsAndDoesNotMutateOutput(t *testing.T) {
	out := map[string]any{
		"media": []any{
			map[string]any{"type": "image", "url": "https://example.com/image.jpg"},
			map[string]any{"type": "video", "url": "https://example.com/video.mp4"},
		},
	}
	filtered := withoutVideoMedia(out)
	items := extractMediaItems(filtered)
	if len(items) != 1 || items[0]["type"] != "image" {
		t.Fatalf("filtered media=%+v", items)
	}
	original := extractMediaItems(out)
	if len(original) != 2 {
		t.Fatalf("original output was mutated: %+v", original)
	}
}

func TestDownloadFilePrefersCompatibleImageFormatsOnlyForOCRAssets(t *testing.T) {
	validMP4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image":
			if got, want := r.Header.Get("Accept"), "image/jpeg,image/png,image/webp,image/*;q=0.8,*/*;q=0.5"; got != want {
				t.Fatalf("image Accept=%q want %q", got, want)
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		case "/video":
			if got := r.Header.Get("Accept"); got != "" {
				t.Fatalf("video Accept=%q; image preference must not affect video downloads", got)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(validMP4)
		}
	}))
	defer server.Close()
	for _, tc := range []struct{ path, mediaType, want string }{
		{path: "/image", mediaType: "image", want: "jpeg"},
		{path: "/video", mediaType: "video", want: string(validMP4)},
	} {
		t.Run(tc.mediaType, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "asset")
			_, err := coremedia.DownloadFile(context.Background(), server.Client(), server.URL+tc.path, dest, 1024, "", tc.mediaType)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(dest)
			if err != nil || string(data) != tc.want {
				t.Fatalf("downloaded=%q err=%v", data, err)
			}
		})
	}
}

func TestExportRecordsASRErrorForInvalidDownloadedVideo(t *testing.T) {
	enabled := true
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "001.mp4"), []byte("not a real video"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{"platform_metadata": map[string]any{}}
	changed := enrichASRFromDownloadedMedia(context.Background(), out, config.ASRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}, []map[string]any{{"type": "video", "local": "001.mp4"}}, assetsDir)
	if !changed {
		t.Fatal("invalid media should trigger output rewrite for asr_error")
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if _, ok := meta["asr_error"].(string); !ok {
		t.Fatalf("expected invalid video to record asr_error without failing collect, meta=%+v", meta)
	}
}

func TestRewritePersistsASRErrorForInvalidDownloadedVideo(t *testing.T) {
	t.Parallel()
	enabled := true
	dir := t.TempDir()
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "001.mp4"), []byte("not a real video"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"title":             "Invalid ASR Video",
		"source_url":        "https://www.toutiao.com/video/1/",
		"platform_metadata": map[string]any{},
	}
	changed := enrichASRFromDownloadedMedia(context.Background(), out, config.ASRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}, []map[string]any{{"type": "video", "local": "001.mp4"}}, assetsDir)
	if !changed {
		t.Fatal("invalid media should trigger output rewrite for asr_error")
	}
	paths, err := BuildOutputPaths(dir, "Invalid ASR Video", time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := rewriteTextualOutputs(out, paths, []string{"json"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.JSON)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	meta, _ := parsed["platform_metadata"].(map[string]any)
	if _, ok := meta["asr_error"].(string); !ok {
		t.Fatalf("json missing asr_error: %s", string(data))
	}
}

func TestASRSuccessClearsPreviousMediaError(t *testing.T) {
	t.Parallel()
	enabled := true
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "002.mp4"), []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}
	ffmpeg := testutil.FakeFFmpeg(t, assetsDir)
	out := map[string]any{"platform_metadata": map[string]any{}}
	changed := enrichASRFromDownloadedMedia(context.Background(), out, config.ASRConfig{
		Enabled:    &enabled,
		Provider:   "stub",
		FFmpegPath: ffmpeg,
		TempDir:    assetsDir,
	}, []map[string]any{
		{"type": "video", "local": "001.mp4"},
		{"type": "video", "local": "002.mp4"},
	}, assetsDir)
	if !changed {
		t.Fatal("successful second media should change output")
	}
	got, _ := out["asr_text"].(string)
	if got != "stub ASR transcript" {
		t.Fatalf("asr_text=%q", got)
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if _, ok := meta["asr_error"]; ok {
		t.Fatalf("stale asr_error should be cleared after success: %+v", meta)
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

func TestDownloadedVideoPathsStayInsideAssetsDirectory(t *testing.T) {
	assetsDir := t.TempDir()
	paths := downloadedVideoPaths([]map[string]any{
		{"type": "video", "local": "001.mp4"},
		{"type": "video", "local": "../../outside.mp4"},
		{"type": "image", "local": "cover.jpg"},
	}, assetsDir)
	if got, want := paths, []string{filepath.Join(assetsDir, "001.mp4"), filepath.Join(assetsDir, "outside.mp4")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("video paths=%q want=%q", got, want)
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
