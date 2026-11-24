//go:build !windows

package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestProcessIdentityRecordValidation(t *testing.T) {
	for _, token := range []string{"process-v2:", "process-v2:garbage", "process-v2:0:1", "process-v2:123:-1", "process-v2:123:1000000000", "short"} {
		if validProcessIdentity(token) {
			t.Fatalf("invalid token accepted: %q", token)
		}
	}
	for _, token := range []string{"process-v2:123:0", "process-v2:123:999999999", fmt.Sprintf("%064x", 1), "process-v2:" + fmt.Sprintf("%064x", 1)} {
		if !validProcessIdentity(token) {
			t.Fatalf("valid token rejected: %q", token)
		}
	}
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	if err := os.WriteFile(bridgeOwnerPath(socket), []byte("123 123 process-v2:garbage\n456 456 process-v2:123:0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBridgeOwner(socket); err == nil {
		t.Fatal("invalid parent identity could authorize browser recovery")
	}
}

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
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		t.Fatal(err)
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
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		t.Fatal(err)
	}
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
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		t.Fatal(err)
	}
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

func TestRecoversRecordedBrowserAfterBridgeExit(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	child := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	identity, err := browserProcessIdentity(context.Background(), child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	// An exited predecessor with a still-live, separately grouped browser.
	owner := bridgeOwner{pid: 99999999, identity: fmt.Sprintf("%064x", 1), browsers: []recordedBrowserProcess{{pid: child.Process.Pid, pgid: child.Process.Pid, identity: identity}}}
	if err := writeBridgeOwnerRecord(socket, owner); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitForRecordedBridgeGroupRelease(ctx, socket); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if err := syscall.Kill(child.Process.Pid, 0); err == nil {
		t.Fatal("recorded browser survived")
	}
}

func TestStaleBrowserIdentityCannotSignalReusedProcess(t *testing.T) {
	child := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	owner := bridgeOwner{pid: 99999999, identity: fmt.Sprintf("%064x", 1), browsers: []recordedBrowserProcess{{pid: child.Process.Pid, pgid: child.Process.Pid, identity: "different-start"}}}
	if err := recoverRecordedBrowser(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(child.Process.Pid, 0); err != nil {
		t.Fatal("unrelated reused PID was signalled")
	}
}

func TestBrowserCannotBeReclaimedFromLivePredecessor(t *testing.T) {
	parent := exec.Command("/bin/sleep", "30")
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Process.Kill(); _ = parent.Wait() }()
	identity, err := bridgeProcessIdentity(context.Background(), parent.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	owner := bridgeOwner{pid: parent.Process.Pid, identity: identity, browsers: []recordedBrowserProcess{{pid: parent.Process.Pid, pgid: parent.Process.Pid, identity: identity}}}
	if err := recoverRecordedBrowser(context.Background(), owner); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("live predecessor: %v", err)
	}
}

func TestStopReclaimsRecordedBrowserBeforeLegacyProfileBarrier(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	base := filepath.Join(dir, "browser")
	if err := os.WriteFile(profile, []byte("LocalBrowser:\n  UserDataDir: "+base+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(child)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	identity, err := browserProcessIdentity(context.Background(), child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(base, "digcrawl-isolated")
	if err := os.MkdirAll(isolated, 0700); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fmt.Sprintf("%s-%d", host, child.Process.Pid), filepath.Join(isolated, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if err := writeBridgeOwnerRecord(socket, bridgeOwner{pid: 99999999, identity: fmt.Sprintf("%064x", 1), browsers: []recordedBrowserProcess{{pid: child.Process.Pid, pgid: child.Process.Pid, identity: identity}}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Stop(ctx, profile); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if err := syscall.Kill(child.Process.Pid, 0); err == nil {
		t.Fatal("orphan browser survived controlled stop")
	}
}

func TestMultipleRecordedBrowsersRetainIdentityAcrossPartialCleanup(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	owner := bridgeOwner{pid: 99999999, identity: fmt.Sprintf("%064x", 1)}
	children := make([]*exec.Cmd, 0, 2)
	for range 2 {
		child := exec.Command("/bin/sleep", "30")
		detachBridgeProcess(child)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
		t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
		identity, err := browserProcessIdentity(context.Background(), child.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		owner.browsers = append(owner.browsers, recordedBrowserProcess{pid: child.Process.Pid, pgid: child.Process.Pid, identity: identity})
	}
	if err := writeBridgeOwnerRecord(socket, owner); err != nil {
		t.Fatal(err)
	}
	originalSignal := signalBridgeProcess
	signalBridgeProcess = func(pid int, sig syscall.Signal) error {
		if pid == -children[1].Process.Pid {
			return syscall.EPERM
		}
		return originalSignal(pid, sig)
	}
	defer func() { signalBridgeProcess = originalSignal }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := waitForRecordedBridgeGroupRelease(ctx, socket); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("partial cleanup error=%v", err)
	}
	stored, exists, err := readBridgeOwner(socket)
	if err != nil || !exists || len(stored.browsers) != 2 {
		t.Fatalf("partial cleanup lost resource identities: %+v %v", stored, err)
	}
	if err := syscall.Kill(children[1].Process.Pid, 0); err != nil {
		t.Fatal("unexpected second browser termination")
	}
	signalBridgeProcess = originalSignal
	if err := waitForRecordedBridgeGroupRelease(ctx, socket); err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		_ = child.Wait()
		if err := syscall.Kill(child.Process.Pid, 0); err == nil {
			t.Fatal("browser survived retry")
		}
	}
}
