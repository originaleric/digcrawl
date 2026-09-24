//go:build !windows

package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// bridgeOwner is a recovery record, not a second lock. The lifecycle lock is
// authoritative while the Bridge lives. This record closes the crash window
// after a direct group kill: a new starter can wait for the old browser group
// even after the killed Bridge released its flock.
type bridgeOwner struct {
	pid  int
	pgid int
}

func bridgeOwnerPath(socket string) string { return socket + ".owner" }

func writeBridgeOwner(socket string) error {
	pid := os.Getpid()
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("inspect local bridge process group: %w", err)
	}
	// Only a private session is safe to recover as a group. A Bridge launched
	// by an older build can share a parent group, so record it as PID-only.
	if pgid != pid {
		pgid = 0
	}
	return writeBridgeOwnerRecord(socket, bridgeOwner{pid: pid, pgid: pgid})
}

func writeBridgeOwnerRecord(socket string, owner bridgeOwner) error {
	dir := filepath.Dir(bridgeOwnerPath(socket))
	temporary, err := os.CreateTemp(dir, filepath.Base(bridgeOwnerPath(socket))+"-")
	if err != nil {
		return fmt.Errorf("create local bridge owner record: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect local bridge owner record: %w", err)
	}
	if _, err := fmt.Fprintf(temporary, "%d %d\n", owner.pid, owner.pgid); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write local bridge owner record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close local bridge owner record: %w", err)
	}
	if err := os.Rename(temporaryPath, bridgeOwnerPath(socket)); err != nil {
		return fmt.Errorf("publish local bridge owner record: %w", err)
	}
	return nil
}

func removeBridgeOwner(socket string) {
	_ = os.Remove(bridgeOwnerPath(socket))
}

func readBridgeOwner(socket string) (bridgeOwner, bool, error) {
	data, err := os.ReadFile(bridgeOwnerPath(socket))
	if errors.Is(err, os.ErrNotExist) {
		return bridgeOwner{}, false, nil
	}
	if err != nil {
		return bridgeOwner{}, false, fmt.Errorf("read local bridge owner record: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return bridgeOwner{}, false, fmt.Errorf("read local bridge owner record: malformed contents")
	}
	pid, pidErr := strconv.Atoi(fields[0])
	pgid, groupErr := strconv.Atoi(fields[1])
	if pidErr != nil || groupErr != nil || pid <= 0 || pgid < 0 {
		return bridgeOwner{}, false, fmt.Errorf("read local bridge owner record: malformed process identity")
	}
	return bridgeOwner{pid: pid, pgid: pgid}, true, nil
}

func readPrivateBridgeOwner(socket string) (bridgeOwner, bool, error) {
	owner, exists, err := readBridgeOwner(socket)
	if err != nil || !exists {
		return owner, exists, err
	}
	return owner, owner.pgid > 0 && owner.pgid == owner.pid, nil
}

func waitForRecordedBridgeGroupRelease(ctx context.Context, socket string) error {
	owner, exists, err := readBridgeOwner(socket)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if owner.pgid != 0 {
		// Callers establish that the predecessor's lifecycle lock is already
		// released before reaching this recovery gate. The replacement worker
		// now holds that lock while it runs this check. Re-checking the lock here
		// would therefore wait on itself; this record's only job is to wait for
		// the predecessor's private group.
		if err := waitForBridgeGroupRelease(ctx, owner.pgid); err != nil {
			return fmt.Errorf("wait for previous local bridge process group: %w", err)
		}
	}
	// This is a read-only recovery gate. Only Serve, while it owns the lifecycle
	// lock, publishes (and atomically replaces) this record. A normal MCP
	// starter owns no lifecycle state, so deleting here could race a worker that
	// has already become the new owner.
	return nil
}
