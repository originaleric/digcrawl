package platformprov

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
