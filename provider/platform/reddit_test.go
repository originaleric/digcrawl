package platformprov

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/reddit"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
)

func TestRedditPostInputPreservesSkillEnhancementFlags(t *testing.T) {
	input, err := json.Marshal(map[string]any{
		"url":             "https://www.reddit.com/r/golang/comments/abc123/post/",
		"include_ocr":     true,
		"include_asr":     true,
		"include_summary": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	in, err := executor.DecodeInput[redditPostReadInput](&protocol.ToolCall{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !in.IncludeOCR || !in.IncludeASR || !in.IncludeSummary {
		t.Fatalf("skill enhancement flags were dropped: %+v", in)
	}
}

func TestRedditPostReadHandlerUsesInjectedImageRetentionConfig(t *testing.T) {
	cfg := &config.Config{ImageDownload: config.ImageDownloadConfig{
		Dir: filepath.Join(t.TempDir(), "images"), Providers: map[string]bool{"reddit": true}, MaxItems: 1,
	}}
	input, err := json.Marshal(redditPostReadInput{URL: "https://www.reddit.com/r/golang/comments/abc123/post/"})
	if err != nil {
		t.Fatal(err)
	}
	handler := redditPostReadHandler(nil, nil, func(_ context.Context, in reddit.ReadInput) (*iplatform.Content, error) {
		if !in.IncludeMedia {
			t.Fatal("injected image retention policy must request media")
		}
		return &iplatform.Content{SourceURL: in.URL, Media: []iplatform.MediaItem{
			{Type: "image", URL: "https://example.com/1.jpg"}, {Type: "image", URL: "https://example.com/2.jpg"},
		}}, nil
	}, cfg)
	out, _, err := handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolRedditPostRead, Input: input})
	var coded interface{ ToolErrorCode() string }
	if !errors.As(err, &coded) || coded.ToolErrorCode() != ProviderImageDownloadFailedCode {
		t.Fatalf("injected image policy was not used: out=%+v err=%v", out, err)
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta["image_download_failure_reason"] != "item_limit_exceeded" {
		t.Fatalf("metadata=%+v", meta)
	}
}

func TestRedditPostReadHandlerAppliesEnabledSkillOCRWithoutReturningInternalMedia(t *testing.T) {
	enabled := true
	cfg := &config.Config{Skill: config.SkillConfig{OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub"}}}
	input, err := json.Marshal(redditPostReadInput{URL: "https://www.reddit.com/r/golang/comments/abc123/post/"})
	if err != nil {
		t.Fatal(err)
	}
	handler := redditPostReadHandler(nil, nil, func(_ context.Context, in reddit.ReadInput) (*iplatform.Content, error) {
		if !in.IncludeMedia {
			t.Fatal("enabled Skill OCR must acquire Reddit image references")
		}
		return &iplatform.Content{Platform: "reddit", ContentType: "post", SourceURL: in.URL, Markdown: "body", Media: []iplatform.MediaItem{{Type: "image", URL: "https://images.example.test/reddit.png"}}}, nil
	}, cfg)

	out, _, err := handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, Tool: ToolRedditPostRead, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from https://images.example.test/reddit.png") {
		t.Fatalf("markdown=%q", out["markdown"])
	}
	if _, ok := out["media"]; ok {
		t.Fatalf("internal OCR media leaked: %+v", out["media"])
	}
}

func TestRedditPostReadHandlerReturnsScreenshotArtifact(t *testing.T) {
	input, err := json.Marshal(redditPostReadInput{
		URL:               "https://www.reddit.com/r/golang/comments/abc123/post/",
		IncludeScreenshot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := redditPostReadHandler(nil, store, func(_ context.Context, in reddit.ReadInput) (*iplatform.Content, error) {
		if !in.IncludeScreenshot {
			t.Fatal("expected include_screenshot to reach Reddit reader")
		}
		return &iplatform.Content{
			Platform:         "reddit",
			ContentType:      "post",
			SourceURL:        in.URL,
			Markdown:         "post body",
			ScreenshotBase64: "iVBORw0KGgo=",
		}, nil
	})

	out, artifacts, err := handler(context.Background(), &protocol.ToolCall{
		Type: protocol.TypeToolCall, ID: "reddit_screenshot", Tool: ToolRedditPostRead, Input: input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["screenshot_artifact_id"]; got == "" || got == "reddit_screenshot_screenshot" {
		t.Fatalf("screenshot artifact must use a runtime-owned ID: %v", got)
	}
	if len(artifacts) != 1 || artifacts[0].ID != out["screenshot_artifact_id"] {
		t.Fatalf("artifacts=%+v", artifacts)
	}
}

func TestRedditPostReadHandlerFailsWithoutScreenshotStore(t *testing.T) {
	input, err := json.Marshal(redditPostReadInput{
		URL:               "https://www.reddit.com/r/golang/comments/abc123/post/",
		IncludeScreenshot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := redditPostReadHandler(nil, nil, func(_ context.Context, in reddit.ReadInput) (*iplatform.Content, error) {
		return &iplatform.Content{SourceURL: in.URL, ScreenshotBase64: "iVBORw0KGgo="}, nil
	})

	out, artifacts, err := handler(context.Background(), &protocol.ToolCall{
		Type: protocol.TypeToolCall, ID: "reddit_missing_store", Tool: ToolRedditPostRead, Input: input,
	})
	if err == nil || out["screenshot_artifact_id"] != nil || len(artifacts) != 0 {
		t.Fatalf("must not return an unusable screenshot artifact: out=%+v artifacts=%+v err=%v", out, artifacts, err)
	}
}

func TestRedditProviderSharesBrowserDiagnosticsWithReaderAndPartialOutput(t *testing.T) {
	for _, partial := range []bool{false, true} {
		input, _ := json.Marshal(redditPostReadInput{URL: "https://www.reddit.com/r/golang/comments/abc123/post/", IncludeScreenshot: partial})
		var id any
		h := redditPostReadHandler(nil, nil, func(ctx context.Context, in reddit.ReadInput) (*iplatform.Content, error) {
			if browser.BrowseDiagnostics(ctx) == nil {
				t.Fatal("provider did not establish invocation mark")
			}
			id = browser.BrowseDiagnostics(ctx)["diagnostic_id"]
			// Validation terminates before acquiring Chrome, while using the public
			// page-fetch entry and its attempt accounting on the reader's context.
			_, _ = browser.PageFetchJSON(ctx, nil)
			c := &iplatform.Content{SourceURL: in.URL, Text: "post body"}
			if partial {
				c.ScreenshotBase64 = "image"
			}
			return c, nil
		}, &config.Config{})
		out, _, err := h(context.Background(), &protocol.ToolCall{Input: input})
		if (err != nil) != partial {
			t.Fatalf("partial=%v error=%v", partial, err)
		}
		d, ok := out["browser_diagnostics"].(map[string]any)
		if !ok || d["diagnostic_id"] != id || d["attempts"] != 1 {
			t.Fatalf("partial=%v diagnostics=%v", partial, out)
		}
	}
}
func TestRedditProviderPreservesTypedBrowserFailure(t *testing.T) {
	input, _ := json.Marshal(redditPostReadInput{URL: "https://www.reddit.com/r/golang/comments/abc123/post/"})
	failure := &browser.BrowseError{Code: browser.ErrorBrowserTimeout, Stage: "acquire", Cause: context.DeadlineExceeded, Diagnostics: map[string]any{"operation": "send_queue", "retry_count": 0, "diagnostic_id": "test-id", "next_actions": []string{"wait"}}}
	h := redditPostReadHandler(nil, nil, func(context.Context, reddit.ReadInput) (*iplatform.Content, error) { return nil, failure }, &config.Config{})
	_, _, err := h(context.Background(), &protocol.ToolCall{Input: input})
	if err != failure {
		t.Fatalf("typed failure was replaced: %v", err)
	}
}
