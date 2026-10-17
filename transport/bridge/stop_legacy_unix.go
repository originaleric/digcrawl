//go:build !windows

package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// stopBridgeOwner signals the current Bridge exactly once. Managed workers
// publish their private group under the lifecycle lock; process-table matching
// remains only for old workers that predate that record.
func stopBridgeOwner(ctx context.Context, socket string) (bool, error) {
	owner, private, err := readPrivateBridgeOwner(socket)
	if err != nil {
		return false, err
	}
	if private {
		current, err := recordedBridgeOwnerIsCurrent(ctx, socket, owner)
		if err != nil {
			return false, err
		}
		if !current {
			// The lifecycle lock can briefly be held between recovery and a new
			// owner's record publication. More importantly, a stale PID/PGID must
			// never be trusted as a kill target: numeric IDs can be reused.
			return false, ErrBridgeOwnerNotFound
		}
		if err := signalBridgeProcess(-owner.pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
			return false, fmt.Errorf("force-stop local bridge process group: %w", err)
		}
		return true, nil
	}
	output, err := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fmt.Errorf("inspect local bridge: %w", err)
	}
	pid := bridgePID(string(output), socket)
	if pid == 0 {
		return false, ErrBridgeOwnerNotFound
	}
	return terminateExactBridgeProcess(pid)
}

// recordedBridgeOwnerIsCurrent turns the small owner file into a checked
// process identity before it is used as a signal target. The lifecycle lock
// serializes workers; this check prevents an interrupted old record from
// targeting a recycled PID or process group during the worker's short setup.
func recordedBridgeOwnerIsCurrent(ctx context.Context, socket string, owner bridgeOwner) (bool, error) {
	output, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(owner.pid), "-o", "pid=,command=").Output()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if _, exited := err.(*exec.ExitError); exited {
			return false, nil
		}
		return false, fmt.Errorf("inspect recorded local bridge owner: %w", err)
	}
	if bridgePID(string(output), socket) != owner.pid {
		return false, nil
	}
	if owner.identity != "" {
		identity, err := bridgeProcessIdentity(ctx, owner.pid)
		if err != nil {
			return false, err
		}
		if identity != owner.identity {
			return false, nil
		}
	}
	pgid, err := bridgeProcessGroup(owner.pid)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect recorded local bridge process group: %w", err)
	}
	return pgid == owner.pgid, nil
}

// terminateExactBridgeProcess is the migration fallback for a worker from an
// older build that lacks a private owner record. It does not request graceful
// HTTP shutdown before stopping the verified process.
//
// A Bridge predating the private-session launch may share its parent's group.
// For that migration-only shape we kill only the verified PID, never a shared
// process group that could contain the host or an unrelated process.
func terminateExactBridgeProcess(pid int) (bool, error) {
	pgid, err := bridgeProcessGroup(pid)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, fmt.Errorf("inspect local bridge process group: %w", err)
	}
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}

	target := pid
	if pgid == pid {
		// POSIX kill accepts a negative PGID to target only that group.
		target = -pgid
	}
	if err := signalBridgeProcess(target, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
		return false, fmt.Errorf("force-stop local bridge: %w", err)
	}
	return true, nil
}

var signalBridgeProcess = syscall.Kill
var bridgeProcessGroup = syscall.Getpgid

func bridgePID(processes, socket string) int {
	needle := "--socket " + socket
	for _, line := range strings.Split(processes, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(line, "bridge serve") || !hasSocketArgument(line, needle) {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// waitForBridgeGroupRelease waits only for members of a previously recorded
// private Bridge group. It intentionally does not inspect the lifecycle lock:
// direct Serve holds that lock while recovering the predecessor's record.
func waitForBridgeGroupRelease(ctx context.Context, pgid int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		released, err := bridgeGroupReleased(pgid)
		if err != nil {
			return err
		}
		if released {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func bridgeGroupReleased(pgid int) (bool, error) {
	if pgid <= 0 {
		return true, nil
	}
	// On macOS, kill(-pgid, 0) can report EPERM for a group which is already
	// draining through launchd, even though this user delivered the preceding
	// SIGKILL. Inspecting the process table is both more diagnostic and lets us
	// distinguish a still-present uninterruptible child from that platform edge.
	output, err := exec.Command("ps", "-ax", "-o", "pid=,pgid=,stat=").Output()
	if err != nil {
		return false, fmt.Errorf("inspect local bridge process group: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		observed, parseErr := strconv.Atoi(fields[1])
		// A zombie has already released its Chrome/profile resources and cannot
		// execute. Waiting for launchd to reap that bookkeeping record would
		// reintroduce an OS-scheduler dependency into restart correctness.
		if parseErr == nil && observed == pgid && !strings.HasPrefix(fields[2], "Z") {
			return false, nil
		}
	}
	return true, nil
}

// hasSocketArgument prevents a prefix match from signalling another profile
// whose generated socket happens to begin with this profile's socket path.
// Bridge paths do not need shell parsing here: this migration guard only
// accepts the normal unquoted `--socket <path>` command emitted by DigCrawl.
func hasSocketArgument(command, needle string) bool {
	for offset := 0; ; {
		index := strings.Index(command[offset:], needle)
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(needle)
		if end == len(command) || command[end] == ' ' || command[end] == '\t' {
			return true
		}
		offset = end
	}
}

// Called under the lifecycle lock, including when old workers predate that
// lock. Enumerate every exact socket candidate, not only the first PID.
func stopLegacyBridgeCandidates(ctx context.Context, socket string) (bool, error) {
	output, err := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return false, err
	}
	stopped := false
	for _, line := range strings.Split(string(output), "\n") {
		pid := bridgePID(line, socket)
		if pid == 0 || pid == os.Getpid() {
			continue
		}
		// Recheck after enumeration so an exited/replaced PID is not trusted.
		owner := bridgeOwner{pid: pid}
		current, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(owner.pid), "-o", "pid=,command=").Output()
		if err != nil {
			if _, gone := err.(*exec.ExitError); gone {
				continue
			}
			return stopped, err
		}
		if bridgePID(string(current), socket) != pid {
			continue
		}
		pgid, err := bridgeProcessGroup(pid)
		if errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return stopped, err
		}
		signalled, err := terminateExactBridgeProcess(pid)
		if err != nil {
			return stopped, err
		}
		stopped = stopped || signalled
		if pgid == pid {
			if err := waitForBridgeGroupRelease(ctx, pgid); err != nil {
				return stopped, err
			}
		}
	}
	return stopped, nil
}
