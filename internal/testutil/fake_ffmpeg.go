// Package testutil provides fixtures shared by integration-style Go tests.
package testutil

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// FakeFFmpeg builds a native fake ffmpeg executable in dir. It writes a small
// WAV payload to the final command-line argument, matching the contract used by
// media.ExtractAudio. A native binary keeps process-boundary tests portable
// across Windows, macOS, and Linux.
func FakeFFmpeg(t testing.TB, dir string) string {
	t.Helper()
	name := "fake-ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	command := exec.Command("go", "build", "-o", path, "github.com/originaleric/digcrawl/internal/testutil/fakeffmpeg")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake ffmpeg fixture: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return path
}
