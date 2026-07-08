package asr

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLocalProviderParsesStdoutJSON(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(dir, "fake-asr")
	script := "#!/bin/sh\nprintf '{\"text\":\"local transcript\",\"segments\":[{\"start\":0,\"end\":1,\"text\":\"local transcript\"}]}'\n"
	if runtime.GOOS != "windows" {
		script = "#!/bin/sh\nprintf '{\"text\":\"local transcript\",\"segments\":[{\"start\":0,\"end\":1,\"text\":\"local transcript\"}]}'\n"
	}
	if err := os.WriteFile(cmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	p := &localProvider{command: cmd, model: "small", timeout: 5 * time.Second}
	result, err := p.Transcribe(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "local transcript" || len(result.Segments) != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestExpandArgs(t *testing.T) {
	got := expandArgs([]string{"{audio_path}", "--model", "{model}", "{language}"}, map[string]string{
		"audio_path": "/tmp/a.wav",
		"model":      "small",
		"language":   "",
	})
	want := []string{"/tmp/a.wav", "--model", "small"}
	if len(got) != len(want) {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%+v want=%+v", got, want)
		}
	}
}
