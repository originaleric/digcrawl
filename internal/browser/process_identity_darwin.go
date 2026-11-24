package browser

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ProcessIdentity binds a PID to its kernel creation time. Display locale,
// process titles and executable replacement do not change that fact.
func ProcessIdentity(ctx context.Context, pid int) (string, error) {
	started, err := ProcessStartedAt(ctx, pid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("process-v2:%d:%d", started.Unix(), started.Nanosecond()), nil
}

func ProcessStartedAt(ctx context.Context, pid int) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if pid <= 0 {
		return time.Time{}, syscall.ESRCH
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.EIO) && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return time.Time{}, syscall.ESRCH
		}
		return time.Time{}, err
	}
	return time.Unix(info.Proc.P_starttime.Sec, int64(info.Proc.P_starttime.Usec)*1000), nil
}
