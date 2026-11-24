package browser

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestProcessIdentityIgnoresDisplayEnvironment(t *testing.T) {
	t.Setenv("TZ", "UTC")
	t.Setenv("LC_ALL", "C")
	ctx := context.Background()
	first, err := ProcessIdentity(ctx, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TZ", "America/New_York")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	second, err := ProcessIdentity(ctx, os.Getpid())
	if err != nil || second != first {
		t.Fatalf("identity changed: %q -> %q (%v)", first, second, err)
	}
	started, err := ProcessStartedAt(ctx, os.Getpid())
	if err != nil || started.IsZero() || started.After(time.Now()) {
		t.Fatalf("invalid creation time: %v (%v)", started, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ProcessIdentity(canceled, os.Getpid()); err == nil {
		t.Fatal("canceled inspection succeeded")
	}
}
