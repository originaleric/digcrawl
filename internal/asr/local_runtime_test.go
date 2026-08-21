package asr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalASRRuntimeReadyHonorsCallerDeadline(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "asr_venv")
	python := localASRTestRuntimePythonPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, python)
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "runtime-health-hang")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, ok := localASRRuntimeReady(ctx, runtimeDir); ok {
		t.Fatal("hung runtime health check was reported ready")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("runtime health check ignored caller deadline: %s", elapsed)
	}
}

func TestLocalASRRuntimeHealthCheckContextBoundsBackgroundCaller(t *testing.T) {
	ctx, cancel := localASRRuntimeHealthCheckContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("runtime health check context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > localASRRuntimeHealthCheckTimeout {
		t.Fatalf("runtime health check deadline=%s, want within (0, %s]", remaining, localASRRuntimeHealthCheckTimeout)
	}
}

func TestLocalProviderRuntimeHealthCheckHonorsTranscriptionDeadline(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := localASRRuntimeDir(filepath.Join(dir, "models"))
	python := localASRTestRuntimePythonPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, python)
	audio := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(audio, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "runtime-health-hang")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := (&localProvider{
		command:       "faster-whisper",
		model:         "small",
		timeout:       5 * time.Second,
		modelCacheDir: filepath.Join(dir, "models"),
	}).Transcribe(ctx, audio)
	if err == nil {
		t.Fatal("expected a deadline-bound runtime health check failure")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("runtime health check ignored transcription deadline: %s", elapsed)
	}
}

func TestExistingRuntimePublishConflictRequiresExistingDirectory(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	runtimeDir := filepath.Join(root, "asr_venv")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := os.Rename(candidate, runtimeDir)
	if err == nil {
		t.Fatal("rename unexpectedly replaced a non-empty runtime directory")
	}
	if !isExistingRuntimePublishConflict(err, runtimeDir) {
		t.Fatalf("existing runtime conflict was not recognized: %v", err)
	}
	if isExistingRuntimePublishConflict(fmt.Errorf("simulated I/O failure"), runtimeDir) {
		t.Fatal("arbitrary publish failure must not permit runtime removal")
	}
}
