package skill

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/originaleric/digcrawl/config"
	coremedia "github.com/originaleric/digcrawl/internal/media"
	"github.com/originaleric/digcrawl/internal/testutil"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

func TestEnhanceUsesSkillOCRConfig(t *testing.T) {
	enabled := true
	out := map[string]any{"markdown": "body", "content_type": "article"}
	err := Enhance(context.Background(), out, config.SkillConfig{
		OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeOCR: true, ScreenshotBase64: "iVBORw0KGgo="})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from screenshot") {
		t.Fatalf("markdown=%q", out["markdown"])
	}
}

func TestEnhanceFailsWhenExplicitOCRHasOnlyProviderErrors(t *testing.T) {
	enabled := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	err := Enhance(context.Background(), map[string]any{"content_type": "article"}, config.SkillConfig{
		OCR: config.OCRConfig{Enabled: &enabled, Provider: "openai_compatible", APIKey: "test-key", BaseURL: server.URL},
	}, EnhanceOptions{IncludeOCR: true, ScreenshotBase64: "iVBORw0KGgo="})
	if err == nil || !strings.Contains(err.Error(), "OCR unavailable: OCR provider HTTP 400") {
		t.Fatalf("error=%v", err)
	}
}

func TestDownloadAssetsUsesVideoArtifactForASR(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "youtube-video", "video/mp4", "video.mp4", []byte("video")); err != nil {
		t.Fatal(err)
	}
	assetsDir := t.TempDir()
	_, err = DownloadAssets(context.Background(), assetsDir, map[string]any{
		"media": []any{map[string]any{"type": "video", "artifact_id": "youtube-video"}},
	}, AssetDownloadOptions{ArtifactStore: store, ASRInputOnly: true, MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(assetsDir, "001.mp4")); err != nil || string(data) != "video" {
		t.Fatalf("artifact video=%q err=%v", data, err)
	}
}

func TestDownloadFilePrefersCompatibleImageFormatsOnlyForOCRAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image":
			if got, want := r.Header.Get("Accept"), "image/jpeg,image/png,image/webp,image/*;q=0.8,*/*;q=0.5"; got != want {
				t.Fatalf("image Accept=%q want %q", got, want)
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		case "/video":
			if got := r.Header.Get("Accept"); got != "" {
				t.Fatalf("video Accept=%q; image preference must not affect video downloads", got)
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("mp4"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	for _, tc := range []struct {
		path, mediaType, want string
	}{
		{path: "/image", mediaType: "image", want: "jpeg"},
		{path: "/video", mediaType: "video", want: "mp4"},
	} {
		t.Run(tc.mediaType, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "asset")
			_, err := coremedia.DownloadFile(context.Background(), server.Client(), server.URL+tc.path, dest, 1024, "", tc.mediaType)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(dest)
			if err != nil || string(data) != tc.want {
				t.Fatalf("downloaded=%q err=%v", data, err)
			}
		})
	}
}

func TestEnhanceVideoFrameOCRUsesDownloadedVideoArtifact(t *testing.T) {
	enabled := true
	store, err := artifact.NewDiskStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "youtube-video", "video/mp4", "video.mp4", []byte("video")); err != nil {
		t.Fatal(err)
	}
	assetsDir := t.TempDir()
	ffmpeg := testutil.FakeFFmpeg(t, assetsDir)
	out := map[string]any{
		"content_type": "video",
		"markdown":     "body",
		"media":        []any{map[string]any{"type": "video", "artifact_id": "youtube-video"}},
	}
	err = Enhance(context.Background(), out, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: assetsDir, MaxBytes: 1024},
		OCR: config.OCRConfig{
			Enabled:  &enabled,
			Provider: "stub",
			VideoFrames: config.VideoFrameOCRConfig{
				Enabled:        &enabled,
				IntervalSec:    1,
				MaxFrames:      1,
				MaxTotalFrames: 1,
				FFmpegPath:     ffmpeg,
				TempDir:        assetsDir,
			},
		},
	}, EnhanceOptions{IncludeOCR: true, ArtifactStore: store})
	if err != nil {
		t.Fatal(err)
	}
	meta := platformMetadata(out)
	if meta["ocr_video_frame_count"] != 1 || meta["ocr_count"] != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	results, ok := meta["ocr_results"].([]map[string]any)
	if !ok || len(results) != 1 || results[0]["type"] != "video_frame" {
		t.Fatalf("ocr results=%+v", meta["ocr_results"])
	}
	if _, exists := results[0]["local_path"]; exists {
		t.Fatalf("video frame OCR must not expose temporary path: %+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(singleSkillAssetsDir(t, assetsDir), "001.mp4")); err != nil {
		t.Fatalf("downloaded video artifact missing: %v", err)
	}
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "digcrawl-ocr-frames-") {
			t.Fatalf("temporary frame directory was not cleaned up: %s", entry.Name())
		}
	}
}

func TestEnhanceVideoFrameOCRSelectsVideoAheadOfAudio(t *testing.T) {
	enabled := true
	store, err := artifact.NewDiskStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, mediaType, name string }{{"audio", "audio/mpeg", "audio.mp3"}, {"video", "video/mp4", "video.mp4"}} {
		if _, err := store.Put(context.Background(), item.id, item.mediaType, item.name, []byte(item.id)); err != nil {
			t.Fatal(err)
		}
	}
	assetsDir := t.TempDir()
	ffmpeg := testutil.FakeFFmpeg(t, assetsDir)
	out := map[string]any{
		"content_type": "video",
		"media": []any{
			map[string]any{"type": "audio", "artifact_id": "audio"},
			map[string]any{"type": "video", "artifact_id": "video"},
		},
	}
	err = Enhance(context.Background(), out, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: assetsDir, MaxBytes: 1024},
		OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub", VideoFrames: config.VideoFrameOCRConfig{
			Enabled: &enabled, IntervalSec: 1, MaxFrames: 1, MaxTotalFrames: 1, FFmpegPath: ffmpeg, TempDir: assetsDir,
		}},
	}, EnhanceOptions{IncludeOCR: true, ArtifactStore: store})
	if err != nil {
		t.Fatal(err)
	}
	meta := platformMetadata(out)
	if meta["ocr_video_frame_count"] != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	downloadDir := singleSkillAssetsDir(t, assetsDir)
	if _, err := os.Stat(filepath.Join(downloadDir, "001.mp4")); err != nil {
		t.Fatalf("video candidate was not downloaded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(downloadDir, "001.mp3")); !os.IsNotExist(err) {
		t.Fatalf("audio must not consume video-frame OCR candidate: %v", err)
	}
}

func TestEnhanceRejectsDisabledExplicitCapabilitiesBeforeWork(t *testing.T) {
	enabled := true
	mediaDir := t.TempDir()
	cases := []struct {
		name string
		out  map[string]any
		cfg  config.SkillConfig
		opts EnhanceOptions
		want string
	}{
		{
			name: "OCR disabled",
			out:  map[string]any{"content_type": "article"},
			opts: EnhanceOptions{IncludeOCR: true},
			want: "Skill.OCR.Enabled",
		},
		{
			name: "ASR media disabled",
			out:  map[string]any{"content_type": "video"},
			cfg:  config.SkillConfig{ASR: config.ASRConfig{Enabled: &enabled, Provider: "stub"}},
			opts: EnhanceOptions{IncludeASR: true},
			want: "Skill.Media.DownloadMedia",
		},
		{
			name: "video summary disabled before download",
			out:  map[string]any{"content_type": "video", "media": []any{map[string]any{"type": "video", "url": "https://example.com/video.mp4"}}},
			cfg: config.SkillConfig{
				Media: config.SkillMediaConfig{DownloadMedia: true, Dir: mediaDir},
				ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
			},
			opts: EnhanceOptions{IncludeSummary: true},
			want: "Skill.Summary.Enabled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Enhance(context.Background(), tc.out, tc.cfg, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("configuration failure must happen before media download, found %v", entries)
	}
}

func TestEnhanceFailsExplicitASRWithoutDownloadedAudioOrVideo(t *testing.T) {
	enabled := true
	err := Enhance(context.Background(), map[string]any{
		"content_type": "video",
		"media":        []any{map[string]any{"type": "image", "url": "https://example.com/poster.png"}},
	}, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: t.TempDir()},
		ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeASR: true})
	if err == nil || !strings.Contains(err.Error(), "ASR unavailable: no downloaded video/audio asset was available for ASR") {
		t.Fatalf("error=%v", err)
	}
}

func TestEnhanceReturnsTypedMediaDownloadErrorBeforeASR(t *testing.T) {
	enabled := true
	err := Enhance(context.Background(), map[string]any{
		"content_type": "video",
		"media": []any{map[string]any{
			"type": "video",
			"url":  "http://127.0.0.1/video.mp4",
		}},
	}, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: t.TempDir()},
		ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeASR: true})
	if err == nil || !strings.Contains(err.Error(), "media download failed") {
		t.Fatalf("error=%v", err)
	}
	var typed interface {
		ToolErrorCode() string
		ToolErrorDetails() map[string]any
	}
	if !errors.As(err, &typed) || typed.ToolErrorCode() != mediaDownloadFailedCode {
		t.Fatalf("typed error=%v", err)
	}
	details := typed.ToolErrorDetails()
	if details["media_type"] != "video" || details["reason"] != "invalid_media_url" || details["retryable"] != false {
		t.Fatalf("details=%+v", details)
	}
}

func TestEnhanceASROnlySkipsPageImagesBeforeMediaFailure(t *testing.T) {
	enabled := true
	assetsDir := t.TempDir()
	err := Enhance(context.Background(), map[string]any{
		"content_type": "video",
		"media": []any{
			map[string]any{"type": "image", "url": "http://127.0.0.1/cover.jpg"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/video.mp4"},
		},
	}, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: assetsDir},
		ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeASR: true})
	if err == nil || !strings.Contains(err.Error(), "media download failed") {
		t.Fatalf("error=%v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(singleSkillAssetsDir(t, assetsDir), "media.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var manifest struct {
		Downloaded []map[string]any `json:"downloaded"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Downloaded) != 1 || manifest.Downloaded[0]["type"] != "video" {
		t.Fatalf("ASR-only download should skip image assets: %+v", manifest.Downloaded)
	}
}

func TestEnhanceASRCandidateLimitBoundsFailedDownloads(t *testing.T) {
	enabled := true
	assetsDir := t.TempDir()
	err := Enhance(context.Background(), map[string]any{
		"content_type": "video",
		"media": []any{
			map[string]any{"type": "video", "url": "http://127.0.0.1/first.mp4"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/second.mp4"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/third.mp4"},
		},
	}, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: assetsDir, MaxASRCandidates: 2},
		ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeASR: true})
	if err == nil || !strings.Contains(err.Error(), "media download failed") {
		t.Fatalf("error=%v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(singleSkillAssetsDir(t, assetsDir), "media.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var manifest struct {
		Downloaded []map[string]any `json:"downloaded"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Downloaded) != 2 {
		t.Fatalf("candidate limit must bound attempts, manifest=%+v", manifest.Downloaded)
	}
}

func TestEnhanceOCRAndASRShareCandidateLimit(t *testing.T) {
	enabled := true
	assetsDir := t.TempDir()
	err := Enhance(context.Background(), map[string]any{
		"content_type": "video",
		"media": []any{
			map[string]any{"type": "image", "url": "http://127.0.0.1/cover.jpg"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/first.mp4"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/second.mp4"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/third.mp4"},
		},
	}, config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: assetsDir, MaxASRCandidates: 2},
		OCR:   config.OCRConfig{Enabled: &enabled, Provider: "stub"},
		ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeOCR: true, IncludeASR: true})
	if err == nil || !strings.Contains(err.Error(), "media download failed") {
		t.Fatalf("error=%v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(singleSkillAssetsDir(t, assetsDir), "media.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var manifest struct {
		Downloaded []map[string]any `json:"downloaded"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	var videoAttempts int
	for _, item := range manifest.Downloaded {
		if item["type"] == "video" {
			videoAttempts++
		}
	}
	if videoAttempts != 2 {
		t.Fatalf("OCR must not bypass ASR candidate limit, manifest=%+v", manifest.Downloaded)
	}
}

func TestDownloadAssetsASROnlySelectsOneCandidateAtATime(t *testing.T) {
	out := map[string]any{
		"media": []any{
			map[string]any{"type": "image", "url": "http://127.0.0.1/cover.jpg"},
			map[string]any{"type": "video", "url": "http://127.0.0.1/first.mp4"},
			map[string]any{"type": "audio", "url": "http://127.0.0.1/second.mp3"},
		},
	}
	for candidate, wantType := range []string{"video", "audio"} {
		dir := t.TempDir()
		if _, err := DownloadAssets(context.Background(), dir, out, AssetDownloadOptions{ASRInputOnly: true, ASRCandidateIndex: candidate}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "media.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Downloaded []map[string]any `json:"downloaded"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if len(manifest.Downloaded) != 1 || manifest.Downloaded[0]["type"] != wantType {
			t.Fatalf("candidate=%d manifest=%+v", candidate, manifest.Downloaded)
		}
	}
}

func TestMediaDownloadErrorReportsSizeLimitWithoutRawURL(t *testing.T) {
	err := asrMediaDownloadError([]map[string]any{
		{"type": "image", "local": "001.jpg"},
		{"type": "video", "error": "Get https://cdn.example/video.mp4?signature=secret: content length 35865567 exceeds max 10485760"},
	})
	if err == nil || !strings.Contains(err.Error(), "size 35865567 bytes exceeds configured limit 10485760 bytes") || strings.Contains(err.Error(), "signature=secret") {
		t.Fatalf("error=%v", err)
	}
	var typed *mediaDownloadError
	if !errors.As(err, &typed) {
		t.Fatalf("typed error=%v", err)
	}
	details := typed.ToolErrorDetails()
	if details["reason"] != "size_limit_exceeded" || details["actual_bytes"] != int64(35865567) || details["max_bytes"] != int64(10485760) || details["retryable"] != false {
		t.Fatalf("details=%+v", details)
	}
}

func TestMediaDownloadErrorClassifiesRetryableHTTPStatuses(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		reason string
	}{
		{raw: "http 429", reason: "rate_limited"},
		{raw: "http 408", reason: "timeout"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			err := newMediaDownloadError("video", tc.raw)
			details := err.ToolErrorDetails()
			if details["reason"] != tc.reason || details["retryable"] != true {
				t.Fatalf("details=%+v", details)
			}
		})
	}
}

func TestMediaDownloadErrorClassifiesMissingURLAsInvalid(t *testing.T) {
	err := newMediaDownloadError("video", "media URL missing")
	details := err.ToolErrorDetails()
	if details["reason"] != "invalid_media_url" || details["retryable"] != false {
		t.Fatalf("details=%+v", details)
	}
}

func TestValidateVideoSummaryRejectsCopiedTranscript(t *testing.T) {
	if err := validateVideoSummary("原始 转写", "原始转写"); err == nil {
		t.Fatal("expected copied transcript to be rejected")
	}
}

func TestSummarizeVideoUsesOpenAICompatibleLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "summary-model" {
			t.Fatalf("model=%v", body["model"])
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) < 1 {
			t.Fatalf("messages=%#v", body["messages"])
		}
		system, _ := messages[0].(map[string]any)["content"].(string)
		if strings.Contains(system, `\`) {
			t.Fatalf("system prompt has escaped JSON=%q", system)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"视频介绍了本地 Skill 的摘要边界。\"}"}}]}`))
	}))
	defer server.Close()

	out := map[string]any{
		"content_type":  "video",
		"platform":      "douyin",
		"title":         "本地 Skill",
		"canonical_url": "https://example.com/video",
		"asr_text":      "原始转写",
	}
	err := summarizeVideo(context.Background(), out, config.SkillSummaryConfig{
		LLM: config.LLMSummaryConfig{APIKey: "test-key", BaseURL: server.URL, Model: "summary-model"},
	}, "页面正文")
	if err != nil {
		t.Fatal(err)
	}
	result, ok := out["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary=%#v", out["summary"])
	}
	if result["summary_provider"] != "llm_direct" || result["overall_summary"] != "视频介绍了本地 Skill 的摘要边界。" {
		t.Fatalf("summary=%#v", result)
	}
}

func TestSkillAssetsDirRequiresAbsoluteConfiguredDirectory(t *testing.T) {
	if err := validateSkillAssetsRoot("relative"); err == nil {
		t.Fatal("expected relative media directory rejection")
	}
}

func TestValidateVideoEnhancementRequestDoesNotAllocateMediaWorkspace(t *testing.T) {
	enabled := true
	root := t.TempDir()
	err := ValidateVideoEnhancementRequest(config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: root},
		ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{IncludeASR: true})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("preflight must not allocate a media workspace: %v", entries)
	}
}

func TestEnhanceConcurrentCallsUseDistinctMediaWorkspaces(t *testing.T) {
	enabled := true
	root := t.TempDir()
	store, err := artifact.NewDiskStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		if _, err := store.Put(context.Background(), id, "image/jpeg", id+".jpg", []byte(id)); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.SkillConfig{
		Media: config.SkillMediaConfig{DownloadMedia: true, Dir: root, MaxBytes: 1024},
		OCR:   config.OCRConfig{Enabled: &enabled, Provider: "stub"},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Enhance(context.Background(), map[string]any{
				"content_type": "article",
				"media":        []any{map[string]any{"type": "image", "artifact_id": id}},
			}, cfg, EnhanceOptions{CallID: "client-controlled-id", IncludeOCR: true, ArtifactStore: store})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	dirs := skillAssetsDirs(t, root)
	if len(dirs) != 2 || dirs[0] == dirs[1] {
		t.Fatalf("concurrent executions must have distinct workspaces: %v", dirs)
	}
	got := map[string]bool{}
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join(dir, "media.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Downloaded []map[string]any `json:"downloaded"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if len(manifest.Downloaded) != 1 {
			t.Fatalf("workspace %s manifest=%+v", dir, manifest.Downloaded)
		}
		id, _ := manifest.Downloaded[0]["artifact_id"].(string)
		got[id] = true
	}
	if !got["first"] || !got["second"] {
		t.Fatalf("concurrent workspaces crossed media state: %v", got)
	}
}

func singleSkillAssetsDir(t *testing.T, root string) string {
	t.Helper()
	dirs := skillAssetsDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("expected one Skill media workspace in %s, got %v", root, dirs)
	}
	return dirs[0]
}

func skillAssetsDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "digcrawl-skill-assets-") {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	return dirs
}

func TestEnhanceASRRecordsErrorForInvalidDownloadedVideo(t *testing.T) {
	enabled := true
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "001.mp4"), []byte("not a real video"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{"platform_metadata": map[string]any{}}
	changed := enrichASRFromDownloadedMedia(context.Background(), out, config.ASRConfig{Enabled: &enabled, Provider: "stub"}, []map[string]any{{"type": "video", "local": "001.mp4"}}, assetsDir)
	if !changed {
		t.Fatal("invalid media should record an ASR error")
	}
	if _, ok := platformMetadata(out)["asr_error"].(string); !ok {
		t.Fatalf("metadata=%+v", platformMetadata(out))
	}
}

func TestEnhanceASRSuccessClearsPreviousMediaError(t *testing.T) {
	enabled := true
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "002.mp4"), []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}
	ffmpeg := testutil.FakeFFmpeg(t, assetsDir)
	out := map[string]any{"platform_metadata": map[string]any{}}
	changed := enrichASRFromDownloadedMedia(context.Background(), out, config.ASRConfig{Enabled: &enabled, Provider: "stub", FFmpegPath: ffmpeg, TempDir: assetsDir}, []map[string]any{{"type": "video", "local": "001.mp4"}, {"type": "video", "local": "002.mp4"}}, assetsDir)
	if !changed || out["asr_text"] != "stub ASR transcript" {
		t.Fatalf("changed=%v output=%+v", changed, out)
	}
	if _, ok := platformMetadata(out)["asr_error"]; ok {
		t.Fatalf("stale ASR error=%+v", platformMetadata(out))
	}
}
