package asr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
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

func TestLocalASRImportCheckPreservesInterpreterDiagnostics(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "asr_venv")
	python := localASRTestRuntimePythonPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, python)
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "runtime-health-fail")

	err := localASRImportCheck(context.Background(), python)
	if err == nil {
		t.Fatal("expected failed local ASR import check")
	}
	if got := err.Error(); !strings.Contains(got, "faster_whisper import failed: missing dependency") {
		t.Fatalf("import diagnostics were discarded: %v", err)
	}
}

func TestPrepareLocalRuntimeReportsExistingRuntimeImportFailureAfterPublishConflict(t *testing.T) {
	root := t.TempDir()
	modelCacheDir := filepath.Join(root, "asr_models")
	runtimeDir := localASRRuntimeDir(modelCacheDir)
	existingPython := localASRTestRuntimePythonPath(runtimeDir)
	if err := os.MkdirAll(filepath.Dir(existingPython), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, existingPython)
	bootstrap := localASRTestExecutablePath(t.TempDir(), "python")
	copyLocalASRTestCommand(t, bootstrap)
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "runtime-existing-health-fail")

	_, err := PrepareLocalRuntime(context.Background(), config.ASRConfig{
		Provider:      "local",
		ModelCacheDir: modelCacheDir,
	}, bootstrap)
	if err == nil {
		t.Fatal("expected existing runtime publish conflict to fail")
	}
	if got := err.Error(); !strings.Contains(got, "faster_whisper import failed: existing runtime is broken") {
		t.Fatalf("existing runtime diagnostics were discarded: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "preserve "+runtimeDir) {
		t.Fatalf("existing runtime preservation guidance missing: %v", err)
	}
}

func TestPrepareLocalRuntimeReportsLinkedRuntimeImportFailureAfterPublishConflict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating directory symlinks requires privileges on some Windows hosts")
	}
	root := t.TempDir()
	modelCacheDir := filepath.Join(root, "asr_models")
	runtimeDir := localASRRuntimeDir(modelCacheDir)
	legacyRuntimeDir := filepath.Join(root, "legacy-asr-venv")
	existingPython := localASRTestRuntimePythonPath(legacyRuntimeDir)
	if err := os.MkdirAll(filepath.Dir(existingPython), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, existingPython)
	if err := os.Symlink(legacyRuntimeDir, runtimeDir); err != nil {
		t.Fatal(err)
	}
	bootstrap := localASRTestExecutablePath(t.TempDir(), "python")
	copyLocalASRTestCommand(t, bootstrap)
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "runtime-existing-health-fail")

	_, err := PrepareLocalRuntime(context.Background(), config.ASRConfig{
		Provider:      "local",
		ModelCacheDir: modelCacheDir,
	}, bootstrap)
	if err == nil {
		t.Fatal("expected linked existing runtime publish conflict to fail")
	}
	if got := err.Error(); !strings.Contains(got, "faster_whisper import failed: existing runtime is broken") {
		t.Fatalf("linked runtime diagnostics were discarded: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "preserve "+runtimeDir) {
		t.Fatalf("linked runtime preservation guidance missing: %v", err)
	}
	if info, statErr := os.Lstat(runtimeDir); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("linked runtime was not preserved: info=%v err=%v", info, statErr)
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

func TestPrepareLocalModelUsesPreparedRuntimeAndConfiguredCache(t *testing.T) {
	root := t.TempDir()
	modelCacheDir := filepath.Join(root, "asr_models")
	runtimePython := localASRTestRuntimePythonPath(localASRRuntimeDir(modelCacheDir))
	if err := os.MkdirAll(filepath.Dir(runtimePython), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLocalASRTestCommand(t, runtimePython)
	t.Setenv("DIGCRAWL_TEST_LOCAL_ASR_MODE", "prepare-model")

	if err := PrepareLocalModel(context.Background(), config.ASRConfig{
		Provider:      "local",
		Model:         "base",
		ModelCacheDir: modelCacheDir,
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(modelCacheDir, "digcrawl-runner", "faster_whisper_asr-*.py"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("bundled runner was not materialized under model cache: entries=%q err=%v", entries, err)
	}
}

func TestPrepareLocalModelRejectsCustomLocalCommand(t *testing.T) {
	err := ValidateLocalModelSetup(config.ASRConfig{
		Provider:     "local",
		LocalCommand: "custom-asr",
	})
	if err == nil || !strings.Contains(err.Error(), "custom ASR.LocalCommand") {
		t.Fatalf("error=%v", err)
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
