package media

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractAudioUsesFFmpegCommand(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(input, []byte("fake video"), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nfor out do :; done\nprintf 'wav' > \"$out\"\n"
	if runtime.GOOS != "windows" {
		script = "#!/bin/sh\nfor out do :; done\nprintf 'wav' > \"$out\"\n"
	}
	if err := os.WriteFile(ffmpeg, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

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
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nfor out do :; done\nprintf 'wav' > \"$out\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
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
