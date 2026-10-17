//go:build !windows

package browser

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
)

type ProfileLock struct {
	file     *os.File
	managed  bool
	identity string
	once     sync.Once
	err      error
}

func acquireProfileFileLock(path string) (*ProfileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open local bridge lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrProfileInUse
		}
		return nil, fmt.Errorf("lock local bridge: %w", err)
	}
	return &ProfileLock{file: file}, nil
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
			_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
			l.err = l.file.Close()
		}
	})
	return l.err
}
