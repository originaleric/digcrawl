//go:build !windows

package bridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLegacyBridgePIDRequiresExactSocketArgument(t *testing.T) {
	socket := "/tmp/digcrawl/current.sock"
	processes := `  120 /usr/local/bin/digcrawl bridge serve --config /tmp/profile --socket /tmp/digcrawl/other.sock
  121 /usr/local/bin/digcrawl bridge serve --config /tmp/profile --socket /tmp/digcrawl/current.sock
  122 /usr/local/bin/digcrawl gateway --socket /tmp/digcrawl/current.sock
  123 /usr/local/bin/digcrawl bridge serve --config /tmp/profile --socket /tmp/digcrawl/current.sock-old
`
	if got := legacyBridgePID(processes, socket); got != 121 {
		t.Fatalf("legacyBridgePID=%d, want 121", got)
	}
}

func TestStopMigratesUnresponsiveLegacyBridgeWithExactSocket(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLegacyBridgeStopHelper$", "--", "bridge", "serve", "--config", profile, "--socket", socket)
	cmd.Env = append(os.Environ(),
		"DIGCRAWL_LEGACY_BRIDGE_HELPER=1",
		"DIGCRAWL_LEGACY_BRIDGE_SOCKET="+socket,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		lock, lockErr := acquireDaemonLock(socket + ".lock")
		if errors.Is(lockErr, ErrAlreadyRunning) {
			break
		}
		if lockErr != nil {
			t.Fatalf("check legacy bridge lifecycle lock: %v", lockErr)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	lock, lockErr := acquireDaemonLock(socket + ".lock")
	if lockErr == nil {
		_ = lock.Close()
		t.Fatal("legacy bridge helper did not acquire its lifecycle lock")
	}
	if !errors.Is(lockErr, ErrAlreadyRunning) {
		t.Fatalf("legacy bridge lifecycle lock: %v", lockErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopped, err := Stop(ctx, profile)
	if err != nil || !stopped {
		t.Fatalf("Stop() = (%v, %v), want (true, nil)", stopped, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("legacy bridge helper: %v", err)
	}
}

func TestLegacyBridgeStopHelper(t *testing.T) {
	if os.Getenv("DIGCRAWL_LEGACY_BRIDGE_HELPER") != "1" {
		return
	}
	lock, err := acquireDaemonLock(os.Getenv("DIGCRAWL_LEGACY_BRIDGE_SOCKET") + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case <-signals:
	case <-time.After(5 * time.Second):
		t.Fatal("legacy bridge helper was not stopped")
	}
}
