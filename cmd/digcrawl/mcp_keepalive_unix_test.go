//go:build !windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/transport/bridge"
)

func TestConcurrentControlIsRejectedBeforeChangingState(t *testing.T) {
	profile := writeMCPPhaseOneProfile(t)
	executable := writeMCPBridgeExecutable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.NewExecutor(ctx, executable, profile, cfg); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		if _, err := bridge.Stop(cleanup, profile); err != nil {
			t.Error(err)
		}
	}()
	commands := t.TempDir()
	seen, release := filepath.Join(commands, "seen"), filepath.Join(commands, "release")
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ ! -f \"$DIGCRAWL_RESTART_SEEN\" ]; then\n touch \"$DIGCRAWL_RESTART_SEEN\"\n while [ ! -f \"$DIGCRAWL_RESTART_RELEASE\" ]; do sleep 0.01; done\nfi\nexec \"$DIGCRAWL_RESTART_PS\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(commands, "ps"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	restart := exec.CommandContext(ctx, executable, "bridge", "restart", "--config", profile)
	restart.Env = append(os.Environ(), "PATH="+commands+string(os.PathListSeparator)+os.Getenv("PATH"), "DIGCRAWL_RESTART_SEEN="+seen, "DIGCRAWL_RESTART_RELEASE="+release, "DIGCRAWL_RESTART_PS="+ps)
	restart.Stdout, restart.Stderr = &output, &output
	if err := restart.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- restart.Wait() }()
	waited := false
	defer func() {
		_ = os.WriteFile(release, nil, 0600)
		if !waited {
			_ = restart.Process.Kill()
			<-done
		}
	}()
	for {
		if _, err := os.Stat(seen); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("restart did not reach the ownership observation")
		case <-time.After(10 * time.Millisecond):
		}
	}
	stop := exec.CommandContext(ctx, executable, "bridge", "stop", "--config", profile)
	if output, err := stop.CombinedOutput(); err == nil || !strings.Contains(string(output), "maintenance") {
		t.Fatalf("concurrent stop was not rejected: %v: %s", err, output)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	err = <-done
	waited = true
	if err != nil {
		t.Fatalf("accepted restart failed: %v: %s", err, output.String())
	}
	stop = exec.CommandContext(ctx, executable, "bridge", "stop", "--config", profile)
	if output, err := stop.CombinedOutput(); err != nil {
		t.Fatalf("serialized retry: %v: %s", err, output)
	}
	state, err := bridge.Inspect(ctx, profile)
	if err != nil || !state.Paused || state.Ready || len(state.Owners) != 0 {
		t.Fatalf("completed stop undone: %+v (%v)", state, err)
	}
}

func TestMCPWatchRecoversUnlinkedSocketAndSurvivesCacheCleanup(t *testing.T) {
	profile := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	exe := writeMCPBridgeExecutable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if _, err := bridge.Stop(stop, profile); err != nil {
			t.Error(err)
		}
	})
	executor, err := bridge.NewExecutor(ctx, exe, profile, cfg)
	if err != nil {
		t.Fatal(err)
	}
	socket, _ := bridge.SocketPath(profile)
	oldPID := waitForMCPTestBridgePID(t, socket)
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	testHome := os.Getenv("DIGCRAWL_TEST_HOME")
	if !strings.HasPrefix(filepath.Base(testHome), "dck-test-") || !strings.HasPrefix(cache, testHome+string(os.PathSeparator)) {
		t.Fatal("cache cleanup requires the isolated test HOME")
	}
	// TestMain supplies an isolated HOME. Clearing the test cache reproduces the
	// original incident while retaining the stable transport and its owner.
	if err := os.RemoveAll(filepath.Join(cache, "digcrawl")); err != nil {
		t.Fatal(err)
	}
	result := executor.Execute(ctx, &protocol.ToolCall{ID: "after-cleanup", Tool: "gateway.ping", Input: []byte(`{}`)})
	if result.Status != "success" || waitForMCPTestBridgePID(t, socket) != oldPID {
		t.Fatalf("cache cleanup interrupted Bridge: %+v", result)
	}
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	watchCtx, stopWatch := context.WithCancel(ctx)
	done := make(chan error, 1)
	alias := filepath.Join(t.TempDir(), "digcrawl-alias")
	if err := os.Symlink(exe, alias); err != nil {
		t.Fatal(err)
	}
	go func() { done <- bridge.Watch(watchCtx, alias, profile, cfg) }()
	defer func() { stopWatch(); <-done }()
	started := time.Now()
	for {
		time.Sleep(200 * time.Millisecond)
		status, err := bridge.Inspect(ctx, profile)
		if err == nil && status.Ready {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("watch recovery: %v", ctx.Err())
		}
	}
	if time.Since(started) > 60*time.Second {
		t.Fatal("recovery missed 90-second target")
	}
	if pid := waitForMCPTestBridgePID(t, socket); pid == oldPID {
		t.Fatal("old owner was not replaced")
	}
	if err := syscall.Kill(oldPID, 0); err == nil {
		t.Fatal("old owner remains alive")
	}
	if err := bridge.Pause(profile, true); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Stop(ctx, profile); err != nil {
		t.Fatal(err)
	}
	// Prove paused watcher does not resurrect the explicitly stopped owner.
	time.Sleep(31 * time.Second)
	status, err := bridge.Inspect(ctx, profile)
	if err != nil || status.Ready || !status.Paused {
		t.Fatalf("paused watcher: %+v %v", status, err)
	}
}

func TestMCPMigrationStopsLegacyOwnerBeforeStableStart(t *testing.T) {
	profile := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	exe := writeMCPBridgeExecutable(t)
	stable, _ := bridge.SocketPath(profile)
	cache, _ := os.UserCacheDir()
	legacy := filepath.Join(cache, "digcrawl", "bridge", filepath.Base(stable))
	// Launch the existing worker entry with a legacy path to exercise migration.
	worker := exec.Command(exe, "bridge", "serve", "--managed-worker", "--config", profile, "--socket", legacy)
	worker.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Process.Kill(); _ = worker.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(legacy); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("legacy fixture did not listen")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := bridge.Stop(ctx, profile); err != nil {
		t.Fatal(err)
	}
	_ = worker.Wait()
	if err := syscall.Kill(worker.Process.Pid, 0); err == nil {
		t.Fatal("legacy owner survived migration")
	}
	executor, err := bridge.NewExecutor(ctx, exe, profile, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = bridge.Stop(context.Background(), profile) }()
	result := executor.Execute(ctx, &protocol.ToolCall{ID: "after-migration", Tool: "gateway.ping", Input: []byte(`{}`)})
	if result.Status != "success" {
		t.Fatalf("migration call: %+v", result)
	}
	canonical, err := filepath.EvalSymlinks(profile)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(canonical))
	if filepath.Base(stable) != fmt.Sprintf("%x.sock", digest[:12]) {
		t.Fatal("profile identity changed")
	}
}

func TestMCPFirstStartPreservesLegacyBudget(t *testing.T) {
	for _, attempts := range []int{1, 3} {
		t.Run(strconv.Itoa(attempts), func(t *testing.T) {
			profile := writeMCPPhaseOneProfile(t)
			cfg, err := config.Load(profile)
			if err != nil {
				t.Fatal(err)
			}
			exe := writeMCPBridgeExecutable(t)
			socket, err := bridge.SocketPath(profile)
			if err != nil {
				t.Fatal(err)
			}
			cache, _ := os.UserCacheDir()
			legacy := filepath.Join(cache, "digcrawl", "bridge", filepath.Base(socket))
			if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
				t.Fatal(err)
			}
			record := map[string]any{"version": 2, "unclean": true, "attempts": attempts, "last_attempt": time.Now().UTC()}
			raw, _ := json.Marshal(record)
			if err := os.WriteFile(legacy+".recovery.json", raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			_, startErr := bridge.NewExecutor(ctx, exe, profile, cfg)
			want := attempts
			if attempts >= 3 {
				if !errors.Is(startErr, bridge.ErrRecoveryExhausted) {
					t.Fatalf("legacy budget bypassed: %v", startErr)
				}
			} else {
				if startErr != nil {
					t.Fatal(startErr)
				}
				want++
				t.Cleanup(func() {
					stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if _, err := bridge.Stop(stop, profile); err != nil {
						t.Error(err)
					}
				})
			}
			data, err := os.ReadFile(socket + ".recovery.json")
			if err != nil {
				t.Fatal(err)
			}
			var migrated struct {
				Attempts int  `json:"attempts"`
				Unclean  bool `json:"unclean"`
			}
			if err := json.Unmarshal(data, &migrated); err != nil || migrated.Attempts != want || !migrated.Unclean {
				t.Fatalf("migration=%s, err=%v", data, err)
			}
		})
	}
}

func TestMCPFirstStartReclaimsRecordedOrphanBrowser(t *testing.T) {
	profile := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	exe := writeMCPBridgeExecutable(t)
	socket, err := bridge.SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("/bin/sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	identityCmd := exec.Command("ps", "-p", strconv.Itoa(child.Process.Pid), "-o", "lstart=,comm=")
	identityCmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	output, err := identityCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	identity := sha256.Sum256(output)
	raw := fmt.Sprintf("99999999 0 %064x\n%d %d %x\n", 1, child.Process.Pid, child.Process.Pid, identity)
	if err := os.WriteFile(socket+".owner", []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	executor, err := bridge.NewExecutor(ctx, exe, profile, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = bridge.Stop(stop, profile)
	}()
	_ = child.Wait()
	if err := syscall.Kill(child.Process.Pid, 0); err == nil {
		t.Fatal("orphan browser survived")
	}
	result := executor.Execute(ctx, &protocol.ToolCall{ID: "after-orphan", Tool: "gateway.ping", Input: []byte(`{}`)})
	if result.Status != "success" {
		t.Fatalf("MCP did not recover: %+v", result)
	}
}
