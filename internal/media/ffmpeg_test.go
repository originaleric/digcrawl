package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
