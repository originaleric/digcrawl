package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const OCRImageAccept = "image/jpeg,image/png,image/webp,image/*;q=0.8,*/*;q=0.5"

// PreferOCRCompatibleImageFormat asks an image CDN for formats accepted by
// the configured OpenAI-compatible OCR providers. It intentionally leaves
// audio/video requests untouched.
func PreferOCRCompatibleImageFormat(req *http.Request, mediaType string) {
	if req == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image", "photo", "screenshot":
		req.Header.Set("Accept", OCRImageAccept)
	}
}

// DownloadFile is the shared bounded media download path for Skill and
// Collect. It owns HTTP negotiation that affects OCR compatibility while the
// callers retain their distinct artifact and export delivery policies.
func DownloadFile(ctx context.Context, client *http.Client, rawURL, dest string, maxBytes int64, referer, mediaType string) (string, error) {
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(dest)
		}
	}()
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	PreferOCRCompatibleImageFormat(req, mediaType)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("content length %d exceeds max %d", resp.ContentLength, maxBytes)
	}
	target, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	written, copyErr := io.Copy(target, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := target.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written > maxBytes {
		return "", fmt.Errorf("download exceeds max bytes %d", maxBytes)
	}
	completed = true
	return resp.Header.Get("Content-Type"), nil
}
