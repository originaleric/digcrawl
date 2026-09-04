package asr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/media"
)

// Segment stores one timestamped transcript segment.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Result stores transcription output.
type Result struct {
	Text       string    `json:"text"`
	Segments   []Segment `json:"segments,omitempty"`
	Language   string    `json:"language,omitempty"`
	Duration   float64   `json:"duration,omitempty"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model,omitempty"`
	Audio      AudioInfo `json:"audio"`
	CapturedAt string    `json:"captured_at"`
}

// AudioInfo describes extracted audio used for transcription.
type AudioInfo struct {
	SourcePath string `json:"source_path"`
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
}

// Options overrides ASR config for a single call.
type Options struct {
	Provider       string
	Model          string
	Language       string
	Prompt         string
	ResponseFormat string
}

type provider interface {
	Transcribe(ctx context.Context, audioPath string) (*Result, error)
	Name() string
	Model() string
}

const (
	localASRDurationMultiplier = 2
	localASRDurationSafety     = 20 * time.Second
	localASRDurationProbeLimit = 10 * time.Second
)

// Readiness reports what a non-transcribing ASR preflight was able to verify.
// A custom local command owns its own script, runtime, and model lifecycle, so
// it can be checked for command availability but not truthfully reported as a
// verified offline faster-whisper installation.
type Readiness struct {
	OfflineModelVerified bool
}

// CheckAvailable preserves the compatibility contract for callers that need
// prerequisite errors but do not need to distinguish a managed runner from a
// custom integration.
func CheckAvailable(cfg config.ASRConfig) error {
	_, err := CheckReadiness(cfg)
	return err
}

// CheckReadiness verifies the prerequisites that Transcribe will need before
// a caller persists an ASR opt-in. The bundled faster-whisper runner is
// checked against the configured cache in offline mode, so this never downloads
// a model but does prove the selected model can be loaded.
func CheckReadiness(cfg config.ASRConfig) (Readiness, error) {
	ffmpeg := cfg.FFmpegPathOrDefault()
	if _, err := exec.LookPath(ffmpeg); err != nil {
		return Readiness{}, fmt.Errorf("ffmpeg %q is not available: %w", ffmpeg, err)
	}
	p, err := newProvider(cfg)
	if err != nil {
		return Readiness{}, err
	}
	local, ok := p.(*localProvider)
	if !ok {
		return Readiness{}, nil
	}
	// Keep readiness behavior aligned with Transcribe: an explicitly prepared
	// DigCrawl runtime is authoritative over an ambient PATH executable.
	if localASRRuntimePython(localASRRuntimeDir(local.modelCacheDir)) != "" {
		fallbackCommand, fallbackArgs, fallbackErr := localCommandFallback(context.Background(), local.command, local.args, local.modelCacheDir, local.legacyModelCacheDirs, local.legacyHubCacheDir)
		if fallbackErr != nil {
			return Readiness{}, fallbackErr
		}
		if fallbackCommand != "" {
			local.command, local.args = fallbackCommand, fallbackArgs
		}
	}
	if _, err := exec.LookPath(local.command); err != nil {
		fallbackCommand, fallbackArgs, fallbackErr := localCommandFallback(context.Background(), local.command, local.args, local.modelCacheDir, local.legacyModelCacheDirs, local.legacyHubCacheDir)
		if fallbackErr != nil {
			return Readiness{}, fallbackErr
		}
		if fallbackCommand == "" {
			return Readiness{}, fmt.Errorf("local ASR command %q is not available", local.command)
		}
		local.command, local.args = fallbackCommand, fallbackArgs
	}
	if !usesBundledFasterWhisperRunner(local.args) {
		return Readiness{}, nil
	}
	// LocalArgs is a runtime template and commonly contains {audio_path},
	// {output_dir}, and {model}. A readiness probe has no audio/output and must
	// use the already-resolved config value rather than passing those placeholders
	// through to argparse.
	checkArgs := []string{local.args[0], "--check", "--check-model", "--model", local.model}
	checkCtx, cancel := localASRRuntimeHealthCheckContext(context.Background())
	defer cancel()
	cmd := exec.CommandContext(checkCtx, local.command, checkArgs...)
	env, envErr := localASREnv(os.Environ(), local.modelCacheDir, local.legacyModelCacheDirs, local.legacyHubCacheDir)
	if envErr != nil {
		return Readiness{}, envErr
	}
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
			return Readiness{}, fmt.Errorf("local faster-whisper runtime check timed out after %s", localASRRuntimeHealthCheckTimeout)
		}
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return Readiness{}, fmt.Errorf("local faster-whisper runtime check failed: %s", message)
	}
	return Readiness{OfflineModelVerified: true}, nil
}

func usesBundledFasterWhisperRunner(args []string) bool {
	if len(args) == 0 {
		return false
	}
	name := filepath.Base(args[0])
	// Keep the historical standalone-script name compatible while recognizing
	// versioned embedded runners. The versioned form is required so an upgraded
	// DigCrawl binary cannot silently execute a stale cached runner.
	return name == "faster_whisper_asr.py" ||
		(strings.HasPrefix(name, "faster_whisper_asr-") && strings.HasSuffix(name, ".py"))
}

// Transcribe extracts audio with ffmpeg and transcribes it with the configured ASR provider.
func Transcribe(ctx context.Context, mediaPath string, cfg config.ASRConfig, opts Options) (*Result, error) {
	if strings.TrimSpace(opts.Provider) != "" {
		cfg.Provider = opts.Provider
	}
	if strings.TrimSpace(opts.Model) != "" {
		cfg.Model = opts.Model
	}
	if strings.TrimSpace(opts.Language) != "" {
		cfg.Language = opts.Language
	}
	if strings.TrimSpace(opts.Prompt) != "" {
		cfg.Prompt = opts.Prompt
	}
	if strings.TrimSpace(opts.ResponseFormat) != "" {
		cfg.ResponseFormat = opts.ResponseFormat
	}

	operationStarted := time.Now()
	configuredTimeout := time.Duration(cfg.TimeoutSecOrDefault()) * time.Second
	maxTimeout := time.Duration(cfg.MaxTimeoutSecOrDefault()) * time.Second
	if configuredTimeout > maxTimeout {
		return nil, fmt.Errorf("ASR.TimeoutSec (%s) exceeds ASR.MaxTimeoutSec (%s)", configuredTimeout, maxTimeout)
	}
	maxCtx, cancelMax := context.WithDeadline(ctx, operationStarted.Add(maxTimeout))
	defer cancelMax()

	// Duration is not known yet, so probing may use only the configured floor.
	// A fast successful probe can raise the operation deadline afterwards; a
	// slow or unavailable probe must fall back without already exceeding that
	// floor.
	probeBudgetCtx, cancelProbeBudget := context.WithDeadline(maxCtx, operationStarted.Add(configuredTimeout))
	timeout := effectiveASRTimeout(probeBudgetCtx, mediaPath, cfg)
	cancelProbeBudget()
	if timeout > maxTimeout {
		return nil, fmt.Errorf("local ASR requires %s for this media, exceeding ASR.MaxTimeoutSec (%s); increase ASR.MaxTimeoutSec", timeout, maxTimeout)
	}
	deadline := operationStarted.Add(timeout)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		return nil, fmt.Errorf("caller deadline (%s) is shorter than the required ASR budget (%s)", time.Until(callerDeadline).Round(time.Second), timeout)
	}
	// Probe, extraction, and inference consume one operation budget. The
	// provider receives the same timeout snapshot, while this deadline remains
	// the source of truth after either earlier phase has consumed time.
	cfg.TimeoutSec = int((timeout + time.Second - 1) / time.Second)
	ctx, cancel := context.WithDeadline(maxCtx, deadline)
	defer cancel()

	audio, err := media.ExtractAudio(ctx, mediaPath, media.AudioExtractOptions{
		FFmpegPath: cfg.FFmpegPathOrDefault(),
		TempDir:    cfg.TempDir,
		SampleRate: cfg.AudioSampleRateOrDefault(),
	})
	if err != nil {
		return nil, err
	}
	defer audio.Cleanup()

	p, err := newProvider(cfg)
	if err != nil {
		return nil, err
	}
	result, err := p.Transcribe(ctx, audio.Path)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("ASR provider returned nil result")
	}
	result.Provider = p.Name()
	result.Model = p.Model()
	result.CapturedAt = time.Now().UTC().Format(time.RFC3339)
	result.Audio.SourcePath = mediaPath
	result.Audio.Format = audio.Format
	result.Audio.SampleRate = audio.SampleRate
	return result, nil
}

func effectiveASRTimeout(ctx context.Context, mediaPath string, cfg config.ASRConfig) time.Duration {
	configured := time.Duration(cfg.TimeoutSecOrDefault()) * time.Second
	if cfg.ProviderOrDefault() != "local" {
		return configured
	}
	probeCtx, cancel := context.WithTimeout(ctx, localASRDurationProbeLimit)
	defer cancel()
	duration, err := media.ProbeDuration(probeCtx, mediaPath, cfg.FFmpegPathOrDefault())
	if err != nil {
		return configured
	}
	return localASRTimeoutForDuration(configured, duration)
}

func localASRTimeoutForDuration(configured, duration time.Duration) time.Duration {
	dynamic := time.Duration(localASRDurationMultiplier)*duration + localASRDurationSafety
	if dynamic > configured {
		return dynamic
	}
	return configured
}

func newProvider(cfg config.ASRConfig) (provider, error) {
	switch cfg.ProviderOrDefault() {
	case "api":
		apiKey := strings.TrimSpace(os.ExpandEnv(cfg.APIKey))
		if apiKey == "" {
			return nil, fmt.Errorf("ASR APIKey is empty")
		}
		responseFormat := cfg.ResponseFormatOrDefault()
		if !config.SupportedASRResponseFormat(responseFormat) {
			return nil, fmt.Errorf("unsupported ASR response format %q", cfg.ResponseFormat)
		}
		return &apiProvider{
			apiKey:         apiKey,
			baseURL:        cfg.BaseURLOrDefault(),
			model:          cfg.ModelOrDefault(),
			language:       strings.TrimSpace(cfg.Language),
			prompt:         strings.TrimSpace(cfg.Prompt),
			responseFormat: responseFormat,
			timeout:        time.Duration(cfg.TimeoutSecOrDefault()) * time.Second,
			maxAudioBytes:  cfg.MaxAudioBytesOrDefault(),
		}, nil
	case "local":
		return &localProvider{
			command:              cfg.LocalCommandOrDefault(),
			args:                 cfg.LocalArgs,
			model:                cfg.ModelOrDefault(),
			language:             strings.TrimSpace(cfg.Language),
			timeout:              time.Duration(cfg.TimeoutSecOrDefault()) * time.Second,
			modelCacheDir:        cfg.ModelCacheDirOrDefault(),
			legacyModelCacheDirs: append([]string(nil), cfg.LegacyModelCacheDirs...),
			legacyHubCacheDir:    cfg.LegacyHubCacheDir,
		}, nil
	case "stub":
		return stubProvider{model: cfg.ModelOrDefault()}, nil
	default:
		return nil, fmt.Errorf("unsupported ASR provider %q", cfg.Provider)
	}
}
