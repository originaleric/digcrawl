package browser

import (
	"fmt"
	"os"
	"path/filepath"
)

// RuntimeDirectory is stable across cache cleanup. Resolving it has no side effects.
func RuntimeDirectory() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "digcrawl", "run"), nil
}

// PrepareRuntimeDirectory is used only by resource owners and controllers.
func PrepareRuntimeDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe Bridge runtime directory")
	}
	if err := checkRuntimeOwner(info); err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return os.Chmod(dir, 0700)
	}
	return nil
}
