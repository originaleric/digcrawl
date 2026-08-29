package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// VideoFrameExtractOptions bounds local frame extraction for OCR.
type VideoFrameExtractOptions struct {
	FFmpegPath  string
	TempDir     string
	IntervalSec int
	MaxFrames   int
}

// ExtractedVideoFrames contains temporary JPEG frames. Call Cleanup after the
// caller has consumed them; the directory is created with private permissions.
type ExtractedVideoFrames struct {
	Dir   string
	Paths []string
}

// Cleanup removes all temporary video frames.
func (f ExtractedVideoFrames) Cleanup() {
	if f.Dir != "" {
		_ = os.RemoveAll(f.Dir)
	}
}

// ExtractVideoFrames samples the first video stream at a fixed interval. It
// deliberately does not decode audio or retain frames after the caller finishes.
func ExtractVideoFrames(ctx context.Context, inputPath string, opts VideoFrameExtractOptions) (ExtractedVideoFrames, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return ExtractedVideoFrames{}, fmt.Errorf("input media path is required")
	}
	if _, err := os.Stat(inputPath); err != nil {
		return ExtractedVideoFrames{}, fmt.Errorf("stat media file: %w", err)
	}
	if opts.IntervalSec <= 0 {
		return ExtractedVideoFrames{}, fmt.Errorf("frame interval must be greater than zero")
	}
	if opts.MaxFrames <= 0 {
		return ExtractedVideoFrames{}, fmt.Errorf("maximum frame count must be greater than zero")
	}
	ffmpeg := strings.TrimSpace(opts.FFmpegPath)
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	baseTemp := strings.TrimSpace(opts.TempDir)
	if baseTemp == "" {
		baseTemp = os.TempDir()
	}
	if err := os.MkdirAll(baseTemp, 0o755); err != nil {
		return ExtractedVideoFrames{}, fmt.Errorf("create OCR frame temp base directory: %w", err)
	}
	dir, err := os.MkdirTemp(baseTemp, "digcrawl-ocr-frames-*")
	if err != nil {
		return ExtractedVideoFrames{}, fmt.Errorf("create OCR frame temp directory: %w", err)
	}
	result := ExtractedVideoFrames{Dir: dir}
	pattern := filepath.Join(dir, "frame-%06d.jpg")
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", inputPath,
		"-map", "0:v:0?",
		"-an",
		"-vf", fmt.Sprintf("fps=1/%d", opts.IntervalSec),
		"-frames:v", fmt.Sprintf("%d", opts.MaxFrames),
		"-q:v", "2",
		pattern,
	}
	if out, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		result.Cleanup()
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return ExtractedVideoFrames{}, fmt.Errorf("ffmpeg extract video frames: %s", message)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "frame-*.jpg"))
	if err != nil {
		result.Cleanup()
		return ExtractedVideoFrames{}, fmt.Errorf("list extracted video frames: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		result.Cleanup()
		return ExtractedVideoFrames{}, fmt.Errorf("ffmpeg extracted no video frames")
	}
	result.Paths = paths
	return result, nil
}
