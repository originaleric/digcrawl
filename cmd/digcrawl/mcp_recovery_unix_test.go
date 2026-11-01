//go:build !windows

package main

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/transport/bridge"
)

func TestMCPRecoversCrashedBridgeWithPersistentFiniteBudget(t *testing.T) {
	profile := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	exe := writeMCPBridgeExecutable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if _, e := bridge.Stop(stop, profile); e != nil {
			t.Error(e)
		}
	})
	e, err := bridge.NewExecutor(ctx, exe, profile, cfg)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := bridge.SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		pid := waitForMCPTestBridgePID(t, socket)
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		// Wait for the original process to disappear before probing the old socket.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if syscall.Kill(pid, 0) != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		result := e.Execute(ctx, &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "after-crash", Tool: "gateway.ping", Input: []byte(`{}`)})
		if attempt < 3 {
			if result.Status != "success" {
				t.Fatalf("recovery %d: %+v", attempt, result.Error)
			}
			// Reconnect a new proxy: its construction must not reset the profile budget.
			e, err = bridge.NewExecutor(ctx, exe, profile, cfg)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if result.Error == nil || result.Error.Details["reason"] != "recovery_exhausted" {
				t.Fatalf("missing finite budget: %+v; error=%+v", result, result.Error)
			}
		}
	}
	// An explicit stop of an already-absent crashed worker must not wash away
	// its dirty exit or let a fresh proxy bypass the exhausted fault allowance.
	if _, err := bridge.Stop(ctx, profile); err != nil {
		t.Fatal(err)
	}
	exhausted := e.Execute(ctx, &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "after-absent-stop", Tool: "gateway.ping", Input: []byte(`{}`)})
	if exhausted.Error == nil || exhausted.Error.Details["reason"] != "recovery_exhausted" {
		t.Fatalf("absent stop reset budget: %+v", exhausted)
	}
	// Explicit operator restart is one attempt, not an automatic budget refund.
	if _, err = bridge.NewExecutorAfterRestart(ctx, exe, profile, cfg); err != nil {
		t.Fatal(err)
	}
}
