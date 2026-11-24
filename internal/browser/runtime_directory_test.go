package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRuntimeDirectoryRejectsSymlinkAndRepairsPrivatePermissions(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skip(err)
	}
	if err := PrepareRuntimeDirectory(alias); err == nil {
		t.Fatal("runtime directory followed a symlink")
	}
	if err := PrepareRuntimeDirectory(target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}
