package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMCPStdioHelper(t *testing.T) {
	if os.Getenv("DIGCRAWL_MCP_STDIO_HELPER") != "1" {
		return
	}
	run([]string{"digcrawl", "mcp", "--config", os.Getenv("DIGCRAWL_MCP_STDIO_CONFIG")})
}

func TestMCPPhaseOneProfileExposesOnlyReadTools(t *testing.T) {
	profilePath := writeMCPPhaseOneProfile(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioHelper$")
	cmd.Env = append(os.Environ(),
		"DIGCRAWL_MCP_STDIO_HELPER=1",
		"DIGCRAWL_MCP_STDIO_CONFIG="+profilePath,
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
