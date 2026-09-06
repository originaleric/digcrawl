package video

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/internal/media"
	"github.com/originaleric/digcrawl/internal/ocr"
)

func TestProcessorsForMode(t *testing.T) {
	enabled := true
	disabled := false
	cfg := &config.Config{ASR: config.ASRConfig{Enabled: &enabled}, OCR: config.OCRConfig{
		Enabled: &enabled, VideoFrames: config.VideoFrameOCRConfig{Enabled: &enabled},
	}}
	for _, tc := range []struct {
		mode        string
		wantASR     bool
		wantOCR     bool
		wantErrText string
	}{
		{mode: "auto", wantASR: true, wantOCR: true},
		{mode: "asr", wantASR: true},
		{mode: "ocr", wantOCR: true},
		{mode: "both", wantASR: true, wantOCR: true},
		{mode: "unknown", wantErrText: "unsupported video mode"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			asr, ocr, err := processorsForMode(tc.mode, cfg)
			if tc.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrText) {
					t.Fatalf("error=%v want %q", err, tc.wantErrText)
				}
				return
			}
			if err != nil || asr != tc.wantASR || ocr != tc.wantOCR {
				t.Fatalf("asr=%v ocr=%v err=%v", asr, ocr, err)
			}
		})
	}
	cfg.ASR.Enabled = &disabled
	cfg.OCR.VideoFrames.Enabled = &disabled
	if _, _, err := processorsForMode("auto", cfg); err == nil || !strings.Contains(err.Error(), "no enabled processor") {
		t.Fatalf("auto error=%v", err)
	}
}

func TestRemoteURLValidationAndExtension(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/video.mp4",
		"https://[::1]/video.mp4",
		"https://localhost/video.mp4",
		"https://user:pass@example.com/video.mp4",
		"file:///tmp/video.mp4",
	} {
		if err := media.ValidateRemoteURL(raw); err == nil {
			t.Fatalf("ValidateRemoteURL(%q) succeeded", raw)
		}
	}
	if err := media.ValidateRemoteURL("https://cdn.example.com/video.mp4?token=a"); err != nil {
		t.Fatal(err)
	}
	if got := safeExtension("/asset.webm"); got != ".webm" {
		t.Fatalf("extension=%q", got)
	}
	if got := safeExtension("/asset.invalid-extension"); got != ".mp4" {
		t.Fatalf("extension=%q", got)
	}
}

func TestDurationProbeUsesActiveProcessorAndIsAdvisory(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		ASR: config.ASRConfig{Enabled: &enabled, FFmpegPath: "/asr/bin/ffmpeg"},
		OCR: config.OCRConfig{Enabled: &enabled, VideoFrames: config.VideoFrameOCRConfig{Enabled: &enabled, FFmpegPath: "/ocr/bin/ffmpeg"}},
	}
	if got := durationFFmpegPath(cfg, false, true); got != "/ocr/bin/ffmpeg" {
		t.Fatalf("OCR ffmpeg=%q", got)
	}
	if got := durationFFmpegPath(cfg, true, true); got != "/asr/bin/ffmpeg" {
		t.Fatalf("ASR ffmpeg=%q", got)
	}
	path := filepath.Join(t.TempDir(), "not-a-video.mp4")
	if err := os.WriteFile(path, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if duration, warning := probeVideoDuration(context.Background(), path, cfg, false, true); duration != 0 || !strings.Contains(warning, "video duration unavailable") {
		t.Fatalf("duration=%s warning=%q", duration, warning)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if duration, warning := probeVideoDuration(cancelled, path, cfg, false, true); duration != 0 || !strings.Contains(warning, "video duration unavailable") {
		t.Fatalf("cancelled duration=%s warning=%q", duration, warning)
	}
}

func TestCopyFileNewDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	dest := filepath.Join(dir, "saved.mp4")
	if err := os.WriteFile(source, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFileNew(source, dest); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "video" {
		t.Fatalf("saved=%q err=%v", got, err)
	}
	if err := copyFileNew(source, dest); err == nil {
		t.Fatal("copyFileNew overwrote existing destination")
	}
}

func TestValidateKeepDestinationFailsBeforeDownload(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.mp4")
	if err := os.WriteFile(existing, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateKeepDestination(existing); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing destination error=%v", err)
	}
	missingParent := filepath.Join(dir, "missing", "video.mp4")
	if err := validateKeepDestination(missingParent); err == nil || !strings.Contains(err.Error(), "parent directory") {
		t.Fatalf("missing parent error=%v", err)
	}
	newPath := filepath.Join(dir, "new.mp4")
	if err := validateKeepDestination(newPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("preflight created destination, stat err=%v", err)
	}
	enabled := true
	_, err := Analyze(context.Background(), "https://unresolvable.invalid/video.mp4", &config.Config{
		ASR: config.ASRConfig{Enabled: &enabled},
	}, Options{KeepPath: existing})
	if err == nil || !strings.Contains(err.Error(), "validate keep destination") {
		t.Fatalf("Analyze error=%v", err)
	}
}

func TestCombinedTextAndWarnings(t *testing.T) {
	result := &Result{
		ASR: ASRStage{Status: "ok", Result: &asr.Result{Text: "speech"}},
		OCR: OCRStage{Status: "error", Error: "provider unavailable", Results: []ocr.Result{{Text: "caption"}}},
	}
	got := combinedText(result)
	if !strings.Contains(got, "## ASR") || !strings.Contains(got, "speech") || !strings.Contains(got, "## OCR") || !strings.Contains(got, "caption") {
		t.Fatalf("combined text=%q", got)
	}
	if warnings := stageWarnings(result); len(warnings) != 1 || !strings.Contains(warnings[0], "provider unavailable") {
		t.Fatalf("warnings=%+v", warnings)
	}
}
