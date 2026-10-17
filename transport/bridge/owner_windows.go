//go:build windows

package bridge

import "context"

// Windows uses taskkill's native process-tree termination; it does not use a
// Unix owner-group record.
func writeBridgeOwner(string) error { return nil }
func removeBridgeOwner(string)      {}
func waitForRecordedBridgeGroupRelease(context.Context, string) error {
	return nil
}
