package asr

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
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
	p := &localProvider{command: cmd, model: "small", timeout: 5 * time.Second, modelCacheDir: filepath.Join(dir, "models")}
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
	script := "#!/bin/sh\ncase \"$HF_HOME\" in */storage/digcrawl/asr_models) ;; *) echo \"bad HF_HOME=$HF_HOME\" >&2; exit 2;; esac\nprintf '{\"text\":\"local transcript\"}'\n"
	if err := os.WriteFile(cmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	p := &localProvider{command: cmd, model: "small", timeout: 5 * time.Second, modelCacheDir: filepath.Join(dir, "storage", "digcrawl", "asr_models")}
	if _, err := p.Transcribe(context.Background(), audio); err != nil {
		t.Fatal(err)
	}
}

func TestLocalASREnvMakesConfiguredCacheAuthoritative(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "models")
	env, err := localASREnv([]string{
		"HF_HOME=/another/cache",
		"HUGGINGFACE_HUB_CACHE=/another/hub",
	}, cacheDir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if values["HF_HOME"] != cacheDir || values["DIGCRAWL_ASR_MODEL_CACHE_DIR"] != cacheDir {
		t.Fatalf("configured cache was not propagated: %#v", values)
	}
	if _, exists := values["HUGGINGFACE_HUB_CACHE"]; exists {
		t.Fatalf("inherited hub cache must not override ModelCacheDir: %#v", values)
	}
}

func TestResolveLocalASRCacheDirAdoptsLegacyWithoutMovingAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "old-working-directory", ".cache", "huggingface")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(legacy, "model.bin")
	if err := os.WriteFile(model, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "profile", "asr_models")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Desktop metadata must not be mistaken for an already populated model
	// cache, otherwise macOS/Windows users would lose the legacy adoption path.
	if err := os.WriteFile(filepath.Join(target, ".DS_Store"), []byte("metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveLocalASRCacheDir(target, []string{legacy})
	if err != nil {
		t.Fatal(err)
	}
	if got != legacy {
		t.Fatalf("active cache=%q want legacy %q", got, legacy)
	}
	if _, err := os.Stat(filepath.Join(legacy, "model.bin")); err != nil {
		t.Fatalf("legacy model must remain in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, localASRCacheStateName)); err != nil {
		t.Fatalf("compatibility state missing: %v", err)
	}
	// Simulate a later service restart from a different cwd: the persisted state,
	// rather than cwd, must continue selecting the already verified model cache.
	t.Chdir(t.TempDir())
	got, err = resolveLocalASRCacheDir(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != legacy {
		t.Fatalf("restart cache=%q want persisted legacy %q", got, legacy)
	}
}

func TestResolveLocalASRCacheDirDoesNotAdoptForExplicitCache(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".cache", "huggingface")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "model.bin"), []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "explicit-models")
	got, err := resolveLocalASRCacheDir(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("explicit cache=%q want %q", got, target)
	}
}

func TestResolveLocalASRCacheDirConcurrentAdoptionConverges(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".cache", "huggingface")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "model.bin"), []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "profile", "asr_models")

	start := make(chan struct{})
	type result struct {
		path string
		err  error
	}
	results := make(chan result, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			path, err := resolveLocalASRCacheDir(target, []string{legacy})
			results <- result{path: path, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent cache initialization failed: %v", result.err)
		}
		if result.path != legacy {
			t.Fatalf("concurrent active cache=%q want %q", result.path, legacy)
		}
	}
}

func TestResolveLocalASRCacheDirRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveLocalASRCacheDir(path, nil)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("regular cache path error=%v, want directory error", err)
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

func TestCheckAvailableRejectsMissingLocalDependencies(t *testing.T) {
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := CheckAvailable(config.ASRConfig{Provider: "local", FFmpegPath: ffmpeg, LocalCommand: "digcrawl-missing-local-asr-command"})
	if err == nil || !strings.Contains(err.Error(), "local ASR command") {
		t.Fatalf("error=%v", err)
	}
}

func TestCheckAvailableRunsBundledFasterWhisperPreflight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only; Windows command discovery is covered by cross-compilation")
	}
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(dir, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\n[ \"$2\" = \"--check\" ] && [ \"$3\" = \"--check-model\" ] && [ \"$4\" = \"--model\" ] && [ \"$5\" = \"small\" ] || exit 13\nprintf '{\\\"status\\\":\\\"ok\\\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := CheckAvailable(config.ASRConfig{
		Provider:      "local",
		FFmpegPath:    ffmpeg,
		LocalCommand:  "faster-whisper",
		ModelCacheDir: filepath.Join(dir, "models"),
	}); err != nil {
		t.Fatalf("CheckAvailable() error = %v", err)
	}
}

func TestCheckAvailableReportsBundledFasterWhisperPreflightFailure(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(dir, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\necho 'faster_whisper import failed: missing' >&2\nexit 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := CheckAvailable(config.ASRConfig{
		Provider:     "local",
		FFmpegPath:   ffmpeg,
		LocalCommand: python,
		LocalArgs:    []string{filepath.Join(dir, "faster_whisper_asr.py")},
	})
	if err == nil || !strings.Contains(err.Error(), "faster_whisper import failed: missing") {
		t.Fatalf("CheckAvailable() error = %v", err)
	}
}

func TestLocalCommandFallbackUsesEmbeddedRunnerOutsideWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	binDir := t.TempDir()
	pythonName := "python"
	if runtime.GOOS == "windows" {
		pythonName = "python.exe"
	}
	python := filepath.Join(binDir, pythonName)
	if err := os.WriteFile(python, []byte("placeholder"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	cacheDir := filepath.Join(t.TempDir(), "models")

	command, args, err := localCommandFallback("faster-whisper", nil, cacheDir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if command != python {
		t.Fatalf("fallback command=%q want %q", command, python)
	}
	if len(args) == 0 || !filepath.IsAbs(args[0]) {
		t.Fatalf("fallback runner must be absolute, args=%q", args)
	}
	if !strings.HasPrefix(args[0], cacheDir+string(filepath.Separator)) {
		t.Fatalf("fallback runner=%q is not profile-owned by cache %q", args[0], cacheDir)
	}
	if _, err := os.Stat(args[0]); err != nil {
		t.Fatalf("embedded runner was not materialized: %v", err)
	}
}

func TestEnsureBundledFasterWhisperRunnerConcurrentPublishConverges(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "models")
	start := make(chan struct{})
	paths := make(chan string, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			path, err := ensureBundledFasterWhisperRunner(cacheDir, nil, "")
			if err != nil {
				errs <- err
				return
			}
			paths <- path
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(paths)
	for err := range errs {
		t.Fatalf("concurrent runner publish failed: %v", err)
	}
	if len(bundledFasterWhisperRunner) == 0 {
		t.Fatal("bundled runner is empty")
	}
	wantDir := filepath.Join(cacheDir, "digcrawl-runner")
	for path := range paths {
		if filepath.Dir(path) != wantDir || !strings.HasPrefix(filepath.Base(path), "faster_whisper_asr-") {
			t.Fatalf("runner path=%q is not a versioned cache runner", path)
		}
	}
	entries, err := os.ReadDir(wantDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("published runner missing or duplicated: entries=%v err=%v", entries, err)
	}
}

func TestEnsureBundledFasterWhisperRunnerDoesNotReuseLegacyUnversionedFile(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "models")
	legacyRunnerDir := filepath.Join(cacheDir, "digcrawl-runner")
	if err := os.MkdirAll(legacyRunnerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyRunner := filepath.Join(legacyRunnerDir, "faster_whisper_asr.py")
	if err := os.WriteFile(legacyRunner, []byte("stale"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner, err := ensureBundledFasterWhisperRunner(cacheDir, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if runner == legacyRunner || filepath.Base(runner) == filepath.Base(legacyRunner) {
		t.Fatalf("versioned runner reused stale path: %q", runner)
	}
	data, err := os.ReadFile(runner)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(bundledFasterWhisperRunner) {
		t.Fatal("versioned runner content differs from embedded asset")
	}
	if _, err := os.Stat(legacyRunner); err != nil {
		t.Fatalf("published runner missing: %v", err)
	}
}

func TestBundledFasterWhisperRunnerMatchesStandaloneScript(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate ASR test source")
	}
	standalonePath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "scripts", "faster_whisper_asr.py")
	standalone, err := os.ReadFile(standalonePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(standalone, bundledFasterWhisperRunner) {
		t.Fatalf("embedded runner and standalone script diverged: %s", standalonePath)
	}
}

func TestUsesBundledFasterWhisperRunnerRecognizesVersionedAndLegacyNames(t *testing.T) {
	for _, arg := range []string{
		"/tmp/faster_whisper_asr.py",
		"/tmp/faster_whisper_asr-deadbeef.py",
	} {
		if !usesBundledFasterWhisperRunner([]string{arg}) {
			t.Fatalf("runner %q must receive the preflight", arg)
		}
	}
	if usesBundledFasterWhisperRunner([]string{"/tmp/other_runner.py"}) {
		t.Fatal("unrelated custom runner must not be classified as faster-whisper")
	}
}

func TestResolveLocalASRCacheLocationPreservesCustomLegacyHubCache(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "profile", "asr_models")
	legacyHub := filepath.Join(root, "custom-hub-cache")
	if err := os.MkdirAll(legacyHub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyHub, "models--small"), []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, err := resolveLocalASRCacheLocation(target, nil, legacyHub)
	if err != nil {
		t.Fatal(err)
	}
	if cache.modelCacheDir != target || cache.hubCacheDir != legacyHub {
		t.Fatalf("cache location=%+v want target=%q legacyHub=%q", cache, target, legacyHub)
	}
	cache, err = resolveLocalASRCacheLocation(target, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if cache.modelCacheDir != target || cache.hubCacheDir != legacyHub {
		t.Fatalf("persisted cache location=%+v want target=%q legacyHub=%q", cache, target, legacyHub)
	}
	env, err := localASREnv(nil, target, nil, legacyHub)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		values[key] = value
	}
	if values["HF_HOME"] != target || values["HUGGINGFACE_HUB_CACHE"] != legacyHub || values["DIGCRAWL_ASR_LEGACY_HUB_CACHE_DIR"] != legacyHub {
		t.Fatalf("custom legacy hub cache was not preserved: %#v", values)
	}
}
