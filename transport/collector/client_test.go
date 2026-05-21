package collector

import (
	"testing"
	"time"

	"github.com/originaleric/diggate/config"
)

func TestReserveQueueSlot(t *testing.T) {
	t.Parallel()
	c := &Client{opts: Options{CallQueueSize: 2}}
	if !c.reserveQueueSlot() {
		t.Fatal("expected first slot")
	}
	if !c.reserveQueueSlot() {
		t.Fatal("expected second slot")
	}
	if c.reserveQueueSlot() {
		t.Fatal("expected queue full")
	}
	c.releaseQueueSlot()
	if !c.reserveQueueSlot() {
		t.Fatal("expected slot after release")
	}
}

func TestOptionsFromConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := config.Get()
	opts := OptionsFromConfig(cfg)
	if opts.CallQueueSize != 32 {
		t.Fatalf("queue size: %d", opts.CallQueueSize)
	}
	if opts.RateLimitCooldown != time.Second {
		t.Fatalf("cooldown: %s", opts.RateLimitCooldown)
	}
	if opts.CallQueueWait != 30*time.Second {
		t.Fatalf("wait: %s", opts.CallQueueWait)
	}
}
