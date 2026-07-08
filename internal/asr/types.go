package asr

import (
	"context"
	"fmt"
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
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil, fmt.Errorf("ASR APIKey is empty")
		}
		responseFormat := cfg.ResponseFormatOrDefault()
		if !config.SupportedASRResponseFormat(responseFormat) {
			return nil, fmt.Errorf("unsupported ASR response format %q", cfg.ResponseFormat)
		}
		return &apiProvider{
			apiKey:         strings.TrimSpace(cfg.APIKey),
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
			command:  cfg.LocalCommandOrDefault(),
			args:     cfg.LocalArgs,
			model:    cfg.ModelOrDefault(),
			language: strings.TrimSpace(cfg.Language),
			timeout:  time.Duration(cfg.TimeoutSecOrDefault()) * time.Second,
		}, nil
	case "stub":
		return stubProvider{model: cfg.ModelOrDefault()}, nil
	default:
		return nil, fmt.Errorf("unsupported ASR provider %q", cfg.Provider)
	}
}
