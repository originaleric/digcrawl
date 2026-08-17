package asr

import (
	"context"
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

// CheckAvailable verifies the local prerequisites that Transcribe will need
// before a caller persists an ASR opt-in. The bundled faster-whisper runner is
// checked against the configured cache in offline mode, so this never downloads
// a model but does prove the selected model can be loaded.
func CheckAvailable(cfg config.ASRConfig) error {
	ffmpeg := cfg.FFmpegPathOrDefault()
	if _, err := exec.LookPath(ffmpeg); err != nil {
		return fmt.Errorf("ffmpeg %q is not available: %w", ffmpeg, err)
	}
	p, err := newProvider(cfg)
	if err != nil {
		return err
	}
	local, ok := p.(*localProvider)
	if !ok {
		return nil
	}
	if _, err := exec.LookPath(local.command); err != nil {
		fallbackCommand, fallbackArgs, fallbackErr := localCommandFallback(local.command, local.args, local.modelCacheDir, local.legacyModelCacheDirs, local.legacyHubCacheDir)
		if fallbackErr != nil {
			return fallbackErr
		}
		if fallbackCommand == "" {
			return fmt.Errorf("local ASR command %q is not available", local.command)
		}
		local.command, local.args = fallbackCommand, fallbackArgs
	}
	if !usesBundledFasterWhisperRunner(local.args) {
		return nil
	}
	// LocalArgs is a runtime template and commonly contains {audio_path},
	// {output_dir}, and {model}. A readiness probe has no audio/output and must
	// use the already-resolved config value rather than passing those placeholders
	// through to argparse.
	checkArgs := []string{local.args[0], "--check", "--check-model", "--model", local.model}
	cmd := exec.Command(local.command, checkArgs...)
	env, envErr := localASREnv(os.Environ(), local.modelCacheDir, local.legacyModelCacheDirs, local.legacyHubCacheDir)
	if envErr != nil {
		return envErr
	}
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("local faster-whisper runtime check failed: %s", message)
	}
	return nil
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

	timeout := time.Duration(cfg.TimeoutSecOrDefault()) * time.Second
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

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
