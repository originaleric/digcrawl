package browser

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrProfileInUse = errors.New("browser profile in use")
var managedProfileMu sync.Mutex
var managedProfile string

func CanonicalProfilePath(path string) (string, error) {
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

// AcquireProfileLock is the common guard for CLI, login and the local Bridge.
func AcquireProfileLock(profile string) (*ProfileLock, string, error) {
	if strings.TrimSpace(profile) == "" {
		return nil, "", nil
	}
	identity, err := CanonicalProfilePath(profile)
	if err != nil {
		return nil, "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(cache, "digcrawl", "bridge")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, "", err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256([]byte(identity))
	lock, err := acquireProfileFileLock(filepath.Join(dir, fmt.Sprintf("browser-%x.lock", digest[:12])))
	return lock, identity, err
}
func AcquireManagedProfileLock(profile string) (*ProfileLock, string, error) {
	lock, identity, err := AcquireProfileLock(profile)
	if err != nil || lock == nil {
		return lock, identity, err
	}
	managedProfileMu.Lock()
	managedProfile = identity
	lock.managed = true
	lock.identity = identity
	managedProfileMu.Unlock()
	return lock, identity, nil
}
func acquireOwnedProfile(profile string) (*ProfileLock, string, error) {
	identity, err := CanonicalProfilePath(profile)
	if err != nil {
		return nil, "", err
	}
	managedProfileMu.Lock()
	held := managedProfile == identity
	managedProfileMu.Unlock()
	if held {
		return nil, identity, nil
	}
	return AcquireProfileLock(identity)
}

// Explicit stop owns the Bridge lifecycle lock before checking this resource.
func WaitProfileRelease(ctx context.Context, profile string) error {
	if strings.TrimSpace(profile) == "" {
		return nil
	}
	guard, identity, err := AcquireProfileLock(profile)
	if err != nil {
		return err
	}
	defer guard.Close()
	return waitForProfileUnlock(ctx, identity)
}
