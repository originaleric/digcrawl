package asr

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalASRCacheLockDeadline(t *testing.T) {
	canonical := t.TempDir()
	unlock, err := lockLocalASRCache(t.Context(), canonical)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	other, err := lockLocalASRCache(ctx, canonical)
	if other != nil {
		other()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended lock error=%v, want deadline exceeded", err)
	}
}

func TestLocalASRCacheConcurrentProcessesConverge(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "models")
	gate := filepath.Join(root, "start")
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	var results []string
	for i := range 4 {
		legacy := filepath.Join(root, string(rune('a'+i)))
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "model.bin"), []byte("preserved model"), 0o600); err != nil {
			t.Fatal(err)
		}
		result := filepath.Join(root, string(rune('a'+i))+".result")
		cmd, output := localASRCacheProcess(t, "resolve", canonical, legacy, gate, result)
		commands = append(commands, cmd)
		outputs = append(outputs, output)
		results = append(results, result)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var winner string
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("cache process %d: %v\n%s", i, err, outputs[i])
		}
		data, err := os.ReadFile(results[i])
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			winner = string(data)
		}
		if string(data) != winner {
			t.Fatalf("processes selected different caches: %q vs %q", winner, data)
		}
	}
	state, ok := readLocalASRCacheState(canonical)
	if !ok || state.ActiveModelCacheDir != winner {
		t.Fatalf("persisted state=%+v, want winner %q", state, winner)
	}
	for i := range 4 {
		if _, err := os.Stat(filepath.Join(root, string(rune('a'+i)), "model.bin")); err != nil {
			t.Fatalf("legacy model must remain in place: %v", err)
		}
	}
}

func TestLocalASRCacheLockReleasedAfterProcessExit(t *testing.T) {
	canonical := t.TempDir()
	ready := filepath.Join(canonical, "ready")
	cmd, output := localASRCacheProcess(t, "hold", canonical, "", "", ready)
	if err := waitForASRCacheTestFile(t.Context(), ready); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("cache lock owner did not start: %v\n%s", err, output)
	}
	blocked, stopWaiting := context.WithTimeout(t.Context(), 100*time.Millisecond)
	other, err := lockLocalASRCache(blocked, canonical)
	stopWaiting()
	if other != nil {
		other()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("separate process did not exclude cache access: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	unlock, err := lockLocalASRCache(ctx, canonical)
	if err != nil {
		t.Fatalf("process exit left cache lock held: %v", err)
	}
	unlock()
}

func TestLocalASRCacheSelectionWaitsForStateOwner(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "models")
	legacy := filepath.Join(root, "legacy")
	for _, directory := range []string{canonical, legacy} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "model.bin"), []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockLocalASRCache(t.Context(), canonical)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	finished := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := resolveLocalASRCacheDir(canonical, []string{legacy})
		finished <- err
	}()
	<-started
	select {
	case err := <-finished:
		t.Fatalf("cache selection bypassed the state owner: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	unlock = nil
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cache selection did not resume after unlock")
	}
}

func TestLocalASRCacheWriteFailurePreservesDataAndReleasesLock(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "models")
	legacy := filepath.Join(root, "legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "model.bin"), []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An obstructed state path is a real I/O failure, never a successful adoption.
	statePath := filepath.Join(canonical, localASRCacheStateName)
	if err := os.MkdirAll(statePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveLocalASRCacheDir(canonical, []string{legacy}); err == nil {
		t.Fatal("expected state write failure")
	}
	if _, err := os.Stat(filepath.Join(legacy, "model.bin")); err != nil {
		t.Fatalf("model was lost after state write failure: %v", err)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	got, err := resolveLocalASRCacheDir(canonical, []string{legacy})
	if err != nil || got != legacy {
		t.Fatalf("retry after failed write: cache=%q error=%v", got, err)
	}
}

func localASRCacheProcess(t *testing.T, mode, canonical, legacy, gate, result string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalASRCacheProcessHelper$")
	cmd.Env = append(os.Environ(), "DIGCRAWL_ASR_CACHE_TEST_MODE="+mode, "DIGCRAWL_ASR_CACHE_TEST_DIR="+canonical,
		"DIGCRAWL_ASR_CACHE_TEST_LEGACY="+legacy, "DIGCRAWL_ASR_CACHE_TEST_GATE="+gate, "DIGCRAWL_ASR_CACHE_TEST_RESULT="+result)
	output := new(bytes.Buffer)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, output
}

func waitForASRCacheTestFile(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestLocalASRCacheProcessHelper(t *testing.T) {
	mode := os.Getenv("DIGCRAWL_ASR_CACHE_TEST_MODE")
	if mode == "" {
		return
	}
	canonical := os.Getenv("DIGCRAWL_ASR_CACHE_TEST_DIR")
	result := os.Getenv("DIGCRAWL_ASR_CACHE_TEST_RESULT")
	if mode == "hold" {
		unlock, err := lockLocalASRCache(t.Context(), canonical)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		if err := os.WriteFile(result, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		// This binary is invoked directly, without go test's default timeout.
		// Keep a timer active so the runtime does not diagnose the intentionally
		// blocked lock owner as a deadlock before its parent terminates it.
		select {
		case <-t.Context().Done():
		case <-time.After(30 * time.Second):
			t.Fatal("lock owner was not terminated by its parent")
		}
		return
	}
	if err := waitForASRCacheTestFile(t.Context(), os.Getenv("DIGCRAWL_ASR_CACHE_TEST_GATE")); err != nil {
		t.Fatal(err)
	}
	cache, err := resolveLocalASRCacheDir(canonical, []string{os.Getenv("DIGCRAWL_ASR_CACHE_TEST_LEGACY")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result, []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
}
