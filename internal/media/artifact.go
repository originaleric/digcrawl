package media

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/originaleric/digcrawl/runtime/artifact"
)

// CopyArtifactToFile is the shared, bounded acquisition path for media that
// already belongs to the current runtime. It keeps Skill and Collect from
// diverging on artifact streaming, size limits, and partial-file cleanup.
func CopyArtifactToFile(ctx context.Context, store artifact.Store, artifactID, dest string, maxBytes int64) (string, error) {
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
	return contentType, nil
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
