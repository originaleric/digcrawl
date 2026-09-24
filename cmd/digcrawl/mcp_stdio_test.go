package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/transport/bridge"
)

func TestMCPStdioHelper(t *testing.T) {
	if os.Getenv("DIGCRAWL_MCP_STDIO_HELPER") != "1" {
		return
	}
	run([]string{"digcrawl", "mcp", "--idle-timeout", "100ms", "--config", os.Getenv("DIGCRAWL_MCP_STDIO_CONFIG")})
}

func TestMCPBridgeHelper(t *testing.T) {
	if os.Getenv("DIGCRAWL_BRIDGE_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			run(append([]string{"digcrawl"}, os.Args[i+1:]...))
			return
		}
	}
	t.Fatal("bridge helper did not receive command arguments")
}

func TestBridgeServeRejectsUnmanagedForegroundWorker(t *testing.T) {
	profilePath := writeMCPPhaseOneProfile(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPBridgeHelper$", "--", "bridge", "serve", "--config", profilePath)
	cmd.Env = append(os.Environ(), "DIGCRAWL_BRIDGE_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("unmanaged bridge serve unexpectedly started")
	}
	if !strings.Contains(string(output), "bridge serve is internal") {
		t.Fatalf("unmanaged bridge serve output=%q", output)
	}
}

func TestMCPPhaseOneProfileExposesOnlyReadTools(t *testing.T) {
	profilePath := writeMCPPhaseOneProfile(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := bridge.Stop(ctx, profilePath); err != nil {
			t.Errorf("stop test bridge: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioHelper$")
	cmd.Env = append(os.Environ(),
		"DIGCRAWL_MCP_STDIO_HELPER=1",
		"DIGCRAWL_MCP_STDIO_CONFIG="+profilePath,
		"DIGCRAWL_BRIDGE_EXECUTABLE="+writeMCPBridgeExecutable(t),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr lockedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	writeMCPRequest(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "digcrawl-test", "version": "1"},
		},
	})
	var initialize struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	readMCPResponse(t, scanner, &initialize, stderr.String())
	if initialize.ID != 1 || len(initialize.Result) == 0 {
		t.Fatalf("unexpected initialize response: %+v", initialize)
	}
	writeMCPRequest(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	writeMCPRequest(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	var list struct {
		ID     int `json:"id"`
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema any    `json:"inputSchema"`
				Annotations struct {
					ReadOnlyHint    *bool `json:"readOnlyHint"`
					DestructiveHint *bool `json:"destructiveHint"`
					IdempotentHint  *bool `json:"idempotentHint"`
				} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	readMCPResponse(t, scanner, &list, stderr.String())
	if list.ID != 2 {
		t.Fatalf("unexpected tools/list response: %+v", list)
	}
	tools := make(map[string]struct {
		inputSchema any
		readOnly    *bool
		destructive *bool
		idempotent  *bool
	}, len(list.Result.Tools))
	for _, tool := range list.Result.Tools {
		tools[tool.Name] = struct {
			inputSchema any
			readOnly    *bool
			destructive *bool
			idempotent  *bool
		}{tool.InputSchema, tool.Annotations.ReadOnlyHint, tool.Annotations.DestructiveHint, tool.Annotations.IdempotentHint}
	}
	for _, name := range []string{"gateway.ping", "browser.browse", "wechat.article.read", "feed.read"} {
		tool, ok := tools[name]
		if !ok || tool.inputSchema == nil {
			t.Fatalf("Phase 1 MCP profile missing callable tool %q: %#v", name, tools)
		}
	}
	for _, name := range []string{"browser.action", "browser.snapshot", "asr.transcribe", "file.read"} {
		if _, ok := tools[name]; ok {
			t.Fatalf("Phase 1 MCP profile exposed prohibited tool %q", name)
		}
	}
	ping := tools["gateway.ping"]
	if ping.readOnly == nil || !*ping.readOnly || ping.destructive == nil || *ping.destructive || ping.idempotent == nil || !*ping.idempotent {
		t.Fatalf("gateway.ping annotations=%+v", ping)
	}
	for _, name := range []string{"browser.browse", "wechat.article.read", "feed.read"} {
		tool := tools[name]
		if tool.readOnly != nil || tool.idempotent != nil || tool.destructive == nil || *tool.destructive {
			t.Fatalf("%s annotations overclaim no side effects: %+v", name, tool)
		}
	}
	writeMCPRequest(t, stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "gateway.ping",
			"arguments": map[string]any{},
		},
	})
	var pingResponse struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	readMCPResponse(t, scanner, &pingResponse, stderr.String())
	if pingResponse.ID != 3 || !bytes.Contains(pingResponse.Result, []byte("pong")) {
		t.Fatalf("gateway.ping response=%s", pingResponse.Result)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("MCP helper failed: %v; stderr=%s", err, stderr.String())
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("MCP stdio integration test timed out: %v; stderr=%s", err, stderr.String())
	}
	// The stdio child has exited, but it must have launched a detached Bridge
	// through Executor.startOrWait. A ready manifest here covers the actual
	// sidecar creation path instead of only attaching to a pre-started server.
	waitForMCPTestBridge(t, profilePath)
}

func TestBridgeRestartReplacesRunningBridge(t *testing.T) {
	profilePath := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := bridge.SocketPath(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStart()
	if _, err := bridge.NewExecutor(startCtx, writeMCPBridgeExecutable(t), profilePath, cfg); err != nil {
		t.Fatalf("start production-shape bridge: %v", err)
	}
	waitForMCPTestBridge(t, profilePath)
	oldPID := waitForMCPTestBridgePID(t, socket)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, stopErr := bridge.Stop(ctx, profilePath); stopErr != nil {
			t.Errorf("stop replacement bridge: %v", stopErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPBridgeHelper$", "--", "bridge", "restart", "--config", profilePath)
	cmd.Env = append(os.Environ(),
		"DIGCRAWL_BRIDGE_HELPER=1",
		"DIGCRAWL_BRIDGE_EXECUTABLE="+writeMCPBridgeExecutable(t),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bridge restart command: %v; stderr=%s", err, stderr.String())
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("bridge restart timed out: %v; stderr=%s", err, stderr.String())
	}
	waitForMCPTestBridge(t, profilePath)
	newPID := waitForMCPTestBridgePID(t, socket)
	if newPID == oldPID {
		t.Fatalf("bridge restart retained old owner PID %d", oldPID)
	}
	if live, err := mcpTestProcessLive(oldPID); err != nil || live {
		t.Fatalf("old bridge owner survived restart: live=%v err=%v", live, err)
	}
}

func TestConcurrentExecutorsLeaveOneManagedBridgeWorker(t *testing.T) {
	profilePath := writeMCPPhaseOneProfile(t)
	cfg, err := config.Load(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := bridge.SocketPath(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	executable := writeMCPBridgeExecutable(t)
	const starters = 12
	errs := make(chan error, starters)
	var callers sync.WaitGroup
	callers.Add(starters)
	for range starters {
		go func() {
			defer callers.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, err := bridge.NewExecutor(ctx, executable, profilePath, cfg)
			errs <- err
		}()
	}
	callers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Executor startup: %v", err)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, stopErr := bridge.Stop(ctx, profilePath); stopErr != nil {
			t.Errorf("stop concurrent test bridge: %v", stopErr)
		}
	})
	waitForMCPTestBridge(t, profilePath)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pids := mcpTestBridgePIDs(t, socket); len(pids) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("managed Bridge workers for one socket = %v, want exactly one", mcpTestBridgePIDs(t, socket))
}

func writeMCPBridgeExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "digcrawl-bridge-test-helper")
	script := "#!/bin/sh\nDIGCRAWL_BRIDGE_HELPER=1 exec " + strconv.Quote(os.Args[0]) + " -test.run='^TestMCPBridgeHelper$' -- \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForMCPTestBridge(t *testing.T, profilePath string) {
	t.Helper()
	socket, err := bridge.SocketPath(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &net.Dialer{}
	httpClient := &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		},
	}}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://digcrawl-bridge/manifest", nil)
		if err == nil {
			response, err := httpClient.Do(request)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("MCP-launched bridge did not become ready")
}

func waitForMCPTestBridgePID(t *testing.T, socket string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
		if err == nil {
			needle := "--socket " + socket
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 || !strings.Contains(line, "bridge serve") || !strings.Contains(line, needle) {
					continue
				}
				pid, parseErr := strconv.Atoi(fields[0])
				if parseErr == nil && pid > 0 {
					return pid
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("MCP-launched bridge owner did not appear in the process table")
	return 0
}

func mcpTestBridgePIDs(t *testing.T, socket string) []int {
	t.Helper()
	output, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		t.Fatalf("inspect MCP-launched bridge workers: %v", err)
	}
	needle := "--socket " + socket
	var pids []int
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(line, "bridge serve") || !strings.Contains(line, needle) {
			continue
		}
		pid, parseErr := strconv.Atoi(fields[0])
		if parseErr == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func mcpTestProcessLive(pid int) (bool, error) {
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(output)) != "" && !strings.HasPrefix(strings.TrimSpace(string(output)), "Z"), nil
}

// lockedBuffer owns child-process stderr for this streaming integration test.
// os/exec copies stderr in a goroutine after Start, while assertion failures
// need a snapshot before Wait; bytes.Buffer alone is not safe for that pair.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func writeMCPPhaseOneProfile(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "config", "digcrawl-skill.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	profile := strings.ReplaceAll(string(data), `"/absolute/path/to/digcrawl/browser_cookies"`, strconv.Quote(filepath.Join(dir, "cookies")))
	profile = strings.ReplaceAll(profile, `"/absolute/path/to/digcrawl/artifacts"`, strconv.Quote(filepath.Join(dir, "artifacts")))
	profile = strings.ReplaceAll(profile, `"/absolute/path/to/digcrawl/skill-media"`, strconv.Quote(filepath.Join(dir, "skill-media")))
	path := filepath.Join(dir, "digcrawl-skill.yaml")
	if err := os.WriteFile(path, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeMCPRequest(t *testing.T, writer io.Writer, request map[string]any) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(writer, "%s\n", data); err != nil {
		t.Fatal(err)
	}
}

func readMCPResponse(t *testing.T, scanner *bufio.Scanner, target any, stderr string) {
	t.Helper()
	if !scanner.Scan() {
		t.Fatalf("MCP response missing: scan_err=%v stderr=%s", scanner.Err(), stderr)
	}
	if err := json.Unmarshal(scanner.Bytes(), target); err != nil {
		t.Fatalf("invalid MCP response %q: %v", scanner.Text(), err)
	}
}
