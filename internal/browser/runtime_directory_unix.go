//go:build !windows

package browser

import (
	"fmt"
	"os"
	"syscall"
)

func checkRuntimeOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("Bridge runtime directory belongs to another user")
	}
	return nil
}
