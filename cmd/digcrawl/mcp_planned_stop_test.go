package main

import (
	"context"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/transport/bridge"
)

func TestMCPPlannedStopsDoNotExhaustFaultRecovery(t *testing.T) {
	profile := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	exe := writeMCPBridgeExecutable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if _, err := bridge.Stop(stopCtx, profile); err != nil {
			t.Errorf("cleanup test Bridge: %v", err)
		}
	})
	for cycle := 1; cycle <= 5; cycle++ {
		if _, err := bridge.NewExecutor(ctx, exe, profile, cfg); err != nil {
			t.Fatalf("normal start/stop cycle %d failed despite zero crashes: %v", cycle, err)
		}
		if _, err := bridge.Stop(ctx, profile); err != nil {
			t.Fatal(err)
		}
	}
}
