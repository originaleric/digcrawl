//go:build !windows

package bridge

import (
	"os/exec"
	"syscall"
)

// detachBridgeProcess gives the on-demand daemon its own session. A stdio MCP
// proxy is allowed to exit as soon as its host closes the pipe; terminal and
// host process-group cleanup must not kill the shared bridge with it.
func detachBridgeProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
