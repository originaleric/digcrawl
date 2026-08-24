package ocr

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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
	remaining := cfg.VideoFrames.MaxFramesOrDefault()
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
		frames, err := media.ExtractVideoFrames(ctx, path, media.VideoFrameExtractOptions{
			FFmpegPath:  cfg.VideoFrames.FFmpegPathOrDefault(),
			TempDir:     cfg.VideoFrames.TempDir,
			IntervalSec: cfg.VideoFrames.IntervalSecOrDefault(),
			MaxFrames:   remaining,
		})
		if err != nil {
			extractionErrs = append(extractionErrs, fmt.Errorf("%s: %w", filepath.Base(path), err))
			continue
		}
		extracted = append(extracted, frames)
		for _, framePath := range frames.Paths {
			refs = append(refs, ImageRef{
				LocalPath: framePath,
				Source:    "video_frame",
				Type:      "video_frame",
			})
			remaining--
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
