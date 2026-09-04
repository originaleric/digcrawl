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
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/config"
)

const localASRRuntimeHealthCheckTimeout = 10 * time.Second

// PrepareLocalRuntime creates the isolated Python runtime used by the generic
// local ASR provider. Model preparation is deliberately separate so callers
// that need a ready-to-transcribe default can make that network download an
// explicit, confirmed step.
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
	if err := localASRImportCheck(ctx, python); err != nil {
		return "", fmt.Errorf("install local ASR runtime dependencies: %w", err)
	}
	if err := os.Rename(candidate, runtimeDir); err == nil {
		return localASRRuntimePython(runtimeDir), nil
	} else if !isExistingRuntimePublishConflict(err, runtimeDir) {
		// Do not treat an arbitrary publish failure (for example permissions or
		// I/O) as evidence that the target is safe to remove.
		return "", fmt.Errorf("publish local ASR runtime: %w", err)
	}
	if existingPython := localASRRuntimePython(runtimeDir); existingPython != "" {
		if err := localASRImportCheck(ctx, existingPython); err == nil {
			// Another setup invocation finished while this one prepared its
			// candidate. Its published runtime is now the source of truth.
			return existingPython, nil
		}
		// The runtime may have been replaced or removed while its health check
		// was running. Re-read it before deciding whether preservation is safe.
		if currentPython := localASRRuntimePython(runtimeDir); currentPython != "" {
			if err := localASRImportCheck(ctx, currentPython); err == nil {
				return currentPython, nil
			} else {
				return "", fmt.Errorf("existing local ASR runtime is not ready; preserve %s, verify no setup is running, then remove that runtime directory and rerun `digcrawl asr setup`: %w", runtimeDir, err)
			}
		}
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

// ValidateLocalModelSetup verifies that cfg uses the portable faster-whisper
// runner. Custom ASR.LocalCommand integrations own their model lifecycle and
// must not be invoked with runner-specific flags.
func ValidateLocalModelSetup(cfg config.ASRConfig) error {
	if cfg.ProviderOrDefault() != "local" {
		return fmt.Errorf("local ASR model setup requires ASR.Provider=local")
	}
	command := strings.ToLower(strings.TrimSpace(cfg.LocalCommand))
	if len(cfg.LocalArgs) != 0 || (command != "" && command != "faster-whisper" && command != "faster_whisper") {
		return fmt.Errorf("local ASR model setup is only available with the default faster-whisper runner; custom ASR.LocalCommand/LocalArgs manage their own models")
	}
	return nil
}

// PrepareLocalModel downloads the configured faster-whisper model into the
// same cache used by local transcription and doctor.
func PrepareLocalModel(ctx context.Context, cfg config.ASRConfig) error {
	if err := ValidateLocalModelSetup(cfg); err != nil {
		return err
	}
	modelCacheDir := cfg.ModelCacheDirOrDefault()
	python, ok := localASRRuntimeReady(ctx, localASRRuntimeDir(modelCacheDir))
	if !ok {
		return fmt.Errorf("isolated local ASR runtime is not ready; run `digcrawl asr setup` again")
	}
	runner, err := ensureBundledFasterWhisperRunner(modelCacheDir, cfg.LegacyModelCacheDirs, cfg.LegacyHubCacheDir)
	if err != nil {
		return fmt.Errorf("prepare bundled faster-whisper runner: %w", err)
	}
	env, err := localASREnv(os.Environ(), modelCacheDir, cfg.LegacyModelCacheDirs, cfg.LegacyHubCacheDir)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, python, runner, "--download-model", "--model", cfg.ModelOrDefault())
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("download local ASR model: cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("download local ASR model %q: %w", cfg.ModelOrDefault(), commandOutputError(output, err))
	}
	return nil
}

func isExistingRuntimePublishConflict(renameErr error, runtimeDir string) bool {
	// Follow a compatibility symlink here. It must be preserved just like a
	// directory: the caller will health-check its target and never remove it
	// automatically.
	info, err := os.Stat(runtimeDir)
	if err != nil || !info.IsDir() {
		return false
	}
	// Unix reports a destination directory as already existing. Windows reports
	// the same directory-to-directory publish conflict as access denied. macOS
	// reports ENOTDIR when that destination is a symlink to a directory. Limit
	// every exception to an existing directory target, so unrelated permission
	// or I/O failures can never authorize replacement.
	if errors.Is(renameErr, fs.ErrExist) || (runtime.GOOS == "windows" && errors.Is(renameErr, fs.ErrPermission)) {
		return true
	}
	linkInfo, lstatErr := os.Lstat(runtimeDir)
	return lstatErr == nil && linkInfo.Mode()&os.ModeSymlink != 0 && errors.Is(renameErr, syscall.ENOTDIR)
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
	return localASRImportCheck(ctx, python) == nil
}

// localASRImportCheck preserves the interpreter's diagnostics for setup while
// keeping readiness checks cheap for callers that only need a boolean answer.
func localASRImportCheck(ctx context.Context, python string) error {
	checkCtx, cancel := localASRRuntimeHealthCheckContext(ctx)
	defer cancel()
	output, err := exec.CommandContext(checkCtx, python, "-c", "import faster_whisper").CombinedOutput()
	if err == nil {
		return nil
	}
	if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("faster-whisper import check timed out after %s", localASRRuntimeHealthCheckTimeout)
	}
	return fmt.Errorf("faster-whisper import check failed: %w", commandOutputError(output, err))
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
