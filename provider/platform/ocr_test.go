package platformprov

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/testutil"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/executor"
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
	if out["screenshot_artifact_id"] == "" || out["screenshot_artifact_id"] == "direct_ocr_screenshot_screenshot" {
		t.Fatalf("screenshot artifact must use a runtime-owned ID: %v", out["screenshot_artifact_id"])
	}
	if len(artifacts) != 1 || artifacts[0].ID != out["screenshot_artifact_id"] {
		t.Fatalf("artifacts=%+v", artifacts)
	}
}

func TestPlatformReadHandlerDoesNotUseExternalCallIDAsArtifactIdentity(t *testing.T) {
	input, err := json.Marshal(platformReadInput{URL: "https://8.8.8.8/post", IncludeScreenshot: true})
	if err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandler([]string{"8.8.8.8"}, nil, store, func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
		return &iplatform.Content{SourceURL: in.URL, ScreenshotBase64: "iVBORw0KGgo="}, nil
	})
	call := &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "client-controlled-id", Tool: "test.read", Input: input}
	_, first, err := handler(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := handler(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].ID == second[0].ID {
		t.Fatalf("same external call ID must not overwrite artifacts: first=%+v second=%+v", first, second)
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

func TestShouldDownloadYouTubeMediaHonorsSkillASROptIn(t *testing.T) {
	oldSkill := config.Skill()
	t.Cleanup(func() { config.SetSkill(oldSkill) })
	config.SetSkill(config.SkillConfig{})
	if shouldDownloadYouTubeMedia(config.Get(), config.Skill(), false, platformReadInput{IncludeASR: true}) {
		t.Fatal("disabled Skill ASR must not trigger YouTube download")
	}
	enabled := true
	config.SetSkill(config.SkillConfig{Media: config.SkillMediaConfig{DownloadMedia: true, Dir: t.TempDir()}, ASR: config.ASRConfig{Enabled: &enabled}})
	if !shouldDownloadYouTubeMedia(config.Get(), config.Skill(), false, platformReadInput{IncludeASR: true}) {
		t.Fatal("enabled Skill ASR must acquire YouTube media")
	}
	if shouldDownloadYouTubeMedia(config.Get(), config.Skill(), false, platformReadInput{IncludeMedia: true}) {
		t.Fatal("include_media alone must not acquire a private MP4")
	}
}

func TestShouldDownloadYouTubeMediaForExplicitVideoFrameOCR(t *testing.T) {
	oldSkill := config.Skill()
	t.Cleanup(func() { config.SetSkill(oldSkill) })
	enabled := true
	config.SetSkill(config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: t.TempDir()},
		OCR: config.OCRConfig{
			Enabled: &enabled,
			VideoFrames: config.VideoFrameOCRConfig{
				Enabled: &enabled,
			},
		},
	})
	if !shouldDownloadYouTubeMedia(config.Get(), config.Skill(), false, platformReadInput{IncludeOCR: true}) {
		t.Fatal("explicit video-frame OCR must acquire YouTube media")
	}

	config.SetSkill(config.SkillConfig{OCR: config.OCRConfig{Enabled: &enabled, VideoFrames: config.VideoFrameOCRConfig{Enabled: &enabled}}})
	if shouldDownloadYouTubeMedia(config.Get(), config.Skill(), false, platformReadInput{IncludeOCR: true}) {
		t.Fatal("video-frame OCR without Skill media opt-in must not acquire YouTube media")
	}
}

func TestShouldDownloadYouTubeMediaUsesSkillSummaryEnvValidation(t *testing.T) {
	oldSkill := config.Skill()
	t.Cleanup(func() { config.SetSkill(oldSkill) })
	t.Setenv("DIGCRAWL_TEST_SUMMARY_KEY", "test-key")
	enabled := true
	config.SetSkill(config.SkillConfig{
		Media:   config.SkillMediaConfig{DownloadMedia: true, Dir: t.TempDir()},
		ASR:     config.ASRConfig{Enabled: &enabled},
		Summary: config.SkillSummaryConfig{Enabled: &enabled, LLM: config.LLMSummaryConfig{APIKey: "${DIGCRAWL_TEST_SUMMARY_KEY}"}},
	})
	if !shouldDownloadYouTubeMedia(config.Get(), config.Skill(), false, platformReadInput{IncludeSummary: true}) {
		t.Fatal("environment-backed valid Skill summary must acquire YouTube media")
	}
}

func TestYouTubeVideoFrameOCRUsesCoreLocalVideoWithoutArtifactStore(t *testing.T) {
	enabled := true
	mediaDir := t.TempDir()
	ffmpeg := testutil.FakeFFmpeg(t, mediaDir)
	runtimeCfg := &config.Config{Skill: config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: mediaDir, MaxBytes: 1024},
		OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub", VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled, IntervalSec: 1, MaxFrames: 1, MaxTotalFrames: 1, FFmpegPath: ffmpeg, TempDir: mediaDir,
		}},
	}}
	videoPath := filepath.Join(mediaDir, "youtube.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldAcquire := acquireYouTubeVideo
	cleaned := false
	acquireYouTubeVideo = func(context.Context, string, int64, bool, string) (string, func(), error) {
		return videoPath, func() { cleaned = true }, nil
	}
	t.Cleanup(func() { acquireYouTubeVideo = oldAcquire })
	input, err := json.Marshal(platformReadInput{URL: "https://www.youtube.com/watch?v=test", IncludeOCR: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandlerNamedWithConfig(ToolYouTubeVideoRead, youtubeDefaultDomains, nil, nil, func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
		return &iplatform.Content{Platform: "youtube", ContentType: "video", SourceURL: in.URL, Markdown: "body"}, nil
	}, runtimeCfg)
	out, artifacts, err := handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "collect_user_supplied", Tool: ToolYouTubeVideoRead, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 || out["media"] != nil {
		t.Fatalf("internal OCR video must not be exported as an artifact: artifacts=%+v out=%+v", artifacts, out)
	}
	meta, ok := out["platform_metadata"].(map[string]any)
	if !ok || meta["ocr_video_frame_count"] != 1 {
		t.Fatalf("video-frame OCR did not use the acquired local video: %+v", out["platform_metadata"])
	}
	serialized, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), videoPath) {
		t.Fatalf("temporary video path leaked to output: %s", serialized)
	}
	if !cleaned {
		t.Fatal("local video cleanup was not called")
	}
}

func TestYouTubeRetentionIsAnExplicitProviderOptIn(t *testing.T) {
	enabled := true
	mediaDir := t.TempDir()
	ffmpeg := testutil.FakeFFmpeg(t, mediaDir)
	runtimeCfg := &config.Config{Skill: config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, RetainDownloadedVideo: true, Dir: mediaDir, MaxBytes: 1024},
		OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub", VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled, IntervalSec: 1, MaxFrames: 1, MaxTotalFrames: 1, FFmpegPath: ffmpeg, TempDir: mediaDir,
		}},
	}}
	videoPath := filepath.Join(mediaDir, "retained.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldAcquire := acquireYouTubeVideo
	var gotRetain bool
	var gotDir string
	acquireYouTubeVideo = func(_ context.Context, _ string, _ int64, retain bool, retainDir string) (string, func(), error) {
		gotRetain, gotDir = retain, retainDir
		return videoPath, func() {}, nil
	}
	t.Cleanup(func() { acquireYouTubeVideo = oldAcquire })
	input, err := json.Marshal(platformReadInput{URL: "https://www.youtube.com/watch?v=test", IncludeOCR: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandlerNamedWithConfig(ToolYouTubeVideoRead, youtubeDefaultDomains, nil, nil, func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
		return &iplatform.Content{Platform: "youtube", ContentType: "video", SourceURL: in.URL}, nil
	}, runtimeCfg)
	out, _, err := handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "retain-video", Tool: ToolYouTubeVideoRead, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !gotRetain || gotDir != mediaDir {
		t.Fatalf("retention opt-in was not passed to provider: retain=%v dir=%q", gotRetain, gotDir)
	}
	meta := out["platform_metadata"].(map[string]any)
	if meta["downloaded_video_retained"] != true {
		t.Fatalf("retained output must be explicit without exposing its local path: %+v", meta)
	}
	if got, want := meta["retained_media_relative_path"], "retained.mp4"; got != want {
		t.Fatalf("retained output path=%#v want %q", got, want)
	}
	serialized, _ := json.Marshal(out)
	if strings.Contains(string(serialized), videoPath) {
		t.Fatalf("retained local path must not leak through tool output: %s", serialized)
	}
}

func TestRetainedMediaRelativePathRejectsOutsideMediaRoot(t *testing.T) {
	mediaDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "retained.mp4")
	if _, err := retainedMediaRelativePath(mediaDir, outside); err == nil {
		t.Fatal("retained media outside the configured root must not be exposed")
	}
}

func TestYouTubeExplicitMediaKeepsPublicCoverWithoutDownloadingMP4(t *testing.T) {
	oldAcquire := acquireYouTubeVideo
	downloadCalled := false
	acquireYouTubeVideo = func(context.Context, string, int64, bool, string) (string, func(), error) {
		downloadCalled = true
		return "", nil, nil
	}
	t.Cleanup(func() { acquireYouTubeVideo = oldAcquire })
	input, err := json.Marshal(platformReadInput{URL: "https://www.youtube.com/watch?v=test", IncludeMedia: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandlerNamedWithConfig(ToolYouTubeVideoRead, youtubeDefaultDomains, nil, nil, func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
		return &iplatform.Content{Platform: "youtube", ContentType: "video", SourceURL: in.URL}, nil
	}, &config.Config{})
	_, _, err = handler(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "direct_media", Tool: ToolYouTubeVideoRead, Input: input})
	if err != nil {
		t.Fatalf("direct media metadata read should not require an MP4 artifact: %v", err)
	}
	if downloadCalled {
		t.Fatal("include_media alone must not download an MP4")
	}
}

func TestYouTubeCollectExecutionPersistsSeparateExportArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	oldAcquire := acquireYouTubeVideo
	acquireYouTubeVideo = func(context.Context, string, int64, bool, string) (string, func(), error) {
		return path, func() {}, nil
	}
	t.Cleanup(func() { acquireYouTubeVideo = oldAcquire })
	downloadEnabled := true
	runtimeCfg := &config.Config{Collect: config.CollectConfig{DownloadMedia: &downloadEnabled, MaxMediaBytes: 16, MediaTimeoutSec: 1}}
	input, err := json.Marshal(platformReadInput{URL: "https://www.youtube.com/watch?v=test", IncludeMedia: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := platformReadHandlerNamedWithConfig(ToolYouTubeVideoRead, youtubeDefaultDomains, nil, store, func(_ context.Context, in iplatform.ReadInput) (*iplatform.Content, error) {
		return &iplatform.Content{Platform: "youtube", ContentType: "video", SourceURL: in.URL}, nil
	}, runtimeCfg)
	out, artifacts, err := handler(executor.WithCollectExecution(context.Background()), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "direct_user_id", Tool: ToolYouTubeVideoRead, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("Collect handoff must not become an output artifact: %+v", artifacts)
	}
	if out["media"] == nil {
		t.Fatalf("Collect output must retain its internal video reference: %+v", out)
	}
	media := out["media"].([]iplatform.MediaItem)
	item := media[len(media)-1]
	if item.ArtifactID == "" || item.ArtifactID == "direct_user_id_youtube_video" {
		t.Fatalf("Collect artifact must not derive identity from external call ID: %+v", item)
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
