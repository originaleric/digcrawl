//go:build !windows

package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
)

// bridgeOwner is a recovery record, not a second lock. The lifecycle lock is
// authoritative while the Bridge lives. This record closes the crash window
// after a direct group kill: a new starter can wait for the old browser group
// even after the killed Bridge released its flock.
type bridgeOwner struct {
	pid         int
	pgid        int
	identity    string
	browsers    []recordedBrowserProcess
	publishedAt time.Time
}

const maxBridgeOwnerRecordBytes = 1 << 20

type recordedBrowserProcess struct {
	pid      int
	pgid     int
	identity string
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, err := bridgeProcessIdentity(ctx, pid)
	if err != nil {
		return err
	}
	return writeBridgeOwnerRecord(socket, bridgeOwner{pid: pid, pgid: pgid, identity: identity})
}

func writeBridgeOwnerRecord(socket string, owner bridgeOwner) error {
	if len(owner.browsers) > 0 && owner.identity == "" {
		return fmt.Errorf("recorded browser has no Bridge start identity")
	}
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
	if _, err := fmt.Fprintf(temporary, "%d %d %s\n", owner.pid, owner.pgid, owner.identity); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write local bridge owner record: %w", err)
	}
	for _, process := range owner.browsers {
		if _, err := fmt.Fprintf(temporary, "%d %d %s\n", process.pid, process.pgid, process.identity); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if info, err := temporary.Stat(); err != nil || info.Size() > maxBridgeOwnerRecordBytes {
		_ = temporary.Close()
		return fmt.Errorf("Bridge owner record exceeds the supported size")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
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
	file, err := os.Open(bridgeOwnerPath(socket))
	if errors.Is(err, os.ErrNotExist) {
		return bridgeOwner{}, false, nil
	}
	if err != nil {
		return bridgeOwner{}, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBridgeOwnerRecordBytes+1))
	if err != nil {
		return bridgeOwner{}, false, err
	}
	if len(data) > maxBridgeOwnerRecordBytes {
		return bridgeOwner{}, false, fmt.Errorf("oversized local Bridge owner record")
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 && (len(fields) < 3 || len(fields)%3 != 0) {
		return bridgeOwner{}, false, fmt.Errorf("read local bridge owner record: malformed contents")
	}
	pid, pidErr := strconv.Atoi(fields[0])
	pgid, groupErr := strconv.Atoi(fields[1])
	if pidErr != nil || groupErr != nil || pid <= 0 || pgid < 0 {
		return bridgeOwner{}, false, fmt.Errorf("read local bridge owner record: malformed process identity")
	}
	info, err := file.Stat()
	if err != nil {
		return bridgeOwner{}, false, err
	}
	owner := bridgeOwner{pid: pid, pgid: pgid, publishedAt: info.ModTime()}
	if len(fields) >= 3 {
		owner.identity = fields[2]
		if !validProcessIdentity(owner.identity) {
			return bridgeOwner{}, false, fmt.Errorf("invalid recorded Bridge identity")
		}
	}
	for i := 3; i+2 < len(fields); i += 3 {
		pid, pidErr := strconv.Atoi(fields[i])
		pgid, groupErr := strconv.Atoi(fields[i+1])
		if pidErr != nil || groupErr != nil || pid <= 0 || pgid != pid || pid == owner.pid || pgid == owner.pgid || !validProcessIdentity(fields[i+2]) {
			return bridgeOwner{}, false, fmt.Errorf("invalid recorded browser identity")
		}
		owner.browsers = append(owner.browsers, recordedBrowserProcess{pid: pid, pgid: pgid, identity: fields[i+2]})
	}

	return owner, true, nil
}

func validProcessIdentity(identity string) bool {
	if strings.HasPrefix(identity, "process-v2:") {
		identity = strings.TrimPrefix(identity, "process-v2:")
		if fields := strings.Split(identity, ":"); len(fields) == 2 {
			seconds, err := strconv.ParseInt(fields[0], 10, 64)
			nanos, nanoErr := strconv.ParseInt(fields[1], 10, 32)
			return err == nil && nanoErr == nil && seconds > 0 && nanos >= 0 && nanos < int64(time.Second)
		}
	}
	decoded, err := hex.DecodeString(identity)
	return err == nil && len(decoded) == sha256.Size
}

func bridgeProcessIdentity(ctx context.Context, pid int) (string, error) {
	return browser.ProcessIdentity(ctx, pid)
}

// All lifecycle decisions use this compatibility boundary. Old fingerprints
// are accepted only for the same live process that predates record publication.
func ownerIdentityMatches(ctx context.Context, owner bridgeOwner) (bool, error) {
	current, err := bridgeProcessIdentity(ctx, owner.pid)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current == owner.identity {
		return true, nil
	}
	if strings.HasPrefix(owner.identity, "process-v2:") {
		return false, nil
	}
	decoded, err := hex.DecodeString(owner.identity)
	if err != nil || len(decoded) != sha256.Size {
		return false, nil
	}
	// Even an old fingerprint that matches to the second cannot authorize a
	// recycled PID created after publication. On macOS the kernel fact is the
	// compatibility authority, independent of the caller's display environment.
	if runtime.GOOS == "darwin" && !owner.publishedAt.IsZero() {
		started, err := browser.ProcessStartedAt(ctx, owner.pid)
		if err != nil {
			return false, err
		}
		return !started.After(owner.publishedAt), nil
	}
	// Direct matching preserves older records on other Unix platforms.
	legacy := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(owner.pid), "-o", "lstart=,command=")
	output, err := legacy.Output()
	if err != nil {
		return false, err
	}
	return fmt.Sprintf("%x", sha256.Sum256(output)) == owner.identity, nil
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
	if err := recoverRecordedBrowser(ctx, owner); err != nil {
		return err
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
	// Callers hold the lifecycle lock while reclaiming verified browser groups.
	// Preserve the record until replacement publication or verified controller
	// cleanup so interruption cannot discard the predecessor's resource identity.
	return nil
}

// This writer remains owned by the Bridge holding the lifecycle lock. Browser
// launch publishes its exact private group before connecting CDP or serving it.
func recordBridgeBrowserProcesses(socket string) (func(), error) {
	owner, exists, err := readBridgeOwner(socket)
	if err != nil {
		return nil, err
	}
	if !exists || owner.pid != os.Getpid() {
		return nil, fmt.Errorf("Bridge process record has no current owner")
	}
	return browser.RegisterOwnedProcessRecorder(func(pid int) error {
		pgid, err := bridgeProcessGroup(pid)
		if err != nil {
			return err
		}
		if pgid != pid {
			return fmt.Errorf("owned browser has no private process group")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		identity, err := browserProcessIdentity(ctx, pid)
		if err != nil {
			return err
		}
		retained := make([]recordedBrowserProcess, 0, len(owner.browsers)+1)
		for _, process := range owner.browsers {
			if err := ctx.Err(); err != nil {
				return err
			}
			if process.pid == pid && process.identity == identity {
				continue
			}
			current, inspectErr := browserProcessIdentity(ctx, process.pid)
			if inspectErr == nil && current != process.identity {
				continue
			}
			if inspectErr != nil && errors.Is(syscall.Kill(process.pid, 0), syscall.ESRCH) {
				released, releaseErr := bridgeGroupReleased(process.pgid)
				if releaseErr == nil && released {
					continue
				}
			}
			retained = append(retained, process)
		}
		owner.browsers = append(retained, recordedBrowserProcess{pid: pid, pgid: pgid, identity: identity})
		return writeBridgeOwnerRecord(socket, owner)
	})
}

func recoverRecordedBrowser(ctx context.Context, owner bridgeOwner) error {
	if len(owner.browsers) == 0 {
		return nil
	}
	// A removed/replaced lifecycle lock must not authorize stealing a live
	// predecessor's browser. The controller must stop that exact owner first.
	if owner.identity != "" {
		matches, err := ownerIdentityMatches(ctx, owner)
		if err != nil {
			return err
		}
		if matches {
			alive, err := browser.ProcessAlive(owner.pid)
			if err != nil {
				return err
			}
			if alive {
				return ErrAlreadyRunning
			}
		}
	}

	for _, process := range owner.browsers {
		if err := recoverBrowserProcess(ctx, process); err != nil {
			return err
		}
	}
	return nil
}

func recoverBrowserProcess(ctx context.Context, process recordedBrowserProcess) error {
	identity, err := browserProcessIdentity(ctx, process.pid)
	if err != nil {
		if !errors.Is(err, syscall.ESRCH) {
			return err
		}
		// Unknown surviving members cannot be signalled from a numeric PGID alone.
		return waitForBridgeGroupRelease(ctx, process.pgid)
	}
	if identity != process.identity {
		// Earlier browser records used a canonical ps fingerprint.
		if strings.HasPrefix(process.identity, "process-v2:") {
			return nil
		}
		command := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(process.pid), "-o", "lstart=,comm=")
		command.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
		output, err := command.Output()
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(output)) != process.identity {
			return nil
		}
	}
	pgid, err := bridgeProcessGroup(process.pid)
	if errors.Is(err, syscall.ESRCH) {
		return waitForBridgeGroupRelease(ctx, process.pgid)
	}
	if err != nil {
		return err
	}
	if pgid != process.pgid || pgid != process.pid {
		return fmt.Errorf("recorded browser changed process group")
	}
	// Recheck start identity immediately before signalling, never a bare stale PID.
	current, err := browserProcessIdentity(ctx, process.pid)
	if err != nil || current != identity {
		return fmt.Errorf("recorded browser identity changed before cleanup")
	}
	if err := signalBridgeProcess(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return waitForBridgeGroupRelease(ctx, pgid)
}

// Browser identity uses start time and executable, independent of caller TZ,
// locale and browser changes to its process title.
func browserProcessIdentity(ctx context.Context, pid int) (string, error) {
	return browser.ProcessIdentity(ctx, pid)
}
