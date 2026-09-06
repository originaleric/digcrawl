package youtube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/media"
)

var ytDLPCommand = "yt-dlp"

const (
	temporaryDownloadMultiplier = int64(3)
	downloadMonitorInterval     = 100 * time.Millisecond
)

// DownloadVideo uses yt-dlp only for an explicit YouTube media request. It
// resolves YouTube's adaptive streams and returns one local MP4 for the
// caller-owned ASR/OCR pipeline. By default the returned cleanup removes the
// download; retain stores it under retainDir and makes cleanup a no-op.
func DownloadVideo(ctx context.Context, rawURL string, maxBytes int64, retain bool, retainDir string) (path string, cleanup func(), err error) {
	norm, err := NormalizeURL(rawURL)
	if err != nil {
		return "", func() {}, err
	}
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	command, err := exec.LookPath(ytDLPCommand)
	if err != nil {
		return "", func() {}, fmt.Errorf("YouTube media download requires yt-dlp on PATH: %w", err)
	}
	parent := ""
	if retain {
		parent = strings.TrimSpace(retainDir)
		if parent == "" {
			return "", func() {}, fmt.Errorf("retained YouTube video requires Skill.Media.Dir")
		}
		if err := os.MkdirAll(parent, 0o750); err != nil {
			return "", func() {}, fmt.Errorf("create retained YouTube media directory: %w", err)
		}
	}
	dir, err := os.MkdirTemp(parent, "digcrawl-youtube-download-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create YouTube download directory: %w", err)
	}
	removeDownload := func() { _ = os.RemoveAll(dir) }
	cleanup = removeDownload
	outputTemplate := filepath.Join(dir, "video.%(ext)s")
	args := []string{
		// The canonical URL is the only user-derived network input. Ignore
		// user/system yt-dlp configuration so it cannot add a proxy,
		// downloader, or command hook outside this provider boundary.
		"--ignore-config",
		"--proxy", "",
		"--no-playlist", "--no-progress", "--no-warnings",
		"--remux-video", "mp4",
		// ASR and frame OCR need a playable file, not a high-resolution archive.
		// Capping the selected stream bounds download time and local disk usage.
		"--format", "bestvideo[height<=360][ext=mp4]+bestaudio[ext=m4a]/best[height<=360][ext=mp4]/best[height<=360]",
		"--max-filesize", fmt.Sprintf("%d", maxBytes),
		"--output", outputTemplate,
		"--print", "after_move:filepath",
		norm.CanonicalURL,
	}
	output, stderr, runErr := runYTDLP(ctx, command, args, dir, maxTemporaryDownloadBytes(maxBytes))
	if runErr != nil {
		removeDownload()
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return "", func() {}, fmt.Errorf("yt-dlp download: %w", runErr)
		}
		message := strings.TrimSpace(string(output))
		if strings.TrimSpace(string(stderr)) != "" {
			message = strings.TrimSpace(string(stderr))
		}
		if message == "" {
			message = runErr.Error()
		}
		return "", func() {}, fmt.Errorf("yt-dlp download: %s", message)
	}
	path = lastOutputPath(string(output))
	if path == "" {
		removeDownload()
		return "", func() {}, fmt.Errorf("yt-dlp did not report a downloaded video path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	path = filepath.Clean(path)
	rel, relErr := filepath.Rel(dir, path)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		removeDownload()
		return "", func() {}, fmt.Errorf("yt-dlp returned a path outside the YouTube download directory")
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		removeDownload()
		return "", func() {}, fmt.Errorf("stat downloaded YouTube video: %w", statErr)
	}
	if !info.Mode().IsRegular() {
		removeDownload()
		return "", func() {}, fmt.Errorf("downloaded YouTube media is not a regular file")
	}
	if !strings.EqualFold(filepath.Ext(path), ".mp4") {
		removeDownload()
		return "", func() {}, fmt.Errorf("yt-dlp did not produce an MP4 video")
	}
	if info.Size() <= 0 || info.Size() > maxBytes {
		removeDownload()
		return "", func() {}, fmt.Errorf("downloaded YouTube video size %d exceeds allowed maximum %d", info.Size(), maxBytes)
	}
	// yt-dlp controls the final extension, but a successful command alone is
	// not proof that the retained file is playable video. Use the same
	// format-level check as the direct provider-media downloader so every
	// durable video result honors one definition of "video".
	if err := media.ValidateVideoFile(path); err != nil {
		removeDownload()
		return "", func() {}, fmt.Errorf("downloaded YouTube media is not a recognized video file: %w", err)
	}
	if retain {
		cleanup = func() {}
	}
	return path, cleanup, nil
}

func maxTemporaryDownloadBytes(maxBytes int64) int64 {
	if maxBytes <= 0 {
		return 0
	}
	if maxBytes > int64(^uint64(0)>>1)/temporaryDownloadMultiplier {
		return int64(^uint64(0) >> 1)
	}
	return maxBytes * temporaryDownloadMultiplier
}

// runYTDLP observes the downloader's private directory while it runs. The
// final artifact remains limited by maxBytes; the finite multiplier leaves
// room for yt-dlp's separate video/audio files and remux output.
func runYTDLP(ctx context.Context, command string, args []string, dir string, maxTemporaryBytes int64) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(downloadMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil && ctx.Err() != nil {
				ctxErr := ctx.Err()
				return stdout.Bytes(), stderr.Bytes(), ctxErr
			}
			return stdout.Bytes(), stderr.Bytes(), err
		case <-ticker.C:
			size, err := downloadDirectorySize(dir)
			if err == nil && (maxTemporaryBytes <= 0 || size <= maxTemporaryBytes) {
				continue
			}
			_ = cmd.Process.Kill()
			<-done
			if err != nil {
				return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("measure YouTube temporary download directory: %w", err)
			}
			return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("YouTube temporary download size %d exceeds allowed maximum %d", size, maxTemporaryBytes)
		}
	}
}

func downloadDirectorySize(dir string) (int64, error) {
	var size int64
	err := filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func lastOutputPath(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
