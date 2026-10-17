//go:build windows

package bridge

// Windows taskkill owns the process-tree containment boundary.
func ValidateManagedWorkerIsolation() error { return nil }
