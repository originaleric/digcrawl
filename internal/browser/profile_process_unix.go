//go:build !windows

package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func profileProcessAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	return false, err
}

func ownedProcessIdentity(ctx context.Context, pid int) (string, error) {
	if pid <= 0 {
		return "", nil
	}
	output, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "lstart=,comm=").Output()
	if err != nil {
		if _, gone := err.(*exec.ExitError); gone {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
func ownedProcessGroupReleased(ctx context.Context, pid int) (bool, error) {
	output, err := exec.CommandContext(ctx, "ps", "-ax", "-o", "pgid=,stat=").Output()
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(output), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		group, _ := strconv.Atoi(f[0])
		if group == pid && !strings.HasPrefix(f[1], "Z") {
			return false, nil
		}
	}
	return true, nil
}
func killVerifiedLauncher(c *browserConnection) error {
	if c.launcher == nil || c.launcher.PID() == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	identity, err := ownedProcessIdentity(ctx, c.launcher.PID())
	if err != nil {
		return err
	}
	if identity != "" && (c.ownerIdentity == "" || identity != c.ownerIdentity) {
		return fmt.Errorf("owned browser process identity changed")
	}
	if identity != "" {
		pgid, err := syscall.Getpgid(c.launcher.PID())
		if err != nil {
			return err
		}
		if pgid != c.launcher.PID() {
			return fmt.Errorf("owned browser is not in its private process group")
		}
		err = syscall.Kill(-pgid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	// The leader is gone, but its recorded private group may still own children.
	// Never send a positive PID signal after the leader has exited.
	if c.ownerIdentity != "" {
		err = syscall.Kill(-c.launcher.PID(), syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}
func waitForOwnedProcessRelease(ctx context.Context, c *browserConnection) error {
	if c.launcher == nil || c.launcher.PID() == 0 {
		return nil
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		released, err := ownedProcessGroupReleased(ctx, c.launcher.PID())
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

func profileSocketReleased(profile string) error {
	socket := filepath.Join(profile, "SingletonSocket")
	target, err := os.Readlink(socket)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("profile socket identity unknown: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(profile, target)
	}
	conn, err := net.DialTimeout("unix", target, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return errInteractiveLoginInProgress
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("profile socket release unconfirmed: %w", err)
}
