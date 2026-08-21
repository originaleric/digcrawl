package asr

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
)

const localASRRuntimeHealthCheckTimeout = 10 * time.Second

// PrepareLocalRuntime creates the isolated Python runtime used by
// the generic local ASR provider. It intentionally does not download a model:
// model acquisition remains the explicit first transcription of any local
// media file, and the runtime is shared by every platform.
func PrepareLocalRuntime(ctx context.Context, cfg config.ASRConfig, bootstrapPython string) (string, error) {
	if cfg.ProviderOrDefault() != "local" {
		return "", fmt.Errorf("local ASR runtime setup requires ASR.Provider=local")
	}
	runtimeDir := localASRRuntimeDir(cfg.ModelCacheDirOrDefault())
	if python, ok := localASRRuntimeReady(ctx, runtimeDir); ok {
		return python, nil
	}
	bootstrap, err := findBootstrapPython(ctx, bootstrapPython)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(runtimeDir), 0o755); err != nil {
		return "", fmt.Errorf("create local ASR runtime parent: %w", err)
	}
	candidate, err := os.MkdirTemp(filepath.Dir(runtimeDir), ".digcrawl-asr-venv-*")
	if err != nil {
		return "", fmt.Errorf("create local ASR runtime candidate: %w", err)
	}
	defer os.RemoveAll(candidate)
	if output, err := exec.CommandContext(ctx, bootstrap, "-m", "venv", candidate).CombinedOutput(); err != nil {
		return "", fmt.Errorf("create local ASR runtime: %w", commandOutputError(output, err))
	}
	python := localASRRuntimePython(candidate)
	if python == "" {
		return "", fmt.Errorf("create local ASR runtime: virtual environment has no Python interpreter")
	}
	if output, err := exec.CommandContext(ctx, python, "-m", "pip", "install", "--upgrade", "pip", "faster-whisper").CombinedOutput(); err != nil {
		return "", fmt.Errorf("install local ASR runtime dependencies: %w", commandOutputError(output, err))
	}
	if _, ok := localASRRuntimeReady(ctx, candidate); !ok {
		return "", fmt.Errorf("install local ASR runtime dependencies: faster-whisper import check failed")
	}
	if err := os.Rename(candidate, runtimeDir); err == nil {
		return localASRRuntimePython(runtimeDir), nil
	} else if !isExistingRuntimePublishConflict(err, runtimeDir) {
		// Do not treat an arbitrary publish failure (for example permissions or
		// I/O) as evidence that the target is safe to remove.
		return "", fmt.Errorf("publish local ASR runtime: %w", err)
	}
	if python, ok := localASRRuntimeReady(ctx, runtimeDir); ok {
		return python, nil
	}
	if localASRRuntimePython(runtimeDir) != "" {
		// A runtime with an interpreter but a failed import may be temporarily
		// unavailable (or may contain state a user needs to inspect). Preserve it
		// rather than turning a failed health check into data deletion.
		return "", fmt.Errorf("existing local ASR runtime is not ready; preserve %s, verify no setup is running, then remove that runtime directory and rerun `digcrawl asr setup`", runtimeDir)
	}
	// Older DigCrawl versions built directly in runtimeDir. If such an install
	// was interrupted before its interpreter was created, replace that known
	// structural residue with the fully prepared candidate. A healthy runtime,
	// and any runtime that can still be inspected, is never removed.
	if err := os.RemoveAll(runtimeDir); err != nil {
		return "", fmt.Errorf("replace incomplete local ASR runtime: %w", err)
	}
	if err := os.Rename(candidate, runtimeDir); err != nil {
		if python, ok := localASRRuntimeReady(ctx, runtimeDir); ok {
			return python, nil
		}
		return "", fmt.Errorf("publish local ASR runtime: %w", err)
	}
	return localASRRuntimePython(runtimeDir), nil
}

func isExistingRuntimePublishConflict(renameErr error, runtimeDir string) bool {
	// Unix reports a destination directory as already existing. Windows reports
	// the same directory-to-directory publish conflict as access denied. Limit
	// the latter exception to Windows and still require an existing directory,
	// so unrelated permission or I/O failures can never authorize replacement.
	isKnownConflict := errors.Is(renameErr, fs.ErrExist) || (runtime.GOOS == "windows" && errors.Is(renameErr, fs.ErrPermission))
	if !isKnownConflict {
		return false
	}
	info, err := os.Lstat(runtimeDir)
	return err == nil && info.IsDir()
}

func localASRRuntimeDir(modelCacheDir string) string {
	return filepath.Join(filepath.Dir(modelCacheDir), "asr_venv")
}

func localASRRuntimePython(runtimeDir string) string {
	for _, candidate := range []string{
		filepath.Join(runtimeDir, "bin", "python"),
		filepath.Join(runtimeDir, "Scripts", "python.exe"),
	} {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func localASRRuntimeReady(ctx context.Context, runtimeDir string) (string, bool) {
	python := localASRRuntimePython(runtimeDir)
	if python == "" {
		return "", false
	}
	if !localASRImportReady(ctx, python) {
		return "", false
	}
	return python, true
}

func localASRImportReady(ctx context.Context, python string) bool {
	checkCtx, cancel := localASRRuntimeHealthCheckContext(ctx)
	defer cancel()
	return exec.CommandContext(checkCtx, python, "-c", "import faster_whisper").Run() == nil
}

func localASRRuntimeHealthCheckContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, localASRRuntimeHealthCheckTimeout)
}

func findBootstrapPython(ctx context.Context, override string) (string, error) {
	var candidates []string
	if override = strings.TrimSpace(override); override != "" {
		candidates = append(candidates, override)
	} else {
		// Prefer python before python3 on this host: python3 may point to a
		// bleeding-edge interpreter without CTranslate2 wheels, while python is
		// the supported project runtime.
		candidates = []string{"python", "python3", "py"}
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		checkCtx, cancel := localASRRuntimeHealthCheckContext(ctx)
		err = exec.CommandContext(checkCtx, path, "-c", "import sys; assert sys.version_info >= (3, 9) and sys.version_info < (3, 14)").Run()
		cancel()
		if err == nil {
			return path, nil
		}
	}
	if override != "" {
		return "", fmt.Errorf("configured Python %q is unavailable or unsupported; require Python 3.9 through 3.13", override)
	}
	return "", fmt.Errorf("no supported Python interpreter found; require Python 3.9 through 3.13")
}

func commandOutputError(output []byte, err error) error {
	if message := strings.TrimSpace(string(output)); message != "" {
		return fmt.Errorf("%s: %w", message, err)
	}
	return err
}
