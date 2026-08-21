package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AudioExtractOptions controls ffmpeg audio extraction.
type AudioExtractOptions struct {
	FFmpegPath string
	TempDir    string
	SampleRate int
}

// ExtractedAudio identifies a temporary audio file created from a media file.
type ExtractedAudio struct {
	Path       string `json:"path"`
	SampleRate int    `json:"sample_rate"`
	Format     string `json:"format"`
}

// Cleanup removes the extracted audio file and its temporary directory when possible.
func (a ExtractedAudio) Cleanup() {
	if a.Path == "" {
		return
	}
	dir := filepath.Dir(a.Path)
	_ = os.Remove(a.Path)
	_ = os.Remove(dir)
}

// ExtractAudio uses ffmpeg to extract mono wav audio from a local media file.
func ExtractAudio(ctx context.Context, inputPath string, opts AudioExtractOptions) (ExtractedAudio, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return ExtractedAudio{}, fmt.Errorf("input media path is required")
	}
	if _, err := os.Stat(inputPath); err != nil {
		return ExtractedAudio{}, fmt.Errorf("stat media file: %w", err)
	}
	ffmpeg := strings.TrimSpace(opts.FFmpegPath)
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	sampleRate := opts.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	baseTemp := strings.TrimSpace(opts.TempDir)
	if baseTemp == "" {
		baseTemp = os.TempDir()
	}
	if err := os.MkdirAll(baseTemp, 0o755); err != nil {
		return ExtractedAudio{}, fmt.Errorf("create ASR temp base directory: %w", err)
	}
	dir, err := os.MkdirTemp(baseTemp, "digcrawl-asr-*")
	if err != nil {
		return ExtractedAudio{}, fmt.Errorf("create temp dir: %w", err)
	}
	outputPath := filepath.Join(dir, "audio.wav")
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-f", "wav",
		outputPath,
	}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return ExtractedAudio{}, fmt.Errorf("ffmpeg extract audio: %s", msg)
	}
	return ExtractedAudio{Path: outputPath, SampleRate: sampleRate, Format: "wav"}, nil
}

// ProbeDuration returns the container duration reported by ffprobe. Callers
// can safely fall back to their static budget when probing is unavailable.
func ProbeDuration(ctx context.Context, inputPath, ffmpegPath string) (time.Duration, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return 0, fmt.Errorf("input media path is required")
	}
	if _, err := os.Stat(inputPath); err != nil {
		return 0, fmt.Errorf("stat media file: %w", err)
	}
	probe := ffprobePath(ffmpegPath)
	output, err := exec.CommandContext(ctx, probe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nokey=1:noprint_wrappers=1",
		inputPath,
	).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return 0, fmt.Errorf("ffprobe media duration: %s", message)
	}
	return parseProbeDuration(string(output))
}

func ffprobePath(ffmpegPath string) string {
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath == "" || ffmpegPath == "ffmpeg" || ffmpegPath == "ffmpeg.exe" {
		return "ffprobe"
	}
	base := strings.ToLower(filepath.Base(ffmpegPath))
	if base == "ffmpeg" || base == "ffmpeg.exe" {
		name := "ffprobe"
		if strings.HasSuffix(base, ".exe") {
			name += ".exe"
		}
		return filepath.Join(filepath.Dir(ffmpegPath), name)
	}
	return "ffprobe"
}

func parseProbeDuration(output string) (time.Duration, error) {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("invalid ffprobe duration %q", strings.TrimSpace(output))
	}
	duration := time.Duration(seconds * float64(time.Second))
	if duration <= 0 {
		return 0, fmt.Errorf("invalid ffprobe duration %q", strings.TrimSpace(output))
	}
	return duration, nil
}
