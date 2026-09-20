//go:build windows

package bridge

import "os/exec"

// Windows has no Unix-style controlling session to detach from here. The
// bridge still has no stdio pipes and is independently reaped by its idle
// timer.
func detachBridgeProcess(_ *exec.Cmd) {}
