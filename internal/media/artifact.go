package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/originaleric/digcrawl/runtime/artifact"
)

// CopyArtifactToFile is the shared, bounded acquisition path for media that
// already belongs to the current runtime. It keeps Skill and Collect from
// diverging on artifact streaming, size limits, format validation, and
// partial-file cleanup.
func CopyArtifactToFile(ctx context.Context, store artifact.Store, artifactID, dest, mediaType string, maxBytes int64) (string, error) {
	if store == nil {
		return "", fmt.Errorf("artifact store is unavailable")
	}
	if maxBytes <= 0 {
		return "", fmt.Errorf("maximum media bytes must be greater than zero")
	}
	if fileStore, ok := store.(artifact.FileStore); ok {
		reader, contentType, err := fileStore.Open(ctx, artifactID)
		if err != nil {
			return "", fmt.Errorf("read media artifact: %w", err)
		}
		defer reader.Close()
		if err := copyBoundedArtifact(dest, reader, maxBytes); err != nil {
			return "", err
		}
		if err := validateCopiedMedia(dest, mediaType); err != nil {
			return "", err
		}
		return contentType, nil
	}
	data, contentType, err := store.Get(ctx, artifactID)
	if err != nil {
		return "", fmt.Errorf("read media artifact: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("media artifact exceeds max bytes %d", maxBytes)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	if err := validateCopiedMedia(dest, mediaType); err != nil {
		return "", err
	}
	return contentType, nil
}

// validateCopiedMedia is intentionally owned by the copy boundary: an
// artifact is another delivery mechanism for the same media contract, not a
// reason to weaken video validation compared with an HTTP or yt-dlp result.
func validateCopiedMedia(dest, mediaType string) error {
	if !strings.EqualFold(strings.TrimSpace(mediaType), "video") {
		return nil
	}
	if err := ValidateVideoFile(dest); err != nil {
		_ = os.Remove(dest)
		return err
	}
	return nil
}

func copyBoundedArtifact(dest string, source io.Reader, maxBytes int64) error {
	target, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(target, io.LimitReader(source, maxBytes+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil || written > maxBytes {
		_ = os.Remove(dest)
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return fmt.Errorf("media artifact exceeds max bytes %d", maxBytes)
	}
	return nil
}
