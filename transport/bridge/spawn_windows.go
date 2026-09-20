//go:build windows

package bridge

import "os/exec"

// Windows has no Unix-style controlling session to detach from here. The
// bridge still has no stdio pipes; its lifecycle is controlled through the
// private socket rather than an idle timer.
func detachBridgeProcess(_ *exec.Cmd) {}
