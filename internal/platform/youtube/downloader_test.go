package youtube

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDownloadVideoUsesCanonicalURLAndEnforcesSize(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	ytDLPCommand = fakeYTDLP(t, "video")
	path, cleanup, err := DownloadVideo(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 16, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Ext(path); got != ".mp4" {
		t.Fatalf("path=%q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "video" {
		t.Fatalf("downloaded data=%q err=%v", data, err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("default temporary download must be removed, stat err=%v", err)
	}
}

func TestDownloadVideoRejectsOversizedOutput(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	ytDLPCommand = fakeYTDLP(t, "oversized")
	_, _, err := DownloadVideo(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", 4, false, "")
	if err == nil || !strings.Contains(err.Error(), "exceeds allowed maximum") {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoPreservesDownloaderFailureDetail(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	ytDLPCommand = fakeYTDLP(t, "failure")
	_, _, err := DownloadVideo(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 16, false, "")
	if err == nil || !strings.Contains(err.Error(), "provider diagnostic") {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoStopsWhenTemporaryDirectoryExceedsBound(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	ytDLPCommand = fakeYTDLP(t, "temporary-oversized")
	_, _, err := DownloadVideo(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 4, false, "")
	if err == nil || !strings.Contains(err.Error(), "temporary download size") {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoPreservesContextDeadline(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	ytDLPCommand = fakeYTDLP(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	_, _, err := DownloadVideo(ctx, "https://youtu.be/dQw4w9WgXcQ", 16, false, "")
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoRetainsOnlyWhenExplicitlyRequested(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	ytDLPCommand = fakeYTDLP(t, "video")
	root := t.TempDir()
	path, cleanup, err := DownloadVideo(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 16, true, root)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("opt-in retained video was removed: %v", err)
	}
	if rel, err := filepath.Rel(root, path); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("retained path=%q must belong to configured directory %q", path, root)
	}
}

// fakeYTDLP builds a native executable rather than writing a shell script.
// The downloader is an external-command boundary, so every operating system
// must exercise the same success and failure contracts in CI.
func fakeYTDLP(t testing.TB, mode string) string {
	t.Helper()
	t.Setenv("DIGCRAWL_TEST_FAKE_YTDLP_MODE", mode)
	name := "fake-yt-dlp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	command := exec.Command("go", "build", "-o", path, "github.com/originaleric/digcrawl/internal/platform/youtube/testdata/fake_ytdlp")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake yt-dlp fixture: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return path
}
