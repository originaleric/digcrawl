//go:build !windows

package bridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStartWaitsForRecordedBridgeGroupAfterOwnerLockRelease(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	child := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.Process != nil && child.ProcessState == nil {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			_ = child.Wait()
		}
	})
	if pgid, err := bridgeProcessGroup(child.Process.Pid); err != nil || pgid != child.Process.Pid {
		t.Fatalf("test group = (%d, %v), want its own PID %d", pgid, err, child.Process.Pid)
	}
	if err := writeBridgeOwnerRecord(socket, bridgeOwner{pid: child.Process.Pid, pgid: child.Process.Pid}); err != nil {
		t.Fatal(err)
	}

	blockedCtx, cancelBlocked := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelBlocked()
	if err := waitForRecordedBridgeGroupRelease(blockedCtx, socket); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("start gate with active recorded group = %v, want deadline exceeded", err)
	}

	if err := syscall.Kill(-child.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("recorded group helper should be terminated directly")
	}
	readyCtx, cancelReady := context.WithTimeout(context.Background(), time.Second)
	defer cancelReady()
	if err := waitForRecordedBridgeGroupRelease(readyCtx, socket); err != nil {
		t.Fatalf("start gate after recorded group release = %v, want nil", err)
	}
	owner, exists, err := readBridgeOwner(socket)
	if err != nil || !exists || owner.pid != child.Process.Pid || owner.pgid != child.Process.Pid {
		t.Fatalf("read-only start gate changed owner record = (%+v, %v, %v)", owner, exists, err)
	}
}

func TestServeRecoversAfterRecordedBridgeGroupStops(t *testing.T) {
	socketDir, err := os.MkdirTemp("/tmp", "digcrawl-bridge-owner-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "bridge.sock")
	cfg := bridgeTestConfig(t, t.TempDir())
	child := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.Process != nil && child.ProcessState == nil {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			_ = child.Wait()
		}
	})
	if err := writeBridgeOwnerRecord(socket, bridgeOwner{pid: child.Process.Pid, pgid: child.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	blockedCtx, cancelBlocked := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelBlocked()
	if err := Serve(blockedCtx, cfg, socket); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Serve() with active recorded group = %v, want deadline exceeded", err)
	}

	if err := syscall.Kill(-child.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("recorded group helper should be terminated directly")
	}

	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	done := make(chan error, 1)
	go func() { done <- Serve(serveCtx, cfg, socket) }()
	readyCtx, cancelReady := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelReady()
	if _, _, err := waitForBridgeOrExit(readyCtx, socket, done, nil); err != nil {
		t.Fatalf("direct Serve() after recorded group release did not become ready: %v", err)
	}
	owner, exists, err := readBridgeOwner(socket)
	if err != nil || !exists || owner.pid != os.Getpid() || owner.pgid != 0 {
		t.Fatalf("replacement owner record = (%+v, %v, %v), want this test process without a private group", owner, exists, err)
	}
	cancelServe()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() cleanup = %v, want nil", err)
		}
	case <-time.After(startupTimeout):
		t.Fatal("direct Serve() did not stop after cancellation")
	}
}

func TestStopWaitsForRecordedGroupAfterBridgeParentIsGone(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.Process != nil && child.ProcessState == nil {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			_ = child.Wait()
		}
	})
	if err := writeBridgeOwnerRecord(socket, bridgeOwner{pid: child.Process.Pid, pgid: child.Process.Pid}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	stopped, err := Stop(ctx, profile)
	if !errors.Is(err, context.DeadlineExceeded) || stopped {
		t.Fatalf("Stop() with only a recorded live group = (%v, %v), want (false, context deadline)", stopped, err)
	}
	if err := syscall.Kill(-child.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("terminate controlled recorded group: %v", err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("controlled recorded group should be terminated")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped, err = Stop(ctx, profile)
	if err != nil || stopped {
		t.Fatalf("Stop() after recorded group exits = (%v, %v), want (false, nil)", stopped, err)
	}
	if _, exists, err := readBridgeOwner(socket); err != nil || exists {
		t.Fatalf("owner record after recovered Stop = (exists=%v, err=%v), want removed", exists, err)
	}
}
