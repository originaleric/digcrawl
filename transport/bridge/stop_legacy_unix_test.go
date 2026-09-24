//go:build !windows

package bridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBridgePIDRequiresExactSocketArgument(t *testing.T) {
	socket := "/tmp/digcrawl/current.sock"
	processes := `  120 /usr/local/bin/digcrawl bridge serve --config /tmp/profile --socket /tmp/digcrawl/other.sock
  121 /usr/local/bin/digcrawl bridge serve --config /tmp/profile --socket /tmp/digcrawl/current.sock
  122 /usr/local/bin/digcrawl gateway --socket /tmp/digcrawl/current.sock
  123 /usr/local/bin/digcrawl bridge serve --config /tmp/profile --socket /tmp/digcrawl/current.sock-old
`
	if got := bridgePID(processes, socket); got != 121 {
		t.Fatalf("bridgePID=%d, want 121", got)
	}
}

func TestStopKillsExactLegacyBridgePID(t *testing.T) {
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
	if err := cmd.Wait(); err == nil {
		t.Fatal("legacy bridge helper should be terminated directly")
	}
}

func TestStopKillsExactBridgeProcessGroupAndChild(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	childPIDFile := filepath.Join(dir, "chrome-child.pid")
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcessGroupStopHelper$", "--", "bridge", "serve", "--config", profile, "--socket", socket)
	detachBridgeProcess(cmd)
	cmd.Env = append(os.Environ(),
		"DIGCRAWL_BRIDGE_GROUP_HELPER=1",
		"DIGCRAWL_BRIDGE_GROUP_SOCKET="+socket,
		"DIGCRAWL_BRIDGE_GROUP_CHILD_PID_FILE="+childPIDFile,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
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
			t.Fatalf("check force-stop bridge lifecycle lock: %v", lockErr)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	lock, lockErr := acquireDaemonLock(socket + ".lock")
	if lockErr == nil {
		_ = lock.Close()
		t.Fatal("Bridge group helper did not acquire its lifecycle lock")
	}
	if !errors.Is(lockErr, ErrAlreadyRunning) {
		t.Fatalf("Bridge group lifecycle lock: %v", lockErr)
	}

	var childPID int
	childDeadline := time.Now().Add(time.Second)
	for time.Now().Before(childDeadline) {
		data, err := os.ReadFile(childPIDFile)
		if err == nil {
			childPID, err = strconv.Atoi(string(data))
			if err == nil && childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID <= 0 {
		t.Fatal("Bridge group helper did not report a child PID")
	}
	if pgid, err := bridgeProcessGroup(cmd.Process.Pid); err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("Bridge helper process group = (%d, %v), want its own PID %d", pgid, err, cmd.Process.Pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopped, err := Stop(ctx, profile)
	if err != nil || !stopped {
		t.Fatalf("Stop() = (%v, %v), want (true, nil)", stopped, err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("Bridge group helper should be terminated directly")
	}
	if released, err := bridgeGroupReleased(cmd.Process.Pid); err != nil || !released {
		t.Fatalf("Bridge process group still exists after Stop: released=%v err=%v", released, err)
	}
	if live, err := processLive(childPID); err != nil || live {
		t.Fatalf("Bridge-owned child process still runs after Stop: live=%v err=%v", live, err)
	}
}

func TestStopDoesNotSignalStaleOwnerRecord(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	stale := exec.Command("/bin/sleep", "30")
	detachBridgeProcess(stale)
	if err := stale.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if stale.Process != nil && stale.ProcessState == nil {
			_ = syscall.Kill(-stale.Process.Pid, syscall.SIGKILL)
			_ = stale.Wait()
		}
	})
	if err := writeBridgeOwnerRecord(socket, bridgeOwner{pid: stale.Process.Pid, pgid: stale.Process.Pid}); err != nil {
		t.Fatal(err)
	}

	// A live lock simulates the small setup window of a new worker. Its owner
	// record is deliberately stale, so Stop must not treat the sleep group as
	// the current Bridge merely because it has matching numeric IDs.
	holder := exec.Command(os.Args[0], "-test.run=^TestLegacyBridgeStopHelper$", "--", "bridge", "serve", "--config", profile, "--socket", socket)
	holder.Env = append(os.Environ(),
		"DIGCRAWL_LEGACY_BRIDGE_HELPER=1",
		"DIGCRAWL_LEGACY_BRIDGE_SOCKET="+socket,
	)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if holder.Process != nil && holder.ProcessState == nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		lock, lockErr := acquireDaemonLock(socket + ".lock")
		if errors.Is(lockErr, ErrAlreadyRunning) {
			break
		}
		if lockErr != nil {
			t.Fatalf("check stale-record lock holder: %v", lockErr)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	lock, lockErr := acquireDaemonLock(socket + ".lock")
	if lockErr == nil {
		_ = lock.Close()
		t.Fatal("stale-record lock holder did not acquire its lifecycle lock")
	}
	if !errors.Is(lockErr, ErrAlreadyRunning) {
		t.Fatalf("stale-record lifecycle lock: %v", lockErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	stopped, err := Stop(ctx, profile)
	if stopped || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() with stale owner record = (%v, %v), want (false, deadline exceeded)", stopped, err)
	}
	if live, err := processLive(stale.Process.Pid); err != nil || !live {
		t.Fatalf("stale recorded process was signalled: live=%v err=%v", live, err)
	}
}

func processLive(pid int) (bool, error) {
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(output)) != "" && !strings.HasPrefix(strings.TrimSpace(string(output)), "Z"), nil
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
	select {}
}

func TestBridgeProcessGroupStopHelper(t *testing.T) {
	if os.Getenv("DIGCRAWL_BRIDGE_GROUP_HELPER") != "1" {
		return
	}
	lock, err := acquireDaemonLock(os.Getenv("DIGCRAWL_BRIDGE_GROUP_SOCKET") + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := writeBridgeOwnerRecord(os.Getenv("DIGCRAWL_BRIDGE_GROUP_SOCKET"), bridgeOwner{pid: os.Getpid(), pgid: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("DIGCRAWL_BRIDGE_GROUP_CHILD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	select {}
}
