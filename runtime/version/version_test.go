package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableMatchesRunningBytesAfterPublication(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "published")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if matches, err := ExecutableMatches(path); err != nil || !matches {
		t.Fatalf("same executable: matches=%v err=%v", matches, err)
	}
	// Release commit labels may be identical. Changed executable bytes must
	// still make an already-running watcher yield before consuming state.
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if matches, err := ExecutableMatches(path); err != nil || matches {
		t.Fatalf("replaced executable: matches=%v err=%v", matches, err)
	}
	if _, err := ExecutableMatches(path + "missing"); err == nil {
		t.Fatal("missing publication accepted")
	}
}
