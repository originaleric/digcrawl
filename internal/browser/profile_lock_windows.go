//go:build windows

package browser

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

// LockFileEx is released by Windows when the owning process dies. Unlike an
// O_EXCL sentinel file, it cannot leave future Bridge starts permanently
// blocked after a crash.
type ProfileLock struct {
	file       *os.File
	managed    bool
	identity   string
	once       sync.Once
	err        error
	overlapped windows.Overlapped
}

func acquireProfileFileLock(path string) (*ProfileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open local bridge lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("protect local bridge lock: %w", err)
	}
	lock := &ProfileLock{file: file}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped); err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrProfileInUse
		}
		return nil, fmt.Errorf("lock local bridge: %w", err)
	}
	return lock, nil
}

func (l *ProfileLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if l.managed {
			managedProfileMu.Lock()
			if managedProfile == l.identity {
				managedProfile = ""
			}
			managedProfileMu.Unlock()
		}
		if l.file != nil {
			_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
			l.err = l.file.Close()
		}
	})
	return l.err
}
