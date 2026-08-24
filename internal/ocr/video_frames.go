package ocr

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/media"
)

// ExtractLocalVideoFrameRefs samples locally available videos for the existing
// OCR pipeline. The returned cleanup must be called after the image references
// have been recognized. The feature is an explicit OCR.VideoFrames opt-in.
func ExtractLocalVideoFrameRefs(ctx context.Context, cfg config.OCRConfig, videoPaths []string) ([]ImageRef, func(), error) {
	if !cfg.EnabledOrDefault() || !cfg.VideoFrames.EnabledOrDefault() || len(videoPaths) == 0 {
		return nil, func() {}, nil
	}
	remaining := cfg.VideoFrames.MaxTotalFramesOrDefault()
	var refs []ImageRef
	var extracted []media.ExtractedVideoFrames
	var extractionErrs []error
	seen := make(map[string]struct{}, len(videoPaths))
	for _, rawPath := range videoPaths {
		if remaining <= 0 {
			break
		}
		path := filepath.Clean(strings.TrimSpace(rawPath))
		if path == "." || path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		// Reserve this video's share before invoking ffmpeg. A corrupt or
		// timed-out video must not leave the total resource budget untouched
		// and thereby allow an unbounded sequence of extraction attempts.
		allocation := min(cfg.VideoFrames.MaxFramesOrDefault(), remaining)
		remaining -= allocation
		frameCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.VideoFrames.TimeoutSecOrDefault())*time.Second)
		frames, err := media.ExtractVideoFrames(frameCtx, path, media.VideoFrameExtractOptions{
			FFmpegPath:  cfg.VideoFrames.FFmpegPathOrDefault(),
			TempDir:     cfg.VideoFrames.TempDir,
			IntervalSec: cfg.VideoFrames.IntervalSecOrDefault(),
			MaxFrames:   allocation,
		})
		timedOut := errors.Is(frameCtx.Err(), context.DeadlineExceeded)
		cancel()
		if err != nil {
			if timedOut {
				extractionErrs = append(extractionErrs, fmt.Errorf("%s: video frame extraction timed out after %ds", filepath.Base(path), cfg.VideoFrames.TimeoutSecOrDefault()))
				continue
			}
			extractionErrs = append(extractionErrs, fmt.Errorf("%s: %w", filepath.Base(path), err))
			continue
		}
		extracted = append(extracted, frames)
		// ffmpeg is instructed to stop at allocation, but retain the boundary
		// here as well because its file output is external to this process.
		if len(frames.Paths) > allocation {
			frames.Paths = frames.Paths[:allocation]
		}
		for index, framePath := range frames.Paths {
			refs = append(refs, ImageRef{
				LocalPath: framePath,
				Source:    fmt.Sprintf("video_frame:%s:%d", filepath.Base(path), index+1),
				Type:      "video_frame",
			})
		}
	}
	cleanup := func() {
		for _, frames := range extracted {
			frames.Cleanup()
		}
	}
	return refs, cleanup, errors.Join(extractionErrs...)
}

// RecognizeLocalVideoFrames extracts bounded local video frames and recognizes
// them through the configured OCR provider. It never retains the temporary
// frames or returns their paths in results.
func RecognizeLocalVideoFrames(ctx context.Context, cfg config.OCRConfig, videoPath string) ([]Result, error) {
	refs, cleanup, err := ExtractLocalVideoFrameRefs(ctx, cfg, []string{videoPath})
	defer cleanup()
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	return recognizeRefs(ctx, cfg, refs)
}
