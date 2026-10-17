//go:build windows

package browser

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os/exec"
	"strconv"
	"time"
)

func profileProcessAlive(pid int) (bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return state == uint32(windows.WAIT_TIMEOUT), err
}

func ownedProcessIdentity(ctx context.Context, pid int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime), nil
}
func killVerifiedLauncher(c *browserConnection) error {
	if c.launcher == nil || c.launcher.PID() == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	alive, err := profileProcessAlive(c.launcher.PID())
	if err != nil || !alive {
		return err
	}
	identity, err := ownedProcessIdentity(ctx, c.launcher.PID())
	if err != nil {
		return err
	}
	if identity == "" || identity != c.ownerIdentity {
		return fmt.Errorf("owned browser process identity changed")
	}
	// Native tree termination, scoped to the verified owner PID.
	return exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(c.launcher.PID()), "/T", "/F").Run()
}
func waitForOwnedProcessRelease(ctx context.Context, c *browserConnection) error {
	if c.launcher == nil || c.launcher.PID() == 0 {
		return nil
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive, err := profileProcessAlive(c.launcher.PID())
		if err != nil {
			return err
		}
		if !alive {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Windows profile ownership is enforced by the common OS lock and Chrome's
// native process checks; Unix socket/Symlink conventions are not assumed.
func profileSocketReleased(string) error { return nil }
