package bridge

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// canonicalPath makes a local ownership key independent of the way a caller
// spelled it. For a not-yet-created path (such as UserDataDir), it resolves
// the deepest existing parent and appends the missing suffix.
func canonicalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	abs = filepath.Clean(abs)

	missing := []string{}
	for probe := abs; ; {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve path symlinks: %w", err)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return abs, nil
		}
		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
}
