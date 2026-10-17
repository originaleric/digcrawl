package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	internalbrowser "github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/executor"
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

func TestBrowseEntryReturnsSnapshotElements(t *testing.T) {
	previous := browsePage
	defer func() { browsePage = previous }()
	for _, elements := range [][]internalbrowser.AXNode{nil, {{Role: "button", Name: "Fixture Button"}}} {
		browsePage = func(_ context.Context, req *internalbrowser.BrowserBrowseRequest) (*internalbrowser.BrowserBrowseResponse, error) {
			if req.Mode != "snapshot" {
				t.Fatalf("mode=%s", req.Mode)
			}
			return &internalbrowser.BrowserBrowseResponse{URL: req.URL, Elements: elements}, nil
		}
		input, _ := json.Marshal(browseInput{URL: "https://example.com/article", Mode: "snapshot"})
		out, _, err := BrowseEntry(nil, nil, &config.Config{}).Handler(context.Background(), &protocol.ToolCall{Input: input})
		if err != nil {
			t.Fatal(err)
		}
		actual, ok := out["elements"].([]internalbrowser.AXNode)
		if !ok || actual == nil || len(actual) != len(elements) {
			t.Fatalf("elements=%v out=%v", elements, out)
		}
		if len(elements) != 0 && actual[0] != elements[0] {
			t.Fatalf("elements changed: actual=%v expected=%v", actual, elements)
		}
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

func TestBrowseEntryOnlyRequestsNeededImageMetadata(t *testing.T) {
	old := browsePage
	defer func() { browsePage = old }()
	enabled, disabled := true, false
	for _, tc := range []struct {
		name                                     string
		input                                    browseInput
		rootOCR, skillOCR, retain, collect, want bool
	}{
		{name: "text"}, {name: "media", input: browseInput{IncludeMedia: true}, want: true},
		{name: "explicit_ocr", input: browseInput{IncludeOCR: true}, skillOCR: true, want: true},
		{name: "root_ocr", rootOCR: true, want: true}, {name: "skill_ocr", skillOCR: true, want: true},
		{name: "retain_images", retain: true, want: true}, {name: "collect_text", collect: true, rootOCR: true, skillOCR: true},
		{name: "collect_media", collect: true, input: browseInput{IncludeMedia: true}, want: true},
		{name: "collect_retain", collect: true, retain: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{OCR: config.OCRConfig{Enabled: &disabled, Provider: "stub"}, Skill: config.SkillConfig{OCR: config.OCRConfig{Enabled: &disabled, Provider: "stub"}}, ImageDownload: config.ImageDownloadConfig{Providers: map[string]bool{"web": tc.retain}}}
			if tc.rootOCR {
				cfg.OCR.Enabled = &enabled
			}
			if tc.skillOCR {
				cfg.Skill.OCR.Enabled = &enabled
			}
			called := false
			browsePage = func(_ context.Context, req *internalbrowser.BrowserBrowseRequest) (*internalbrowser.BrowserBrowseResponse, error) {
				called = true
				if (req.MetadataScript != "") != tc.want {
					t.Errorf("metadata requested=%v want=%v", req.MetadataScript != "", tc.want)
				}
				return &internalbrowser.BrowserBrowseResponse{URL: req.URL, Text: "body", Markdown: "body", Metadata: map[string]any{}}, nil
			}
			tc.input.URL = "https://example.com/article"
			input, _ := json.Marshal(tc.input)
			ctx := context.Background()
			if tc.collect {
				ctx = executor.WithCollectExecution(ctx)
			}
			_, _, err := BrowseEntry(nil, nil, cfg).Handler(ctx, &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolBrowserBrowse, Input: input})
			if err != nil || !called {
				t.Fatalf("handler=%v called=%v", err, called)
			}
		})
	}
}

func TestChromeTextOnlyProviderAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	disabled, headless := false, true
	previous := *config.Get()
	defer func() { *config.Get() = previous }()
	cfg := config.Config{LocalBrowser: config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), TotalTimeoutSec: 20, WaitSelectorTimeoutSec: 1}, OCR: config.OCRConfig{Enabled: &disabled}, Skill: config.SkillConfig{OCR: config.OCRConfig{Enabled: &disabled}}}
	*config.Get() = cfg
	defer internalbrowser.ShutdownImmediately(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><head><title>text only</title></head><body><article>usable text</article><script>Object.defineProperty(document,"images",{get(){throw new Error("image enrichment unavailable")}})</script></body></html>`)
	}))
	defer server.Close()
	input, _ := json.Marshal(browseInput{URL: server.URL})
	out, _, err := BrowseEntry(nil, nil, &cfg).Handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolBrowserBrowse, Input: input})
	if err != nil || out["text"] != "usable text" {
		t.Fatalf("text only read failed: out=%v err=%v", out, err)
	}
	input, _ = json.Marshal(browseInput{URL: server.URL, IncludeMedia: true})
	out, _, err = BrowseEntry(nil, nil, &cfg).Handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolBrowserBrowse, Input: input})
	var be *internalbrowser.BrowseError
	if out != nil || !errors.As(err, &be) || be.Stage != "metadata" {
		t.Fatalf("requested media failure hidden: out=%v err=%v", out, err)
	}
	t.Run("snapshot_source_policy", testChromeSnapshotSourcePolicyAcceptance)
}

func TestBrowseEntryChecksFinalURLAgainstCallPolicy(t *testing.T) {
	old := browsePage
	defer func() { browsePage = old }()
	browsePage = func(context.Context, *internalbrowser.BrowserBrowseRequest) (*internalbrowser.BrowserBrowseResponse, error) {
		return &internalbrowser.BrowserBrowseResponse{URL: "https://blocked.example/", Text: "foreign content"}, nil
	}
	input, _ := json.Marshal(browseInput{URL: "https://allowed.example/"})
	for _, tc := range []struct {
		name    string
		domains []string
		policy  protocol.CallPolicy
	}{
		{name: "configured", domains: []string{"allowed.example"}},
		{name: "per_call", policy: protocol.CallPolicy{AllowedDomains: []string{"allowed.example"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := BrowseEntry(tc.domains, nil, &config.Config{}).Handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolBrowserBrowse, Input: input, Policy: tc.policy})
			if out != nil || err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") {
				t.Fatalf("final URL restriction bypassed: out=%v err=%v", out, err)
			}
		})
	}
}

func testChromeSnapshotSourcePolicyAcceptance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://"+strings.Replace(r.Host, "localhost", "127.0.0.1", 1)+"/final", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<html><head><title>Final source</title></head><body><button>Final source button</button></body></html>`)
	}))
	defer server.Close()
	start := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/redirect"
	input, _ := json.Marshal(internalbrowser.BrowserSnapshotRequest{URL: start})
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow_redirect=%v", allow), func(t *testing.T) {
			domains := []string{"localhost"}
			if allow {
				domains = append(domains, "127.0.0.1")
			}
			out, arts, err := SnapshotEntry(nil).Handler(context.Background(), &protocol.ToolCall{Input: input, Policy: protocol.CallPolicy{AllowedDomains: domains}})
			t.Logf("start=%s final=%s out=%+v artifacts=%d err=%v", start, server.URL+"/final", out, len(arts), err)
			if !allow {
				if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || out != nil {
					t.Errorf("blocked final source returned as success")
				}
				return
			}
			if err != nil || out["source_url"] != server.URL+"/final" {
				t.Errorf("snapshot did not report actual source")
			}
		})
	}
}
