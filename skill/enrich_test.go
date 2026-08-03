package skill

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestEnhanceUsesSkillOCRConfig(t *testing.T) {
	enabled := true
	out := map[string]any{"markdown": "body", "content_type": "article"}
	err := Enhance(context.Background(), out, config.SkillConfig{
		OCR: config.OCRConfig{Enabled: &enabled, Provider: "stub"},
	}, EnhanceOptions{CallID: "skill-ocr", IncludeOCR: true, ScreenshotBase64: "iVBORw0KGgo="})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out["markdown"].(string), "stub OCR text from screenshot") {
		t.Fatalf("markdown=%q", out["markdown"])
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
			opts: EnhanceOptions{CallID: "ocr-disabled", IncludeOCR: true},
			want: "Skill.OCR.Enabled",
		},
		{
			name: "ASR media disabled",
			out:  map[string]any{"content_type": "video"},
			cfg:  config.SkillConfig{ASR: config.ASRConfig{Enabled: &enabled, Provider: "stub"}},
			opts: EnhanceOptions{CallID: "asr-media-disabled", IncludeASR: true},
			want: "Skill.Media.DownloadMedia",
		},
		{
			name: "video summary disabled before download",
			out:  map[string]any{"content_type": "video", "media": []any{map[string]any{"type": "video", "url": "https://example.com/video.mp4"}}},
			cfg: config.SkillConfig{
				Media: config.SkillMediaConfig{DownloadMedia: true, Dir: mediaDir},
				ASR:   config.ASRConfig{Enabled: &enabled, Provider: "stub"},
			},
			opts: EnhanceOptions{CallID: "summary-disabled", IncludeSummary: true},
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
	}, EnhanceOptions{CallID: "asr-no-media", IncludeASR: true})
	if err == nil || !strings.Contains(err.Error(), "ASR unavailable: no downloaded video/audio asset was available for ASR") {
		t.Fatalf("error=%v", err)
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
	if _, err := skillAssetsDir("relative", "call"); err == nil {
		t.Fatal("expected relative media directory rejection")
	}
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
	if runtime.GOOS == "windows" {
		t.Skip("shell fake ffmpeg is not portable on windows")
	}
	enabled := true
	assetsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(assetsDir, "002.mp4"), []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(assetsDir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nfor out do :; done\nprintf 'wav' > \"$out\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{"platform_metadata": map[string]any{}}
	changed := enrichASRFromDownloadedMedia(context.Background(), out, config.ASRConfig{Enabled: &enabled, Provider: "stub", FFmpegPath: ffmpeg, TempDir: assetsDir}, []map[string]any{{"type": "video", "local": "001.mp4"}, {"type": "video", "local": "002.mp4"}}, assetsDir)
	if !changed || out["asr_text"] != "stub ASR transcript" {
		t.Fatalf("changed=%v output=%+v", changed, out)
	}
	if _, ok := platformMetadata(out)["asr_error"]; ok {
		t.Fatalf("stale ASR error=%+v", platformMetadata(out))
	}
}
