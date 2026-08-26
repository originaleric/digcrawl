package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadVideoUsesCanonicalURLAndEnforcesSize(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	dir := t.TempDir()
	command := filepath.Join(dir, "fake-yt-dlp")
	script := "#!/bin/sh\nout=\"\"\nlast=\"\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--output\" ]; then out=\"$2\"; shift 2; continue; fi\n  last=\"$1\"; shift\ndone\npath=\"$(dirname \"$out\")/video.mp4\"\nprintf 'video' > \"$path\"\nprintf '%s\\n' \"$path\"\nprintf '%s\\n' \"$last\" >&2\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDLPCommand = command
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
	dir := t.TempDir()
	command := filepath.Join(dir, "fake-yt-dlp")
	script := "#!/bin/sh\nout=\"\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--output\" ]; then out=\"$2\"; shift 2; continue; fi\n  shift\ndone\npath=\"$(dirname \"$out\")/video.mp4\"\nprintf 'this file is too large' > \"$path\"\nprintf '%s\\n' \"$path\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDLPCommand = command
	_, _, err := DownloadVideo(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ", 4, false, "")
	if err == nil || !strings.Contains(err.Error(), "exceeds allowed maximum") {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoPreservesDownloaderFailureDetail(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	command := filepath.Join(t.TempDir(), "fake-yt-dlp")
	if err := os.WriteFile(command, []byte("#!/bin/sh\necho 'provider diagnostic' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDLPCommand = command
	_, _, err := DownloadVideo(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 16, false, "")
	if err == nil || !strings.Contains(err.Error(), "provider diagnostic") {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoStopsWhenTemporaryDirectoryExceedsBound(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	command := filepath.Join(t.TempDir(), "fake-yt-dlp")
	script := "#!/bin/sh\nout=\"\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--output\" ]; then out=\"$2\"; shift 2; continue; fi\n  shift\ndone\ndir=\"$(dirname \"$out\")\"\ndd if=/dev/zero of=\"$dir/partial\" bs=1 count=13 2>/dev/null\nsleep 2\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDLPCommand = command
	_, _, err := DownloadVideo(context.Background(), "https://youtu.be/dQw4w9WgXcQ", 4, false, "")
	if err == nil || !strings.Contains(err.Error(), "temporary download size") {
		t.Fatalf("err=%v", err)
	}
}

func TestDownloadVideoPreservesContextDeadline(t *testing.T) {
	oldCommand := ytDLPCommand
	t.Cleanup(func() { ytDLPCommand = oldCommand })
	command := filepath.Join(t.TempDir(), "fake-yt-dlp")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDLPCommand = command
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
	command := filepath.Join(t.TempDir(), "fake-yt-dlp")
	script := "#!/bin/sh\nout=\"\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--output\" ]; then out=\"$2\"; shift 2; continue; fi\n  shift\ndone\npath=\"$(dirname \"$out\")/video.mp4\"\nprintf 'video' > \"$path\"\nprintf '%s\\n' \"$path\"\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDLPCommand = command
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
