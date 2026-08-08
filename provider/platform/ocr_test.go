package platformprov

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

func TestPlatformReadHandlerReturnsMarkdownAndScreenshotArtifact(t *testing.T) {
	input, err := json.Marshal(platformReadInput{
		URL:               "https://8.8.8.8/post",
		IncludeScreenshot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandler(
		[]string{"8.8.8.8"},
		nil,
		store,
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

	out, artifacts, err := handler(context.Background(), &protocol.ToolCall{
		Type:  protocol.TypeToolCall,
		ID:    "direct_ocr_screenshot",
		Tool:  "test.read",
		Input: input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["markdown"]; got != "body" {
		t.Fatalf("markdown=%v, want body to remain available with screenshot", got)
	}
	if out["screenshot_artifact_id"] != "direct_ocr_screenshot_screenshot" {
		t.Fatalf("screenshot_artifact_id=%v", out["screenshot_artifact_id"])
	}
	if len(artifacts) != 1 || artifacts[0].ID != out["screenshot_artifact_id"] {
		t.Fatalf("artifacts=%+v", artifacts)
	}
}

func TestPlatformReadHandlerAppliesSkillConfiguredOCR(t *testing.T) {
	enabled := true
	oldSkill := config.Skill()
	config.SetSkill(config.SkillConfig{OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub"}})
	defer func() {
		config.SetSkill(oldSkill)
	}()

	input, err := json.Marshal(platformReadInput{
		URL: "https://8.8.8.8/post", IncludeScreenshot: true, IncludeOCR: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandler(
		[]string{"8.8.8.8"}, nil, store,
		func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
			if !in.IncludeMedia {
				t.Fatal("skill OCR should request media from the platform reader")
			}
			return &iplatform.Content{Platform: "test", ContentType: "article", SourceURL: in.URL, Markdown: "body", ScreenshotBase64: "iVBORw0KGgo="}, nil
		},
	)

	out, _, err := handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "skill_ocr_screenshot", Tool: "test.read", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from screenshot") {
		t.Fatalf("skill OCR markdown=%s", out["markdown"])
	}
}

func TestPlatformReadHandlerRejectsUnconfiguredSkillOCR(t *testing.T) {
	oldSkill := config.Skill()
	config.SetSkill(config.SkillConfig{})
	defer func() {
		config.SetSkill(oldSkill)
	}()

	input, err := json.Marshal(platformReadInput{URL: "https://8.8.8.8/post", IncludeOCR: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandler(
		[]string{"8.8.8.8"}, nil, nil,
		func(_ context.Context, _ iplatform.ReadInput) (*iplatform.Content, error) {
			return &iplatform.Content{Platform: "test", ContentType: "article", SourceURL: "https://8.8.8.8/post", Markdown: "body"}, nil
		},
	)

	_, _, err = handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "skill_ocr_disabled", Tool: "test.read", Input: input})
	if err == nil || !strings.Contains(err.Error(), "Skill.OCR.Enabled") {
		t.Fatalf("err=%v, expected explicit Skill OCR configuration error", err)
	}
}

func TestPlatformReadHandlerFailsWhenScreenshotCannotBePersisted(t *testing.T) {
	input, err := json.Marshal(platformReadInput{URL: "https://8.8.8.8/post", IncludeScreenshot: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandler(
		[]string{"8.8.8.8"}, nil, nil,
		func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
			return &iplatform.Content{SourceURL: in.URL, ScreenshotBase64: "iVBORw0KGgo="}, nil
		},
	)
	out, artifacts, err := handler(context.Background(), &protocol.ToolCall{
		Type: protocol.TypeToolCall, ID: "missing_store", Tool: "test.read", Input: input,
	})
	if err == nil || !strings.Contains(err.Error(), "artifact store is unavailable") {
		t.Fatalf("expected an explicit artifact-store failure, got out=%+v artifacts=%+v err=%v", out, artifacts, err)
	}
	if out["screenshot_artifact_id"] != nil || len(artifacts) != 0 {
		t.Fatalf("handler must not return an unusable artifact id: out=%+v artifacts=%+v", out, artifacts)
	}
}
