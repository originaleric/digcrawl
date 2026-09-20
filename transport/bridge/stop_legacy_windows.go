//go:build windows

package bridge

import (
	"context"
	"fmt"
)

// Pre-control-endpoint Windows Bridges need one manual stop during migration;
// current releases use the private socket endpoint and never reach this path.
func stopLegacyBridge(context.Context, string) (bool, error) {
	return false, fmt.Errorf("%w; update once after stopping the old Bridge", ErrBridgeControlUnsupported)
}
