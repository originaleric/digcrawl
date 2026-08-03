package ocr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
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
