//go:build !windows

package bridge

import (
	"context"
	"errors"
	"io"
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
	cmd := exec.Command(bridgeStopFixture(t), "bridge", "serve", "--config", profile, "--socket", socket)
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

	deadline := time.Now().Add(5 * time.Second)
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
	cmd := exec.Command(bridgeStopFixture(t), "bridge", "serve", "--config", profile, "--socket", socket)
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
	deadline := time.Now().Add(5 * time.Second)
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
	childDeadline := time.Now().Add(5 * time.Second)
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
	holder := exec.Command(bridgeStopFixture(t), "bridge", "serve", "--config", profile, "--socket", socket)
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
	deadline := time.Now().Add(5 * time.Second)
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

func TestMain(m *testing.M) {
	helper := ""
	if os.Getenv("DIGCRAWL_LEGACY_BRIDGE_HELPER") == "1" {
		helper = "TestLegacyBridgeStopHelper"
	}
	if os.Getenv("DIGCRAWL_BRIDGE_GROUP_HELPER") == "1" {
		helper = "TestBridgeProcessGroupStopHelper"
	}
	if helper != "" {
		os.Args = []string{os.Args[0], "-test.run=^" + helper + "$"}
	}
	os.Exit(m.Run())
}
func bridgeStopFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "digcrawl")
	src, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return path
}

func TestMigrationDoesNotSignalInterpreterWithWorkerText(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python fixture unavailable")
	}
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	cmd := exec.Command(python, "-c", "import time;time.sleep(30)", "/tmp/digcrawl", "bridge", "serve", "--socket", socket)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped, err := stopLegacyBridgeCandidates(ctx, socket)
	if err != nil || stopped {
		t.Fatalf("interpreter was selected: stopped=%v err=%v", stopped, err)
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("unrelated interpreter terminated:", err)
	}
}

func TestStartRejectsLivePredecessorAfterLockFileDeletion(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bridgeStopFixture(t), "bridge", "serve", "--config", profile, "--socket", socket)
	cmd.Env = append(os.Environ(), "DIGCRAWL_LEGACY_BRIDGE_HELPER=1", "DIGCRAWL_LEGACY_BRIDGE_SOCKET="+socket)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(2 * time.Second)
	acquired := false
	for time.Now().Before(deadline) {
		l, e := acquireDaemonLock(socket + ".lock")
		if errors.Is(e, ErrAlreadyRunning) {
			acquired = true
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		_ = l.Close()
		time.Sleep(10 * time.Millisecond)
	}
	if !acquired {
		t.Fatal("predecessor failed to acquire lock")
	}
	if err = os.Remove(socket + ".lock"); err != nil {
		t.Fatal(err)
	}
	// A replacement can now claim a different inode; the process ownership gate
	// must still prevent it from publishing a socket or acquiring a browser.
	l, err := acquireDaemonLock(socket + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err = waitForPriorBridgeOwners(ctx, socket); err == nil {
		t.Fatal("live prior worker bypassed ownership gate")
	}
	if err = cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("read-only startup gate terminated predecessor")
	}
}

func TestOwnerGateDoesNotTreatCancellationAsRelease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForPriorBridgeOwners(ctx, filepath.Join(t.TempDir(), "absent.sock")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection falsely succeeded: %v", err)
	}
}
func TestBridgeIdentitySupportsRenamedNativeCLI(t *testing.T) {
	original := bridgeStopFixture(t)
	renamed := filepath.Join(filepath.Dir(original), "custom-agent-browser")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if !isBridgeExecutable(renamed) {
		t.Fatal("renamed native DigCrawl test worker rejected")
	}
	if isBridgeExecutable("/bin/sh") {
		t.Fatal("shell accepted as DigCrawl executable")
	}
}

func TestOwnerVerificationAcceptsExecutableSymlink(t *testing.T) {
	actual := bridgeStopFixture(t)
	alias := filepath.Join(t.TempDir(), "project - worker-link")
	if err := os.Symlink(actual, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	cmd := exec.Command(alias, "bridge", "serve", "--socket", socket)
	cmd.Env = append(os.Environ(), "DIGCRAWL_LEGACY_BRIDGE_HELPER=1", "DIGCRAWL_LEGACY_BRIDGE_SOCKET="+socket)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := verifiedBridgeProcess(ctx, cmd.Process.Pid, socket)
		if err == nil && info.Verified {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("symlink-launched native worker could not prove executable identity")
}

func TestRecordedOwnerSurvivesExecutableUnlinkDuringUpgrade(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	executable := bridgeStopFixture(t)
	cmd := exec.Command(executable, "bridge", "serve", "--config", profile, "--socket", socket)
	detachBridgeProcess(cmd)
	cmd.Env = append(os.Environ(), "DIGCRAWL_BRIDGE_GROUP_HELPER=1", "DIGCRAWL_BRIDGE_GROUP_SOCKET="+socket, "DIGCRAWL_BRIDGE_GROUP_CHILD_PID_FILE="+filepath.Join(dir, "child"))
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "child")); e == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	identity, err := bridgeProcessIdentity(ctx, cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	owner := bridgeOwner{pid: cmd.Process.Pid, pgid: cmd.Process.Pid, identity: identity}
	if err = writeBridgeOwnerRecord(socket, owner); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	current, err := recordedBridgeOwnerIsCurrent(ctx, socket, owner)
	if err != nil || !current {
		t.Fatalf("retained owner identity lost after unlink: %v %v", current, err)
	}
	stopped, err := Stop(ctx, profile)
	if err != nil || !stopped {
		t.Fatalf("upgrade could not retire recorded owner: %v %v", stopped, err)
	}
}

func TestReviewNativeWorkerInDashSpacePath(t *testing.T) {
	socket := "/tmp/review.sock"
	command := "234 /tmp/Projects - Copy/digcrawl bridge serve --managed-worker --config /tmp/profile.yaml --socket " + socket
	if got := bridgePID(command, socket); got != 234 {
		t.Fatalf("legitimate native worker in a path containing space-dash was rejected: got PID %d", got)
	}
}

func TestMigrationDoesNotSignalShellWithWorkerText(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("shell unavailable")
	}
	socket := filepath.Join(t.TempDir(), "bridge.sock")
	cmd := exec.Command(shell, "-c", "sleep 30", "/tmp/digcrawl", "bridge", "serve", "--socket", socket)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stopped, err := stopLegacyBridgeCandidates(ctx, socket)
	if err != nil && !errors.Is(err, ErrBridgeOwnerNotFound) {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("shell selected as native owner")
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("shell was terminated", err)
	}
}
