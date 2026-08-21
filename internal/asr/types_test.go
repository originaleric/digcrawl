package asr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/testutil"
)

func TestLocalASRTimeoutForDurationUsesConfiguredFloor(t *testing.T) {
	if got, want := localASRTimeoutForDuration(20*time.Minute, 7*time.Minute), 20*time.Minute; got != want {
		t.Fatalf("timeout=%s want %s", got, want)
	}
}

func TestLocalASRTimeoutForDurationScalesLongMedia(t *testing.T) {
	if got, want := localASRTimeoutForDuration(5*time.Minute, 426*time.Second), 14*time.Minute+32*time.Second; got != want {
		t.Fatalf("timeout=%s want %s", got, want)
	}
}

func TestEffectiveASRTimeoutUsesProbedDurationForLocalProvider(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, filepath.Join(dir, "ffprobe"))
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "ffprobe")
	got := effectiveASRTimeout(context.Background(), mediaPath, config.ASRConfig{
		Provider:   "local",
		TimeoutSec: 1,
		FFmpegPath: filepath.Join(dir, "ffmpeg"),
	})
	if want := 14*time.Minute + 33*time.Second + 853*time.Millisecond; got != want {
		t.Fatalf("timeout=%s want %s", got, want)
	}
}

func TestTranscribePropagatesDynamicTimeoutToLocalProvider(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(dir, "ffmpeg")
	data, err := os.ReadFile(testutil.FakeFFmpeg(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ffmpeg, data, 0o700); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, filepath.Join(dir, "ffprobe"))
	localCommand := filepath.Join(dir, "local-asr")
	copyLocalASRTestCommand(t, localCommand)
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "slow-transcript-with-probe")

	result, err := Transcribe(context.Background(), mediaPath, config.ASRConfig{
		Provider:      "local",
		TimeoutSec:    1,
		FFmpegPath:    ffmpeg,
		LocalCommand:  localCommand,
		ModelCacheDir: filepath.Join(dir, "models"),
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "local transcript" {
		t.Fatalf("text=%q", result.Text)
	}
}

func TestTranscribeRejectsDynamicBudgetAboveMaximum(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, filepath.Join(dir, "ffprobe"))
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "ffprobe")

	_, err := Transcribe(context.Background(), mediaPath, config.ASRConfig{
		Provider:      "local",
		TimeoutSec:    1,
		MaxTimeoutSec: 60,
		FFmpegPath:    filepath.Join(dir, "ffmpeg"),
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "exceeding ASR.MaxTimeoutSec") {
		t.Fatalf("error=%v, want maximum-budget guidance", err)
	}
}

func TestTranscribeRejectsFloorAboveMaximum(t *testing.T) {
	_, err := Transcribe(context.Background(), "unused.mp4", config.ASRConfig{
		Provider:      "local",
		TimeoutSec:    120,
		MaxTimeoutSec: 60,
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "ASR.TimeoutSec") {
		t.Fatalf("error=%v, want floor/maximum configuration guidance", err)
	}
}

func TestTranscribeCountsDurationProbeInsideOperationBudget(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(dir, "ffmpeg")
	data, err := os.ReadFile(testutil.FakeFFmpeg(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ffmpeg, data, 0o700); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, filepath.Join(dir, "ffprobe"))
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "slow-probe")

	started := time.Now()
	_, err = Transcribe(context.Background(), mediaPath, config.ASRConfig{
		Provider:      "local",
		TimeoutSec:    1,
		MaxTimeoutSec: 1800,
		FFmpegPath:    ffmpeg,
		LocalCommand:  localASRTestCommand(t),
	}, Options{})
	if err == nil {
		t.Fatal("expected operation budget exhaustion after slow duration probe")
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("duration probe escaped operation budget: %s", elapsed)
	}
}
