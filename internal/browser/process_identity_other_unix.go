//go:build !darwin && !windows

package browser

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func ProcessIdentity(ctx context.Context, pid int) (string, error) {
	command := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "lstart=,comm=")
	command.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	output, err := command.Output()
	if err != nil {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return "", syscall.ESRCH
		}
		return "", err
	}
	return fmt.Sprintf("process-v2:%x", sha256.Sum256(output)), nil
}

// Legacy fingerprints are still matched directly on these platforms.
func ProcessStartedAt(context.Context, int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("native legacy owner migration requires macOS")
}
