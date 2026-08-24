package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/internal/testutil"
)

func TestExtractAudioUsesFFmpegCommand(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(input, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := testutil.FakeFFmpeg(t, dir)

	audio, err := ExtractAudio(context.Background(), input, AudioExtractOptions{
		FFmpegPath: ffmpeg,
		TempDir:    dir,
		SampleRate: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Cleanup()
	if audio.SampleRate != 8000 || audio.Format != "wav" {
		t.Fatalf("unexpected audio: %+v", audio)
	}
	data, err := os.ReadFile(audio.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "wav" {
		t.Fatalf("unexpected audio payload %q", data)
	}
}

func TestParseProbeDuration(t *testing.T) {
	got, err := parseProbeDuration("426.926500\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := 426*time.Second + 926500*time.Microsecond; got != want {
		t.Fatalf("duration=%s want %s", got, want)
	}
}

func TestParseProbeDurationRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "N/A", "0", "-1"} {
		if _, err := parseProbeDuration(raw); err == nil {
			t.Fatalf("parseProbeDuration(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestExtractAudioCreatesConfiguredTempDirectory(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(input, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := testutil.FakeFFmpeg(t, dir)
	baseTemp := filepath.Join(dir, "missing", "asr")
	audio, err := ExtractAudio(context.Background(), input, AudioExtractOptions{FFmpegPath: ffmpeg, TempDir: baseTemp})
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Cleanup()
	if filepath.Dir(filepath.Dir(audio.Path)) != baseTemp {
		t.Fatalf("audio temp path %q not under configured base %q", audio.Path, baseTemp)
	}
}

func TestExtractVideoFramesCreatesPrivateTemporaryFrames(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(input, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	frames, err := ExtractVideoFrames(context.Background(), input, VideoFrameExtractOptions{
		FFmpegPath:  testutil.FakeFFmpeg(t, dir),
		TempDir:     filepath.Join(dir, "frames"),
		IntervalSec: 5,
		MaxFrames:   8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames.Paths) != 1 {
		t.Fatalf("frame paths=%q", frames.Paths)
	}
	if _, err := os.Stat(frames.Paths[0]); err != nil {
		t.Fatalf("extracted frame missing: %v", err)
	}
	frameDir := frames.Dir
	frames.Cleanup()
	if _, err := os.Stat(frameDir); !os.IsNotExist(err) {
		t.Fatalf("temporary frame directory still exists, err=%v", err)
	}
}

func TestExtractVideoFramesRejectsUnboundedSampling(t *testing.T) {
	input := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(input, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractVideoFrames(context.Background(), input, VideoFrameExtractOptions{IntervalSec: 0, MaxFrames: 8}); err == nil {
		t.Fatal("expected invalid frame interval error")
	}
	if _, err := ExtractVideoFrames(context.Background(), input, VideoFrameExtractOptions{IntervalSec: 5, MaxFrames: 0}); err == nil {
		t.Fatal("expected invalid maximum frame count error")
	}
}
