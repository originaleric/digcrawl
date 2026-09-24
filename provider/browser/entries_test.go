package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	internalbrowser "github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/protocol"
)

func TestValidateCallURLOpensPublicWebWhenDomainRestrictionIsUnset(t *testing.T) {
	if err := validateCallURL("https://8.8.8.8/article", &protocol.ToolCall{}, nil); err != nil {
		t.Fatalf("expected public URL to be allowed without a domain restriction: %v", err)
	}
}

func TestBrowseEntryExposesScreenshotAlongsideRead(t *testing.T) {
	entry := BrowseEntry(nil, nil)
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(entry.Descriptor.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["include_screenshot"]; !ok {
		t.Fatal("browser.browse must expose include_screenshot so a host can receive text and an image attachment together")
	}
	if _, ok := schema.Properties["include_media"]; !ok {
		t.Fatal("browser.browse must expose include_media for rendered image references")
	}
}

func TestBrowserImageMediaKeepsOnlyUniqueURLs(t *testing.T) {
	media := browserImageMedia(map[string]any{"image_urls": []any{"https://cdn.example/a.jpg", "https://cdn.example/a.jpg", "", 42}})
	if len(media) != 1 || media[0]["type"] != "image" || media[0]["url"] != "https://cdn.example/a.jpg" {
		t.Fatalf("media=%+v", media)
	}
}

func TestBrowserImageMediaRejectsCredentialBearingOrInvalidURLs(t *testing.T) {
	media := browserImageMedia(map[string]any{"image_urls": []any{
		"https://user:password@cdn.example/private.jpg",
		"javascript:alert(1)",
		"https://cdn.example/public.jpg",
	}})
	if len(media) != 1 || media[0]["url"] != "https://cdn.example/public.jpg" {
		t.Fatalf("media=%+v", media)
	}
	if !strings.Contains(browserImageMetadataScript(), "!u.username") || !strings.Contains(browserImageMetadataScript(), "!u.password") {
		t.Fatal("browser metadata script must exclude URL credentials before they enter metadata")
	}
}

func TestBrowseEntryAppliesSkillOCRWithoutReturningInternalMedia(t *testing.T) {
	enabled := true
	cfg := &config.Config{Skill: config.SkillConfig{OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub"}}}
	oldBrowsePage := browsePage
	browsePage = func(_ context.Context, req *internalbrowser.BrowserBrowseRequest) (*internalbrowser.BrowserBrowseResponse, error) {
		return &internalbrowser.BrowserBrowseResponse{
			URL: req.URL, Markdown: "body",
			Metadata: map[string]any{"image_urls": []any{"https://images.example.test/browser.png"}},
		}, nil
	}
	t.Cleanup(func() { browsePage = oldBrowsePage })
	input, err := json.Marshal(browseInput{URL: "https://example.com/article"})
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := BrowseEntry(nil, nil, cfg).Handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolBrowserBrowse, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from https://images.example.test/browser.png") {
		t.Fatalf("markdown=%q", out["markdown"])
	}
	if _, ok := out["media"]; ok {
		t.Fatalf("internal OCR media leaked: %+v", out["media"])
	}
}

func TestBrowseEntryPreservesPageContentWhenConfiguredOCRProviderFails(t *testing.T) {
	enabled := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	cfg := &config.Config{Skill: config.SkillConfig{OCR: config.OCRConfig{
		Enabled: &enabled, Provider: "openai_compatible", APIKey: "test-key", BaseURL: server.URL,
	}}}
	oldBrowsePage := browsePage
	browsePage = func(_ context.Context, req *internalbrowser.BrowserBrowseRequest) (*internalbrowser.BrowserBrowseResponse, error) {
		return &internalbrowser.BrowserBrowseResponse{
			URL: req.URL, Markdown: "page body",
			Metadata: map[string]any{"image_urls": []any{server.URL + "/image.png"}},
		}, nil
	}
	t.Cleanup(func() { browsePage = oldBrowsePage })

	input, err := json.Marshal(browseInput{URL: "https://example.com/article"})
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := BrowseEntry(nil, nil, cfg).Handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolBrowserBrowse, Input: input})
	if err != nil {
		t.Fatalf("OCR provider failure must not turn a successful browser read into a tool error: %v", err)
	}
	if got := out["markdown"]; got != "page body" {
		t.Fatalf("markdown=%q, want original page content", got)
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta["ocr_status"] != "degraded" || meta["ocr_failure_reason"] != "provider_unavailable" {
		t.Fatalf("OCR degradation is not observable: %+v", meta)
	}
}

func TestValidateCallURLAllowsLocalTargetsAndHonorsOptionalRestriction(t *testing.T) {
	if err := validateCallURL("http://127.0.0.1/admin", &protocol.ToolCall{}, nil); err != nil {
		t.Fatalf("expected an unrestricted local target to be allowed: %v", err)
	}
	if err := validateCallURL("https://1.1.1.1/article", &protocol.ToolCall{}, []string{"8.8.8.8"}); err == nil {
		t.Fatal("expected configured domain restriction to be enforced")
	}
}
