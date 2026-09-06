// Package video analyzes one local video file or a directly downloadable video URL.
package video

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/internal/media"
	"github.com/originaleric/digcrawl/internal/ocr"
)

const (
	DefaultMaxBytes      int64         = 512 * 1024 * 1024
	durationProbeTimeout time.Duration = 10 * time.Second
)

// Options controls one video analysis operation.
type Options struct {
	// Mode is auto, asr, ocr, or both. Auto runs each processor explicitly
	// enabled in the supplied configuration.
	Mode string
	// MaxBytes bounds a URL download. It does not apply to a local input file.
	MaxBytes int64
	// DownloadTimeout bounds URL acquisition. Zero uses five minutes.
	DownloadTimeout time.Duration
	// KeepPath persists a successfully downloaded URL input at this exact path.
	// The destination must not already exist. Local inputs are never copied.
	KeepPath string
}

// Source describes the analyzed video without exposing temporary URL downloads.
type Source struct {
	Input           string  `json:"input"`
	Kind            string  `json:"kind"`
	Path            string  `json:"path,omitempty"`
	Bytes           int64   `json:"bytes"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// ASRStage holds the transcription result for an analysis run.
type ASRStage struct {
	Status string      `json:"status"`
	Result *asr.Result `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// OCRStage holds the video-frame OCR result for an analysis run.
type OCRStage struct {
	Status  string       `json:"status"`
	Results []ocr.Result `json:"results,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// Result is a stable, machine-readable result for a single video.
type Result struct {
	Source   Source   `json:"source"`
	ASR      ASRStage `json:"asr"`
	OCR      OCRStage `json:"ocr"`
	Text     string   `json:"text,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Analyze acquires one video, runs the requested existing ASR and frame-OCR
// pipelines, and cleans up every temporary URL download before returning.
// A partially successful result is returned together with a non-nil error only
// when every requested processor fails.
func Analyze(ctx context.Context, input string, cfg *config.Config, opts Options) (*Result, error) {
	if cfg == nil {
		return nil, fmt.Errorf("video analysis config is required")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("video input is required")
	}
	runASR, runOCR, err := processorsForMode(opts.Mode, cfg)
	if err != nil {
		return nil, err
	}
	keepPath := strings.TrimSpace(opts.KeepPath)
	remoteInput := isHTTPURL(input)
	if keepPath != "" {
		if !remoteInput {
			return nil, fmt.Errorf("--keep is only valid for a video URL; local input is already retained")
		}
		if err := validateKeepDestination(keepPath); err != nil {
			return nil, fmt.Errorf("validate keep destination: %w", err)
		}
	}

	path, kind, cleanup, err := acquire(ctx, input, opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat video input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("video input must be a regular file")
	}
	duration, durationWarning := probeVideoDuration(ctx, path, cfg, runASR, runOCR)
	if kind == "url" && keepPath != "" {
		if err := copyFileNew(path, keepPath); err != nil {
			return nil, fmt.Errorf("keep downloaded video: %w", err)
		}
	}

	result := &Result{
		Source: Source{Input: input, Kind: kind, Bytes: info.Size(), DurationSeconds: duration.Seconds()},
		ASR:    ASRStage{Status: "skipped"},
		OCR:    OCRStage{Status: "skipped"},
	}
	if durationWarning != "" {
		result.Warnings = append(result.Warnings, durationWarning)
	}
	if kind == "local" {
		result.Source.Path = path
	}
	if runASR {
		result.ASR = runTranscription(ctx, path, input, cfg.ASR)
	}
	if runOCR {
		result.OCR = runFrameOCR(ctx, path, cfg.OCR)
	}
	result.Text = combinedText(result)
	result.Warnings = append(result.Warnings, stageWarnings(result)...)
	if !hasSuccessfulProcessor(result, runASR, runOCR) {
		return result, fmt.Errorf("video analysis failed: %s", failureSummary(result, runASR, runOCR))
	}
	return result, nil
}

// probeVideoDuration enriches output when ffprobe is present, but duration is
// metadata rather than a prerequisite for either ASR or frame OCR. ASR itself
// already falls back to its static timeout when its probe is unavailable.
func probeVideoDuration(ctx context.Context, path string, cfg *config.Config, runASR, runOCR bool) (time.Duration, string) {
	probeCtx, cancel := context.WithTimeout(ctx, durationProbeTimeout)
	defer cancel()
	duration, err := media.ProbeDuration(probeCtx, path, durationFFmpegPath(cfg, runASR, runOCR))
	if err != nil {
		return 0, "video duration unavailable: " + err.Error()
	}
	return duration, ""
}

// validateKeepDestination preflights deterministic local failures before a
// potentially large URL download. The final copy still uses O_EXCL so a target
// created after this check cannot be overwritten.
func validateKeepDestination(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("destination %q already exists", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination %q: %w", path, err)
	}
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("inspect parent directory %q: %w", parent, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("keep parent %q is not a directory", parent)
	}
	return nil
}

func durationFFmpegPath(cfg *config.Config, runASR, runOCR bool) string {
	if !runASR && runOCR {
		return cfg.OCR.VideoFrames.FFmpegPathOrDefault()
	}
	return cfg.ASR.FFmpegPathOrDefault()
}

func processorsForMode(raw string, cfg *config.Config) (bool, bool, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "auto":
		asrEnabled := cfg.ASR.EnabledOrDefault()
		ocrEnabled := cfg.OCR.EnabledOrDefault() && cfg.OCR.VideoFrames.EnabledOrDefault()
		if !asrEnabled && !ocrEnabled {
			return false, false, fmt.Errorf("video analysis has no enabled processor: enable ASR.Enabled and/or OCR.Enabled with OCR.VideoFrames.Enabled")
		}
		return asrEnabled, ocrEnabled, nil
	case "asr":
		if !cfg.ASR.EnabledOrDefault() {
			return false, false, fmt.Errorf("ASR.Enabled must be true for --mode asr")
		}
		return true, false, nil
	case "ocr":
		if !cfg.OCR.EnabledOrDefault() || !cfg.OCR.VideoFrames.EnabledOrDefault() {
			return false, false, fmt.Errorf("OCR.Enabled and OCR.VideoFrames.Enabled must be true for --mode ocr")
		}
		return false, true, nil
	case "both":
		if !cfg.ASR.EnabledOrDefault() {
			return false, false, fmt.Errorf("ASR.Enabled must be true for --mode both")
		}
		if !cfg.OCR.EnabledOrDefault() || !cfg.OCR.VideoFrames.EnabledOrDefault() {
			return false, false, fmt.Errorf("OCR.Enabled and OCR.VideoFrames.Enabled must be true for --mode both")
		}
		return true, true, nil
	default:
		return false, false, fmt.Errorf("unsupported video mode %q (want auto, asr, ocr, or both)", raw)
	}
}

func runTranscription(ctx context.Context, path, input string, cfg config.ASRConfig) ASRStage {
	transcript, err := asr.Transcribe(ctx, path, cfg, asr.Options{})
	if err != nil {
		return ASRStage{Status: "error", Error: err.Error()}
	}
	// URL downloads are ephemeral. Report the user-provided source instead of a
	// stale temporary pathname in the reusable transcript payload.
	transcript.Audio.SourcePath = input
	return ASRStage{Status: "ok", Result: transcript}
}

func runFrameOCR(ctx context.Context, path string, cfg config.OCRConfig) OCRStage {
	results, err := ocr.RecognizeLocalVideoFrames(ctx, cfg, path)
	if err != nil {
		return OCRStage{Status: "error", Error: err.Error()}
	}
	for _, result := range results {
		if strings.TrimSpace(result.Text) != "" {
			return OCRStage{Status: "ok", Results: results}
		}
	}
	if len(results) == 0 {
		return OCRStage{Status: "error", Error: "frame OCR produced no results"}
	}
	var failures []string
	for _, result := range results {
		if result.Error != "" {
			failures = append(failures, result.Error)
		}
	}
	if len(failures) > 0 {
		return OCRStage{Status: "error", Results: results, Error: strings.Join(failures, "; ")}
	}
	return OCRStage{Status: "ok", Results: results}
}

func combinedText(result *Result) string {
	var sections []string
	if result != nil && result.ASR.Result != nil && strings.TrimSpace(result.ASR.Result.Text) != "" {
		sections = append(sections, "## ASR\n\n"+strings.TrimSpace(result.ASR.Result.Text))
	}
	if result != nil {
		var ocrText []string
		for _, item := range result.OCR.Results {
			if text := strings.TrimSpace(item.Text); text != "" {
				ocrText = append(ocrText, text)
			}
		}
		if len(ocrText) > 0 {
			sections = append(sections, "## OCR\n\n"+strings.Join(ocrText, "\n\n"))
		}
	}
	return strings.Join(sections, "\n\n")
}

func stageWarnings(result *Result) []string {
	if result == nil {
		return nil
	}
	var warnings []string
	if result.ASR.Status == "error" && result.ASR.Error != "" {
		warnings = append(warnings, "ASR: "+result.ASR.Error)
	}
	if result.OCR.Status == "error" && result.OCR.Error != "" {
		warnings = append(warnings, "OCR: "+result.OCR.Error)
	}
	return warnings
}

func hasSuccessfulProcessor(result *Result, runASR, runOCR bool) bool {
	return result != nil && ((runASR && result.ASR.Status == "ok") || (runOCR && result.OCR.Status == "ok"))
}

func failureSummary(result *Result, runASR, runOCR bool) string {
	var failures []string
	if runASR {
		failures = append(failures, "ASR: "+firstNonEmpty(result.ASR.Error, "no transcript"))
	}
	if runOCR {
		failures = append(failures, "OCR: "+firstNonEmpty(result.OCR.Error, "no recognized text"))
	}
	return strings.Join(failures, "; ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func acquire(ctx context.Context, input string, opts Options) (string, string, func(), error) {
	if isHTTPURL(input) {
		return downloadURL(ctx, input, opts)
	}
	path, err := filepath.Abs(input)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve local video path: %w", err)
	}
	return path, "local", func() {}, nil
}

func isHTTPURL(input string) bool {
	u, err := url.Parse(input)
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

func downloadURL(ctx context.Context, rawURL string, opts Options) (string, string, func(), error) {
	if err := media.ValidateRemoteURL(rawURL); err != nil {
		return "", "", nil, fmt.Errorf("validate video URL: %w", err)
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	timeout := opts.DownloadTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	dir, err := os.MkdirTemp("", "digcrawl-video-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create video download directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	u, _ := url.Parse(rawURL)
	name := "video" + safeExtension(u.Path)
	dest := filepath.Join(dir, name)
	client := media.NewSafeHTTPClient(timeout)
	if _, err := media.DownloadFile(ctx, client, rawURL, dest, maxBytes, "", "video"); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("download video: %w", err)
	}
	return dest, "url", cleanup, nil
}

func safeExtension(rawPath string) string {
	ext := strings.ToLower(filepath.Ext(rawPath))
	if len(ext) < 2 || len(ext) > 10 {
		return ".mp4"
	}
	for _, char := range ext[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return ".mp4"
		}
	}
	return ext
}

func copyFileNew(source, dest string) error {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return closeErr
	}
	return nil
}
