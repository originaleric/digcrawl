//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package asr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func lockLocalASRCache(ctx context.Context, canonical string) (func(), error) {
	// Keep this file after unlocking: removing it could let a new process lock
	// a different inode while another process is still waiting on the old one.
	// Its control-file prefix keeps it out of hasASRModelData.
	file, err := os.OpenFile(filepath.Join(canonical, ".digcrawl-asr-cache-v1.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
