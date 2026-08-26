package ocr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/testutil"
)

func TestEnrichMapStubAppendsMarkdownAndMetadata(t *testing.T) {
	enabled := true
	out := map[string]any{
		"markdown": "body",
		"media": []any{
			map[string]any{"type": "image", "url": "https://example.com/a.jpg"},
		},
	}
	results := EnrichMap(context.Background(), out, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		Model:    "stub-model",
	}, EnrichOptions{})
	if len(results) != 1 {
		t.Fatalf("results=%d", len(results))
	}
	if !strings.Contains(out["markdown"].(string), "## OCR") {
		t.Fatalf("markdown missing OCR section: %s", out["markdown"])
	}
	meta := out["platform_metadata"].(map[string]any)
	if meta["ocr_count"] != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	rawResults, ok := meta["ocr_results"].([]map[string]any)
	if !ok || len(rawResults) != 1 {
		t.Fatalf("ocr_results=%T %+v", meta["ocr_results"], meta["ocr_results"])
	}
	if rawResults[0]["provider"] != "stub" {
		t.Fatalf("ocr result=%+v", rawResults[0])
	}
}

func TestEnrichContentWritesPlatformMetadata(t *testing.T) {
	enabled := true
	content := &platform.Content{
		Platform:    "test",
		ContentType: "article",
		SourceURL:   "https://example.com/post",
		Markdown:    "body",
		Media:       []platform.MediaItem{{Type: "image", URL: "https://example.com/a.jpg"}},
	}
	results := EnrichContent(context.Background(), content, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	})
	if len(results) != 1 {
		t.Fatalf("results=%d", len(results))
	}
	if !strings.Contains(content.Markdown, "## OCR") {
		t.Fatalf("markdown=%s", content.Markdown)
	}
	if content.PlatformMetadata["ocr_count"] != 1 {
		t.Fatalf("metadata=%+v", content.PlatformMetadata)
	}
}

func TestEnrichMapPrefersDownloadedLocalImageWithoutDuplicateURL(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	local := filepath.Join(dir, "001.jpg")
	if err := os.WriteFile(local, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"media": []any{
			map[string]any{"type": "image", "url": "https://example.com/a.jpg"},
		},
	}
	results := EnrichMap(context.Background(), out, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}, EnrichOptions{
		AssetsDir: dir,
		Downloaded: []map[string]any{
			{"type": "image", "url": "https://example.com/a.jpg", "local": "001.jpg"},
		},
	})
	if len(results) != 1 {
		t.Fatalf("results=%d: %+v", len(results), results)
	}
	if results[0].LocalPath != local {
		t.Fatalf("expected local image OCR, got %+v", results[0])
	}
	meta := out["platform_metadata"].(map[string]any)
	encoded := meta["ocr_results"].([]map[string]any)
	if _, exists := encoded[0]["local_path"]; exists {
		t.Fatalf("OCR output must not export local paths: %+v", encoded[0])
	}
}

func TestRequestedFailureDistinguishesProviderErrorFromNoTextResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta map[string]any
		want bool
	}{
		{name: "all provider failures", meta: map[string]any{"ocr_results": []map[string]any{{"error": "OCR provider HTTP 400"}}}, want: true},
		{name: "global provider failure", meta: map[string]any{"ocr_error": "OCR APIKey is empty"}, want: true},
		{name: "model found no text", meta: map[string]any{"ocr_results": []map[string]any{{"text": "No readable text found."}}}},
		{name: "no candidates", meta: map[string]any{"ocr_count": 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RequestedFailure(map[string]any{"platform_metadata": tc.meta})
			if (err != nil) != tc.want {
				t.Fatalf("error=%v wantFailure=%v", err, tc.want)
			}
		})
	}
}

func TestEnrichMapExtraDataURLScreenshot(t *testing.T) {
	enabled := true
	out := map[string]any{
		"markdown": "body",
	}
	results := EnrichMap(context.Background(), out, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}, EnrichOptions{
		ExtraImages: []ImageRef{PNGDataURLImageRef("screenshot", "iVBORw0KGgo=")},
	})
	if len(results) != 1 {
		t.Fatalf("results=%d", len(results))
	}
	if results[0].Source != "screenshot" {
		t.Fatalf("expected screenshot source, got %+v", results[0])
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from screenshot") {
		t.Fatalf("markdown missing screenshot OCR: %s", out["markdown"])
	}
	meta := out["platform_metadata"].(map[string]any)
	rawResults, ok := meta["ocr_results"].([]map[string]any)
	if !ok || rawResults[0]["source"] != "screenshot" {
		t.Fatalf("ocr_results=%T %+v", meta["ocr_results"], meta["ocr_results"])
	}
}

func TestEnrichMapKeepsDistinctInlineImagesWithSameSource(t *testing.T) {
	enabled := true
	out := map[string]any{
		"markdown": "body",
	}
	results := EnrichMap(context.Background(), out, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}, EnrichOptions{
		ExtraImages: []ImageRef{
			PNGDataURLImageRef("screenshot", "iVBORw0KGgo="),
			PNGDataURLImageRef("screenshot", "iVBORw0KGgp="),
		},
	})
	if len(results) != 2 {
		t.Fatalf("expected two inline images with same source, got %d: %+v", len(results), results)
	}
	meta := out["platform_metadata"].(map[string]any)
	if meta["ocr_count"] != 2 {
		t.Fatalf("metadata=%+v", meta)
	}
}

func TestOpenAICompatibleProviderLocalImage(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer test-key" {
			sawAuth = true
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "vision-test" {
			t.Fatalf("payload=%+v", payload)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello image"}}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	img := filepath.Join(dir, "a.png")
	if err := os.WriteFile(img, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := newProvider(config.OCRConfig{
		Provider: "openai_compatible",
		APIKey:   "test-key",
		BaseURL:  srv.URL,
		Model:    "vision-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.Recognize(context.Background(), ImageRef{LocalPath: img, Type: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello image" || !sawAuth {
		t.Fatalf("text=%q sawAuth=%v", text, sawAuth)
	}
}

func TestOpenAICompatibleProviderDataURLImage(t *testing.T) {
	var sawDataURL bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		image := content[1].(map[string]any)["image_url"].(map[string]any)
		rawURL, _ := image["url"].(string)
		if strings.HasPrefix(rawURL, "data:image/png;base64,") {
			sawDataURL = true
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"screenshot text"}}]}`))
	}))
	defer srv.Close()

	p, err := newProvider(config.OCRConfig{
		Provider: "openai_compatible",
		APIKey:   "test-key",
		BaseURL:  srv.URL,
		Model:    "vision-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.Recognize(context.Background(), PNGDataURLImageRef("screenshot", "iVBORw0KGgo="))
	if err != nil {
		t.Fatal(err)
	}
	if text != "screenshot text" || !sawDataURL {
		t.Fatalf("text=%q sawDataURL=%v", text, sawDataURL)
	}
}

func TestOpenAICompatibleProviderAcceptsContentBlocksAndLegacyText(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "content blocks",
			body: `{"choices":[{"message":{"content":[{"type":"text","text":"first line"},{"type":"text","text":"second line"}]}}]}`,
			want: "first line\nsecond line",
		},
		{
			name: "legacy choice text",
			body: `{"choices":[{"text":"legacy text"}]}`,
			want: "legacy text",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["temperature"] != float64(0) {
					t.Fatalf("temperature=%v", payload["temperature"])
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			p, err := newProvider(config.OCRConfig{Provider: "openai_compatible", APIKey: "test-key", BaseURL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Recognize(context.Background(), PNGDataURLImageRef("screenshot", "iVBORw0KGgo="))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("text=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestNewProviderRejectsUnsupportedProvider(t *testing.T) {
	_, err := newProvider(config.OCRConfig{Provider: "zhipu_ocr", APIKey: "test-key"})
	if err == nil || !strings.Contains(err.Error(), `unsupported OCR provider "zhipu_ocr"`) {
		t.Fatalf("error=%v", err)
	}
}

func TestOpenAICompatibleProviderRejectsOversizedDataURLBeforeRequest(t *testing.T) {
	p, err := newProvider(config.OCRConfig{
		Provider:      "openai_compatible",
		APIKey:        "test-key",
		BaseURL:       "http://127.0.0.1:1",
		Model:         "vision-test",
		MaxImageBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4, 5})
	_, err = p.Recognize(context.Background(), ImageRef{
		DataURL: "data:image/png;base64," + payload,
		Type:    "screenshot",
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds max bytes 4") {
		t.Fatalf("expected max bytes error, got %v", err)
	}
}

func TestDataURLSizeEstimateCountsInvalidWhitespace(t *testing.T) {
	raw := "data:image/png;base64,AAAA" + strings.Repeat(" ", 64)
	err := validateDataImageURL(raw, 4)
	if err == nil || !strings.Contains(err.Error(), "exceeds max bytes 4") {
		t.Fatalf("expected max bytes error before decode, got %v", err)
	}
}

func TestRecognizeLocalVideoFramesUsesBoundedOptInAndHidesTemporaryPaths(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	video := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(video, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	results, err := RecognizeLocalVideoFrames(context.Background(), config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled:     &enabled,
			IntervalSec: 5,
			MaxFrames:   2,
			FFmpegPath:  testutil.FakeFFmpeg(t, dir),
			TempDir:     dir,
		},
	}, video)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	if !strings.HasPrefix(results[0].Source, "video_frame:video.mp4:") || results[0].Type != "video_frame" || results[0].LocalPath != "" {
		t.Fatalf("video frame result leaked temporary path: %+v", results[0])
	}
	if results[0].Text != "stub OCR text from video frame" {
		t.Fatalf("text=%q", results[0].Text)
	}
}

func TestExtractLocalVideoFrameRefsAppliesMaxFramesToEachVideo(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("fake video"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	refs, cleanup, err := ExtractLocalVideoFrameRefs(context.Background(), config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled:        &enabled,
			MaxFrames:      1,
			MaxTotalFrames: 2,
			FFmpegPath:     testutil.FakeFFmpeg(t, dir),
			TempDir:        dir,
		},
	}, []string{first, second})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || !strings.Contains(refs[0].Source, "first.mp4") || !strings.Contains(refs[1].Source, "second.mp4") {
		t.Fatalf("refs=%+v", refs)
	}
}

func TestExtractLocalVideoFrameRefsReservesTotalBudgetForFailedVideo(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.mp4")
	second := filepath.Join(dir, "second.mp4")
	if err := os.WriteFile(second, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, cleanup, err := ExtractLocalVideoFrameRefs(context.Background(), config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled:        &enabled,
			MaxFrames:      1,
			MaxTotalFrames: 1,
			FFmpegPath:     testutil.FakeFFmpeg(t, dir),
			TempDir:        dir,
		},
	}, []string{missing, second})
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), "missing.mp4") {
		t.Fatalf("expected failed first video error, got %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("a failed first video must consume the only allocation, refs=%+v", refs)
	}
}

func TestEnrichMapAddsOnlyNewVideoFramesWhenOCRResultsAlreadyExist(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	frame := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(frame, []byte("fake frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"markdown": "body",
		"media":    []any{map[string]any{"type": "image", "url": "https://example.com/cover.jpg"}},
	}
	cfg := config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled,
		},
	}
	if results := EnrichMap(context.Background(), out, cfg, EnrichOptions{}); len(results) != 1 {
		t.Fatalf("initial results=%+v", results)
	}
	results := EnrichMap(context.Background(), out, cfg, EnrichOptions{
		ExtraImages: []ImageRef{{LocalPath: frame, Source: "video_frame:video.mp4:1", Type: "video_frame"}},
	})
	if len(results) != 1 || results[0].Type != "video_frame" {
		t.Fatalf("video frame results=%+v", results)
	}
	meta := out["platform_metadata"].(map[string]any)
	encoded := ocrResultMaps(meta)
	if len(encoded) != 2 || meta["ocr_count"] != 2 {
		t.Fatalf("existing and new OCR results must both remain: %+v", meta)
	}
	if !strings.Contains(out["markdown"].(string), "### Video frame 1") {
		t.Fatalf("markdown missing video OCR: %q", out["markdown"])
	}
	if duplicate := EnrichMap(context.Background(), out, cfg, EnrichOptions{
		ExtraImages: []ImageRef{{LocalPath: frame, Source: "video_frame:video.mp4:1", Type: "video_frame"}},
	}); len(duplicate) != 0 {
		t.Fatalf("recorded video frame was processed twice: %+v", duplicate)
	}
}

func TestEnrichMapIgnoresVideoFramesUnlessExplicitlyEnabled(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	frame := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(frame, []byte("fake frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := EnrichMap(context.Background(), map[string]any{}, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}, EnrichOptions{
		ExtraImages: []ImageRef{{LocalPath: frame, Source: "video_frame:video.mp4:1", Type: "video_frame"}},
	})
	if len(results) != 0 {
		t.Fatalf("disabled video frame OCR returned results: %+v", results)
	}
}

func TestRecognizeLocalVideoFramesHasIndependentExtractionTimeout(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	video := filepath.Join(dir, "slow.mp4")
	if err := os.WriteFile(video, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIGCRAWL_TEST_FAKE_FFMPEG_SLEEP", "2s")
	_, err := RecognizeLocalVideoFrames(context.Background(), config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled:    &enabled,
			TimeoutSec: 1,
			FFmpegPath: testutil.FakeFFmpeg(t, dir),
			TempDir:    dir,
		},
	}, video)
	if err == nil || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("timeout error=%v", err)
	}
}

func TestRecognizeLocalVideoFramesIsDisabledUnlessExplicitlyOptedIn(t *testing.T) {
	results, err := RecognizeLocalVideoFrames(context.Background(), config.OCRConfig{}, "/does/not/exist.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("disabled feature returned results: %+v", results)
	}
}

func TestEnrichMapLabelsVideoFramesAndDoesNotExportTheirTemporaryPaths(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	frame := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(frame, []byte("fake frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{"markdown": "body"}
	results := EnrichMap(context.Background(), out, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled,
		},
	}, EnrichOptions{ExtraImages: []ImageRef{{LocalPath: frame, Source: "video_frame", Type: "video_frame"}}})
	if len(results) != 1 || results[0].LocalPath != "" {
		t.Fatalf("results=%+v", results)
	}
	if !strings.Contains(out["markdown"].(string), "### Video frame 1") {
		t.Fatalf("markdown=%q", out["markdown"])
	}
	meta := out["platform_metadata"].(map[string]any)
	encoded := meta["ocr_results"].([]map[string]any)
	if _, ok := encoded[0]["local_path"]; ok {
		t.Fatalf("temporary frame path was exported: %+v", encoded[0])
	}
}

func TestEnrichMapKeepsImageAndVideoFrameBudgetsIndependent(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	refs := make([]ImageRef, 0, 9)
	for i := 0; i < 8; i++ {
		image := filepath.Join(dir, fmt.Sprintf("image-%d.jpg", i))
		if err := os.WriteFile(image, []byte("image"), 0o600); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ImageRef{LocalPath: image, Type: "image"})
	}
	frame := filepath.Join(dir, "frame.jpg")
	if err := os.WriteFile(frame, []byte("frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs = append(refs, ImageRef{LocalPath: frame, Source: "video_frame:video.mp4:1", Type: "video_frame"})
	results := EnrichMap(context.Background(), map[string]any{}, config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
		VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled,
		},
	}, EnrichOptions{ExtraImages: refs})
	if len(results) != 9 {
		t.Fatalf("results=%d; video frame displaced regular images", len(results))
	}
	if results[8].Type != "video_frame" {
		t.Fatalf("last result=%+v", results[8])
	}
}
