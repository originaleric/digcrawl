package asr

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestLocalProviderSetsWritableHuggingFaceCache(t *testing.T) {
	t.Setenv("HF_HOME", "")
	t.Setenv("HUGGINGFACE_HUB_CACHE", "")

	dir := t.TempDir()
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(dir, "fake-asr")
	script := "#!/bin/sh\ncase \"$HF_HOME\" in */.cache/huggingface) ;; *) echo \"bad HF_HOME=$HF_HOME\" >&2; exit 2;; esac\nprintf '{\"text\":\"local transcript\"}'\n"
	if err := os.WriteFile(cmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	p := &localProvider{command: cmd, model: "small", timeout: 5 * time.Second}
	if _, err := p.Transcribe(context.Background(), audio); err != nil {
		t.Fatal(err)
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

func TestLocalProviderMissingCommandReturnsReadinessError(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &localProvider{command: "digcrawl-missing-local-asr-command", model: "small", timeout: time.Second}
	_, err := p.Transcribe(context.Background(), audio)
	if err == nil {
		t.Fatal("expected missing command error")
	}
	if !strings.Contains(err.Error(), "ASR.LocalCommand") || !strings.Contains(err.Error(), "--local-command") {
		t.Fatalf("error is not actionable: %v", err)
	}
}
