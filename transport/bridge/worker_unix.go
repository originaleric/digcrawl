//go:build !windows

package bridge

import (
	"fmt"
	"os"
	"syscall"
)

// ValidateManagedWorkerIsolation rejects a foreground/shared-group worker.
// Every production Bridge is created by Executor with setsid, so its PID is
// its own PGID and SIGKILL(-PGID) always reaches exactly its Chrome tree.
func ValidateManagedWorkerIsolation() error {
	pid := os.Getpid()
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("inspect local bridge process group: %w", err)
	}
	if pgid != pid {
		return fmt.Errorf("managed local bridge must run in its own process group")
	}
	return nil
}
