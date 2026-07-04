package platformprov

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/protocol"
)

func TestPlatformReadHandlerAppliesOCRToDirectScreenshot(t *testing.T) {
	enabled := true
	oldOCR := config.Get().OCR
	config.Get().OCR = config.OCRConfig{
		Enabled:  &enabled,
		Provider: "stub",
	}
	defer func() {
		config.Get().OCR = oldOCR
	}()

	input, err := json.Marshal(platformReadInput{
		URL:               "https://example.com/post",
		IncludeScreenshot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandler(
		[]string{"example.com"},
		nil,
		nil,
		func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
			if !in.IncludeScreenshot {
				t.Fatal("expected include_screenshot to reach platform reader")
			}
			return &iplatform.Content{
				Platform:         "test",
				ContentType:      "article",
				SourceURL:        in.URL,
				Title:            "Screenshot article",
				Markdown:         "body",
				ScreenshotBase64: "iVBORw0KGgo=",
			}, nil
		},
	)

	out, _, err := handler(context.Background(), &protocol.ToolCall{
		Type:  protocol.TypeToolCall,
		ID:    "direct_ocr_screenshot",
		Tool:  "test.read",
		Input: input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from screenshot") {
		t.Fatalf("markdown missing screenshot OCR: %s", out["markdown"])
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta["ocr_count"] != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	results, ok := meta["ocr_results"].([]map[string]any)
	if !ok || len(results) != 1 || results[0]["source"] != "screenshot" {
		t.Fatalf("ocr_results=%T %+v", meta["ocr_results"], meta["ocr_results"])
	}
	if out["screenshot_artifact_id"] != "direct_ocr_screenshot_screenshot" {
		t.Fatalf("screenshot_artifact_id=%v", out["screenshot_artifact_id"])
	}
}
