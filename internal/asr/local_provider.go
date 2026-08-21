package asr

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type localProvider struct {
	command              string
	args                 []string
	model                string
	language             string
	timeout              time.Duration
	modelCacheDir        string
	legacyModelCacheDirs []string
	legacyHubCacheDir    string
}

// bundledFasterWhisperRunner is materialized beneath the configured model
// cache when the faster-whisper console command is unavailable. Embedding it
// keeps the fallback independent of the current directory and of whether a
// release package exposes a repository-level scripts/ directory.
//
//go:embed assets/faster_whisper_asr.py
var bundledFasterWhisperRunner []byte

func (p *localProvider) Name() string  { return "local" }
func (p *localProvider) Model() string { return p.model }

func (p *localProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	info, err := os.Stat(audioPath)
	if err != nil {
		return nil, fmt.Errorf("stat audio: %w", err)
	}
	// A successful `asr setup` establishes one isolated dependency runtime for
	// every local media file. Prefer that runtime even when an unrelated
	// faster-whisper executable happens to be present on PATH; otherwise the
	// selected model/cache and dependency versions would depend on shell state.
	if localASRRuntimePython(localASRRuntimeDir(p.modelCacheDir)) != "" {
		fallbackCommand, fallbackArgs, fallbackErr := localCommandFallback(ctx, p.command, p.args, p.modelCacheDir, p.legacyModelCacheDirs, p.legacyHubCacheDir)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		if fallbackCommand != "" {
			p.command = fallbackCommand
			p.args = fallbackArgs
		}
	}
	if _, err := exec.LookPath(p.command); err != nil {
		fallbackCommand, fallbackArgs, fallbackErr := localCommandFallback(ctx, p.command, p.args, p.modelCacheDir, p.legacyModelCacheDirs, p.legacyHubCacheDir)
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		if fallbackCommand == "" {
			return nil, fmt.Errorf("local ASR command %q not found in PATH; install it or configure ASR.LocalCommand / --local-command", p.command)
		}
		p.command = fallbackCommand
		p.args = fallbackArgs
	}
	outDir, err := os.MkdirTemp("", "digcrawl-asr-local-*")
	if err != nil {
		return nil, fmt.Errorf("create local ASR temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	args := p.args
	if len(args) == 0 {
		args = []string{"{audio_path}", "--model", "{model}", "--output_format", "json", "--output_dir", "{output_dir}"}
		if p.language != "" {
			args = append(args, "--language", "{language}")
		}
	}
	args = expandArgs(args, map[string]string{
		"audio_path": audioPath,
		"model":      p.model,
		"language":   p.language,
		"output_dir": outDir,
	})

	cmdCtx := ctx
	cancel := func() {}
	if p.timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, p.timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, p.command, args...)
	env, envErr := localASREnv(os.Environ(), p.modelCacheDir, p.legacyModelCacheDirs, p.legacyHubCacheDir)
	if envErr != nil {
		return nil, envErr
	}
	cmd.Env = env
	stdout, err := cmd.Output()
	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("local ASR timed out after its operation budget; adjust ASR.TimeoutSec, ASR.MaxTimeoutSec, or --timeout for this transcription")
		}
		if errors.Is(cmdCtx.Err(), context.Canceled) {
			return nil, fmt.Errorf("local ASR cancelled: %w", cmdCtx.Err())
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("local ASR failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("local ASR failed: %w", err)
	}

	result, parseErr := parseTranscriptJSON(stdout)
	if parseErr != nil || strings.TrimSpace(result.Text) == "" {
		result, parseErr = readTranscriptFromDir(outDir)
	}
	if parseErr != nil {
		text := strings.TrimSpace(string(stdout))
		if text == "" {
			return nil, parseErr
		}
		result = &Result{Text: text}
	}
	result.Audio.SizeBytes = info.Size()
	return result, nil
}

func localASREnv(base []string, modelCacheDir string, legacyModelCacheDirs []string, legacyHubCacheDir string) ([]string, error) {
	env := append([]string{}, base...)
	cache, err := resolveLocalASRCacheLocation(modelCacheDir, legacyModelCacheDirs, legacyHubCacheDir)
	if err != nil {
		return nil, err
	}
	// ModelCacheDir is the configured source of truth for both normal
	// transcription and the bundled runner's offline preflight. Remove a
	// process-global hub override so it cannot silently split model state across
	// two directories, then pass the path explicitly to Python as well.
	env = setEnvValue(env, "HF_HOME", cache.modelCacheDir)
	env = removeEnvValue(env, "HUGGINGFACE_HUB_CACHE")
	if cache.hubCacheDir != "" {
		env = setEnvValue(env, "HUGGINGFACE_HUB_CACHE", cache.hubCacheDir)
	}
	env = setEnvValue(env, "DIGCRAWL_ASR_MODEL_CACHE_DIR", cache.modelCacheDir)
	env = removeEnvValue(env, "DIGCRAWL_ASR_LEGACY_HUB_CACHE_DIR")
	if cache.hubCacheDir != "" {
		env = setEnvValue(env, "DIGCRAWL_ASR_LEGACY_HUB_CACHE_DIR", cache.hubCacheDir)
	}
	if !envHasValue(env, "HF_HUB_DISABLE_PROGRESS_BARS") {
		env = append(env, "HF_HUB_DISABLE_PROGRESS_BARS=1")
	}
	return env, nil
}

func setEnvValue(env []string, key, value string) []string {
	env = removeEnvValue(env, key)
	return append(env, key+"="+value)
}

func removeEnvValue(env []string, key string) []string {
	prefix := key + "="
	out := env[:0]
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func localASRCacheDir(configured string) (string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured, nil
	}
	wd, err := os.Getwd()
	if err == nil && strings.TrimSpace(wd) != "" {
		return filepath.Join(wd, "storage", "digcrawl", "asr_models"), nil
	}
	tmp := os.TempDir()
	if strings.TrimSpace(tmp) == "" {
		return "", fmt.Errorf("resolve local ASR model cache dir: empty temp dir")
	}
	return filepath.Join(tmp, "digcrawl-asr-models"), nil
}

const localASRCacheStateName = ".digcrawl-asr-cache-v1.json"

type localASRCacheState struct {
	ActiveModelCacheDir string `json:"active_model_cache_dir"`
	HubCacheDir         string `json:"hub_cache_dir,omitempty"`
}

type localASRCacheLocation struct {
	modelCacheDir string
	hubCacheDir   string
}

// resolveLocalASRCacheDir selects one stable cache for a provider invocation.
// The configured directory remains the canonical owner of compatibility state.
// For an old profile with no explicit cache, an existing legacy cache is adopted
// in place and recorded atomically rather than moved: model downloads are large,
// may cross volumes, and may also be shared with an earlier installation.
func resolveLocalASRCacheDir(configured string, legacyCandidates []string) (string, error) {
	cache, err := resolveLocalASRCacheLocation(configured, legacyCandidates, "")
	if err != nil {
		return "", err
	}
	return cache.modelCacheDir, nil
}

func resolveLocalASRCacheLocation(configured string, legacyCandidates []string, legacyHubCacheDir string) (localASRCacheLocation, error) {
	canonical, err := localASRCacheDir(configured)
	if err != nil {
		return localASRCacheLocation{}, err
	}
	canonical = filepath.Clean(canonical)
	if err := validateASRCacheDirectory(canonical); err != nil {
		return localASRCacheLocation{}, err
	}
	if hasASRModelData(canonical) {
		return localASRCacheLocation{modelCacheDir: canonical}, nil
	}
	if state, ok := readLocalASRCacheState(canonical); ok {
		activeIsCanonical := filepath.Clean(state.ActiveModelCacheDir) == canonical
		if (activeIsCanonical || validASRCacheCandidate(state.ActiveModelCacheDir, canonical)) &&
			(state.HubCacheDir == "" || validASRCacheDirectory(state.HubCacheDir)) {
			return localASRCacheLocation{modelCacheDir: state.ActiveModelCacheDir, hubCacheDir: state.HubCacheDir}, nil
		}
	}
	legacyHubCacheDir = filepath.Clean(strings.TrimSpace(legacyHubCacheDir))
	if legacyHubCacheDir != "." && validASRCacheDirectory(legacyHubCacheDir) && hasASRModelData(legacyHubCacheDir) {
		state := localASRCacheState{ActiveModelCacheDir: canonical, HubCacheDir: legacyHubCacheDir}
		if err := writeLocalASRCacheState(canonical, state); err != nil {
			return localASRCacheLocation{}, fmt.Errorf("record ASR cache compatibility state: %w", err)
		}
		return localASRCacheLocation{modelCacheDir: canonical, hubCacheDir: legacyHubCacheDir}, nil
	}
	for _, candidate := range legacyCandidates {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if !validASRCacheCandidate(candidate, canonical) || !hasASRModelData(candidate) {
			continue
		}
		state := localASRCacheState{ActiveModelCacheDir: candidate}
		if err := writeLocalASRCacheState(canonical, state); err != nil {
			return localASRCacheLocation{}, fmt.Errorf("record ASR cache compatibility state: %w", err)
		}
		return localASRCacheLocation{modelCacheDir: candidate}, nil
	}
	return localASRCacheLocation{modelCacheDir: canonical}, nil
}

func validateASRCacheDirectory(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("ASR model cache path is not a directory: %s", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return nil
}

func validASRCacheCandidate(candidate, canonical string) bool {
	if candidate == "" || filepath.Clean(candidate) == filepath.Clean(canonical) {
		return false
	}
	info, err := os.Stat(candidate)
	return err == nil && info.IsDir()
}

func validASRCacheDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func hasASRModelData(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if !isASRCacheControlEntry(name) {
			return true
		}
	}
	return false
}

func isASRCacheControlEntry(name string) bool {
	return name == localASRCacheStateName ||
		name == "digcrawl-runner" ||
		strings.HasPrefix(name, ".digcrawl-asr-cache-") ||
		name == ".DS_Store" ||
		name == "Thumbs.db"
}

func readLocalASRCacheState(canonical string) (localASRCacheState, bool) {
	data, err := os.ReadFile(filepath.Join(canonical, localASRCacheStateName))
	if err != nil {
		return localASRCacheState{}, false
	}
	var state localASRCacheState
	if json.Unmarshal(data, &state) != nil || strings.TrimSpace(state.ActiveModelCacheDir) == "" {
		return localASRCacheState{}, false
	}
	state.ActiveModelCacheDir = filepath.Clean(state.ActiveModelCacheDir)
	state.HubCacheDir = strings.TrimSpace(state.HubCacheDir)
	if state.HubCacheDir != "" {
		state.HubCacheDir = filepath.Clean(state.HubCacheDir)
	}
	return state, true
}

func writeLocalASRCacheState(canonical string, state localASRCacheState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(canonical, ".digcrawl-asr-cache-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	statePath := filepath.Join(canonical, localASRCacheStateName)
	if err := os.Rename(temporaryPath, statePath); err != nil {
		if existing, ok := readLocalASRCacheState(canonical); ok && existing == state {
			return nil
		}
		return err
	}
	return nil
}

func envHasValue(env []string, key string) bool {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) && strings.TrimSpace(strings.TrimPrefix(item, prefix)) != "" {
			return true
		}
	}
	return false
}

func localCommandFallback(ctx context.Context, command string, args []string, modelCacheDir string, legacyModelCacheDirs []string, legacyHubCacheDir string) (string, []string, error) {
	if len(args) > 0 {
		return "", nil, nil
	}
	command = strings.ToLower(strings.TrimSpace(command))
	if command != "faster-whisper" && command != "faster_whisper" {
		return "", nil, nil
	}
	runner, err := ensureBundledFasterWhisperRunner(modelCacheDir, legacyModelCacheDirs, legacyHubCacheDir)
	if err != nil {
		return "", nil, fmt.Errorf("prepare bundled faster-whisper runner: %w", err)
	}
	python, err := findPythonInterpreter(ctx, modelCacheDir)
	if err != nil {
		return "", nil, fmt.Errorf("local ASR runtime unavailable; run `digcrawl asr setup`: %w", err)
	}
	return python, []string{
		runner,
		"{audio_path}",
		"--model", "{model}",
		"--output_format", "json",
		"--output_dir", "{output_dir}",
	}, nil
}

func findPythonInterpreter(ctx context.Context, modelCacheDir string) (string, error) {
	runtimeDir := localASRRuntimeDir(modelCacheDir)
	if runtime := localASRRuntimePython(runtimeDir); runtime != "" {
		if err := localASRImportCheck(ctx, runtime); err != nil {
			return "", fmt.Errorf("prepared local ASR runtime %q failed its health check; rerun `digcrawl asr setup`: %w", runtime, err)
		}
		return runtime, nil
	}
	// Match setup's supported-version selection rather than choosing an arbitrary
	// python3 first. This keeps the fallback from selecting an interpreter that
	// cannot install CTranslate2 wheels when a supported one is also available.
	python, err := findBootstrapPython(ctx, "")
	if err != nil {
		return "", err
	}
	if err := localASRImportCheck(ctx, python); err != nil {
		return "", fmt.Errorf("local ASR runtime is not installed or did not pass its health check; run `digcrawl asr setup`: %w", err)
	}
	return python, nil
}

func ensureBundledFasterWhisperRunner(modelCacheDir string, legacyModelCacheDirs []string, legacyHubCacheDir string) (string, error) {
	cache, err := resolveLocalASRCacheLocation(modelCacheDir, legacyModelCacheDirs, legacyHubCacheDir)
	if err != nil {
		return "", err
	}
	runnerDir := filepath.Join(cache.modelCacheDir, "digcrawl-runner")
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bundledFasterWhisperRunner)
	runnerPath := filepath.Join(runnerDir, "faster_whisper_asr-"+hex.EncodeToString(sum[:8])+".py")
	if info, err := os.Stat(runnerPath); err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("bundled ASR runner path is a directory: %s", runnerPath)
		}
		return runnerPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	temporary, err := os.CreateTemp(runnerDir, ".faster-whisper-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(bundledFasterWhisperRunner); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, runnerPath); err != nil {
		// Concurrent MCP/doctor starts can race only on this final publish. A
		// complete runner published by another process is equally valid.
		if info, statErr := os.Stat(runnerPath); statErr == nil && !info.IsDir() {
			return runnerPath, nil
		}
		return "", err
	}
	return runnerPath, nil
}

func expandArgs(args []string, values map[string]string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		for key, val := range values {
			arg = strings.ReplaceAll(arg, "{"+key+"}", val)
		}
		if strings.TrimSpace(arg) != "" {
			out = append(out, arg)
		}
	}
	return out
}

func readTranscriptFromDir(dir string) (*Result, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("local ASR produced no JSON transcript")
	}
	sort.Strings(matches)
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	return parseTranscriptJSON(data)
}

func parseTranscriptJSON(data []byte) (*Result, error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return &Result{}, fmt.Errorf("empty transcript JSON")
	}
	var raw struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return &Result{}, err
	}
	result := &Result{
		Text:     strings.TrimSpace(raw.Text),
		Language: raw.Language,
		Duration: raw.Duration,
	}
	for _, seg := range raw.Segments {
		result.Segments = append(result.Segments, Segment{
			Start: seg.Start,
			End:   seg.End,
			Text:  strings.TrimSpace(seg.Text),
		})
	}
	return result, nil
}
