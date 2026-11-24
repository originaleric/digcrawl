//go:build windows

package browser

import "os"

func checkRuntimeOwner(info os.FileInfo) error { return nil }
