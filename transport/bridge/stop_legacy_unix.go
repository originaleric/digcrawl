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
)

// stopLegacyBridge is deliberately narrow migration support for pre-control
// endpoint releases. New Bridges are always stopped through the private
// socket. Here we only signal a process whose command line names this exact
// socket, never a broad process-name match.
func stopLegacyBridge(ctx context.Context, socket string) (bool, error) {
	output, err := exec.CommandContext(ctx, "ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return false, fmt.Errorf("inspect legacy local bridge: %w", err)
	}
	pid := legacyBridgePID(string(output), socket)
	if pid == 0 {
		return false, ErrBridgeControlUnsupported
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
		return false, fmt.Errorf("stop legacy local bridge: %w", err)
	}
	if err := waitForBridgeStopped(ctx, socket); err != nil {
		return false, err
	}
	return true, nil
}

func legacyBridgePID(processes, socket string) int {
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
