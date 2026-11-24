package ocr

import (
	"context"
	"github.com/originaleric/digcrawl/config"
	"strings"
	"testing"
)

func TestProviderErrorsAreClassifiedWithoutReflectingInput(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
		retry      bool
	}{
		{400, `{"error":{"message":"image download failed https://secret/?token=private","code":"secret-token"}}`, "image_unavailable", false},
		{400, `{"error":{"message":"invalid input private credential"}}`, "invalid_input", false},
		{401, `secret`, "authentication_failed", false}, {403, `secret`, "authentication_failed", false},
		{429, `secret`, "rate_limited", true}, {503, `secret`, "provider_unavailable", true},
	} {
		e := classifyProviderError(tc.status, []byte(tc.body)).(*providerError)
		if e.code != tc.code || e.retryable != tc.retry || strings.Contains(e.Error(), "secret") || strings.Contains(e.Error(), "private") {
			t.Fatalf("%+v", e)
		}
	}
}
func TestInlineAcquisitionFailureDoesNotFallBackToRemoteURL(t *testing.T) {
	enabled := true
	cfg := config.OCRConfig{Enabled: &enabled, Provider: "stub"}
	out := map[string]any{"text": "original body", "media": []map[string]any{{"type": "image", "url": "https://example.test/requires-session"}}}
	EnrichMap(context.Background(), out, cfg, EnrichOptions{ExtraImages: []ImageRef{
		{URL: "https://example.test/requires-session", Type: "image", Error: "access_denied"},
		{Source: "valid", Type: "image", DataURL: "data:image/png;base64,YQ=="},
	}})
	meta := ensureMeta(out)
	if meta["ocr_status"] != "degraded" || meta["ocr_failure_count"] != 1 || meta["ocr_success_count"] != 1 {
		t.Fatalf("%+v", meta)
	}
	results := ocrResultMaps(meta)
	if len(results) != 2 || !strings.Contains(stringValue(results[0], "error"), "access_denied") {
		t.Fatalf("%+v", results)
	}
}

func TestOCRDiagnosticSourceOmitsSignedQueryAndCredentials(t *testing.T) {
	got := safeImageSourceURL("https://user:password@cdn.example/image.png?token=private#secret")
	if got != "https://cdn.example/image.png" {
		t.Fatalf("unsafe source reference: %s", got)
	}
}
