package mcpgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

func TestServeStdioReclaimsAbandonedOpenPipe(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdio(context.Background(), server, stdin, io.Discard, 40*time.Millisecond)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idle stdio server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle stdio server did not exit while its client pipe stayed open")
	}
}

func TestDefaultStdioIdleTimeoutKeepsAnOpenSessionAlive(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdio(context.Background(), server, stdin, io.Discard, DefaultStdioIdleTimeout)
	}()

	select {
	case err := <-done:
		t.Fatalf("default stdio session closed without EOF: %v", err)
	case <-time.After(120 * time.Millisecond):
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stdio server returned error after EOF: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stdio server did not exit after EOF")
	}
}

func TestDeprecatedCodexIdleTimeoutAliasKeepsItsPublicValue(t *testing.T) {
	if DefaultCodexStdioIdleTimeout != DefaultStdioSessionIdleTimeout {
		t.Fatalf("deprecated Codex timeout=%s want session timeout=%s", DefaultCodexStdioIdleTimeout, DefaultStdioSessionIdleTimeout)
	}
}

func TestLegacyStdioEntrypointsRetainExactRuntimeFunctionTypes(t *testing.T) {
	var _ func(*runtime.Runtime) error = ServeStdio
	var _ func(*runtime.Runtime, time.Duration) error = ServeStdioWithIdleTimeout
	var _ func(*runtime.Runtime) error = ServeStdioWithCodexRecovery
	var _ func(ToolRuntime, time.Duration, bool) error = ServeToolRuntimeStdio
}

func TestAutomaticCodexRecoveryReclaimsExistingCodexRegistration(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdioWithAutomaticCodexRecovery(context.Background(), server, stdin, io.Discard, newIdleGuard(), time.Second, 40*time.Millisecond)
	}()
	writeMCPInitialize(t, client, "Codex")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("automatic Codex recovery returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Codex session was not reclaimed after its idle deadline")
	}
}

func TestAutomaticRecoveryReclaimsPingingUnknownRegistration(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	const idleTimeout = 60 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdioWithAutomaticCodexRecovery(context.Background(), server, stdin, io.Discard, newIdleGuard(), time.Second, idleTimeout)
	}()
	writeMCPInitialize(t, client, "future-host-with-an-unrecognized-client-name")

	// Codex can ping a healthy MCP child without any user work taking place.
	// Those liveness packets must not make the idle deadline slide forever.
	stopPings := make(chan struct{})
	pingsStopped := make(chan struct{})
	go func() {
		defer close(pingsStopped)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPings:
				return
			case <-ticker.C:
				if _, err := io.WriteString(client, `{"jsonrpc":"2.0","id":2,"method":"ping","params":{}}`+"\n"); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(stopPings)
		_ = client.Close()
		<-pingsStopped
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("automatic Codex recovery returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pinging unknown session was not reclaimed after its idle deadline")
	}
}

func TestAutomaticRecoveryReclaimsUnknownInitializedClient(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdioWithAutomaticCodexRecovery(context.Background(), server, stdin, io.Discard, newIdleGuard(), 40*time.Millisecond, 40*time.Millisecond)
	}()
	writeMCPInitialize(t, client, "future-host-with-an-unrecognized-client-name")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unknown initialized session returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unknown initialized session was not reclaimed after its idle deadline")
	}
}

func TestAutomaticCodexRecoveryReclaimsUninitializedOpenPipe(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdioWithAutomaticCodexRecovery(context.Background(), server, stdin, io.Discard, newIdleGuard(), 40*time.Millisecond, time.Second)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("uninitialized pipe recovery returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("uninitialized open pipe was not reclaimed")
	}
}

func TestIdleGuardActivityWinsOverAnExpiredTimer(t *testing.T) {
	guard := newIdleGuard()
	guard.mu.Lock()
	guard.lastActivity = time.Now().Add(-time.Minute)
	guard.mu.Unlock()

	// This models a read that has already completed when timer.C and activity
	// are both selectable. The cancellation predicate must use the fresh
	// timestamp, not which select arm happened to win.
	guard.touch()
	if guard.shouldCancelForIdle(time.Second) {
		t.Fatal("fresh stdio activity was treated as idle")
	}
}

func TestIdleGuardActivityWinsOverAnExpiredInitializeTimer(t *testing.T) {
	guard := newIdleGuard()
	guard.mu.Lock()
	guard.lastActivity = time.Now().Add(-time.Minute)
	guard.mu.Unlock()

	// An initialize line may be observed just as its grace timer becomes ready.
	// Fresh bytes must win even before the observer finishes decoding that line.
	guard.touch()
	if guard.shouldCancelBeforeInitialize(time.Second) {
		t.Fatal("fresh initialize activity was treated as an abandoned pipe")
	}
}

func writeMCPInitialize(t *testing.T, writer io.Writer, clientName string) {
	t.Helper()
	if _, err := io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"`+clientName+`","version":"test"}}}`+"\n"); err != nil {
		t.Fatalf("write MCP initialize request: %v", err)
	}
}

func TestServeStdioIdleTimeoutResetsWhenClientWrites(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	const idleTimeout = 120 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdio(context.Background(), server, stdin, io.Discard, idleTimeout)
	}()

	// This is a valid request, and arrives just before the original deadline.
	// Keeping the pipe open afterward verifies that incoming bytes reset the
	// watchdog rather than merely postponing process startup.
	time.Sleep(70 * time.Millisecond)
	if _, err := io.WriteString(client, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`+"\n"); err != nil {
		t.Fatalf("write MCP initialize request: %v", err)
	}

	select {
	case err := <-done:
		t.Fatalf("server stopped before reset idle deadline: %v", err)
	case <-time.After(80 * time.Millisecond):
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idle stdio server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not exit after the reset idle deadline")
	}
}

func TestServeStdioIdleTimeoutDoesNotTreatPingAsActivity(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	const idleTimeout = 60 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdio(context.Background(), server, stdin, io.Discard, idleTimeout)
	}()
	writeMCPInitialize(t, client, "any-host")

	stopPings := make(chan struct{})
	pingsStopped := make(chan struct{})
	go func() {
		defer close(pingsStopped)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPings:
				return
			case <-ticker.C:
				if _, err := io.WriteString(client, `{"jsonrpc":"2.0","id":2,"method":"ping","params":{}}`+"\n"); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(stopPings)
		_ = client.Close()
		<-pingsStopped
	})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idle stdio server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pinging stdio session was not reclaimed after its idle deadline")
	}
}

func TestServeStdioIdleTimeoutWaitsForInFlightTool(t *testing.T) {
	stdin, client := io.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	const idleTimeout = 50 * time.Millisecond
	guard := newIdleGuard()
	finishTool := guard.startCall()
	done := make(chan error, 1)
	go func() {
		server := mcpserver.NewMCPServer("test", "1.0.0")
		done <- serveStdioWithGuard(context.Background(), server, stdin, io.Discard, idleTimeout, guard)
	}()

	// The stdin pipe is otherwise silent. The guard must keep the server alive
	// while a legitimate long-running tool call is in flight.
	select {
	case err := <-done:
		t.Fatalf("server stopped while tool was active: %v", err)
	case <-time.After(2 * idleTimeout):
	}

	finishTool()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idle stdio server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not exit after the in-flight tool completed")
	}
}

func TestMCPTimeoutUsesBoundRuntimeExecutionPolicy(t *testing.T) {
	const want = 23 * time.Minute
	rt := runtime.New(nil, runtime.Options{TimeoutForCall: func(*protocol.ToolCall) time.Duration { return want }})
	if got := time.Duration(mcpTimeoutMs(rt, &protocol.ToolCall{Tool: "youtube.video.read"})) * time.Millisecond; got != want {
		t.Fatalf("MCP timeout=%s want %s", got, want)
	}
}

func TestBuildMCPToolPreservesRuntimeSchemasAndDoesNotOverclaimReadOnly(t *testing.T) {
	input := json.RawMessage(`{"type":"object","required":["url"],"properties":{"url":{"type":"string"}}}`)
	output := json.RawMessage(`{"type":"object","properties":{"markdown":{"type":"string"}}}`)
	tool := buildMCPTool(protocol.ToolDescriptor{
		Name:         "wechat.article.read",
		Description:  "Read an article.",
		InputSchema:  input,
		OutputSchema: output,
		Capabilities: []string{"platform.read"},
	})
	if string(tool.RawInputSchema) != string(input) || string(tool.RawOutputSchema) != string(output) {
		t.Fatalf("MCP tool schema drift: %+v", tool)
	}
	if _, err := json.Marshal(tool); err != nil {
		t.Fatalf("MCP tool schema must be serializable: %v", err)
	}
	if tool.Annotations.ReadOnlyHint != nil || tool.Annotations.IdempotentHint != nil ||
		tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Fatalf("content reader annotations overclaim side-effect safety: %+v", tool.Annotations)
	}
}

func TestBuildMCPToolMarksHealthCheckReadOnlyAndIdempotent(t *testing.T) {
	tool := buildMCPTool(protocol.ToolDescriptor{
		Name:         "gateway.ping",
		Capabilities: []string{"gateway.meta"},
	})
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint ||
		tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint ||
		tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		t.Fatalf("health check annotations are not safe: %+v", tool.Annotations)
	}
}

func TestBuildMCPToolMarksLocalSkillSettingAsDestructive(t *testing.T) {
	tool := buildMCPTool(protocol.ToolDescriptor{
		Name:         "skill.media.configure",
		Capabilities: []string{"config.local"},
	})
	if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
		t.Fatalf("local configuration tool must advertise a local write: %+v", tool.Annotations)
	}
}

func TestToolResultToMCPPreservesStructuredErrorOutput(t *testing.T) {
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status: "error",
		Output: json.RawMessage(`{"title":"Restricted article","platform_metadata":{"retryable":false}}`),
		Error:  &protocol.ToolError{Code: "LOGIN_REQUIRED", Message: "local login required"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("MCP result should be marked as an error")
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("missing structured error payload: %#v", result.StructuredContent)
	}
	if payload["title"] != "Restricted article" {
		t.Fatalf("partial output was lost: %#v", payload)
	}
	errorPayload, ok := payload["error"].(map[string]string)
	if !ok || errorPayload["code"] != "LOGIN_REQUIRED" {
		t.Fatalf("structured error was lost: %#v", payload)
	}
}

func TestToolResultToMCPIncludesErrorDetails(t *testing.T) {
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status: "error",
		Error: &protocol.ToolError{
			Code:    "BROWSER_PAGE_CLOSED",
			Message: "页面导航失败: target closed",
			Details: map[string]any{"component": "local_browser", "stage": "navigate", "retryable": true},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := result.StructuredContent.(map[string]any)
	errorPayload, ok := payload["error"].(map[string]string)
	if !ok || errorPayload["code"] != "BROWSER_PAGE_CLOSED" {
		t.Fatalf("compatible error object was lost: %#v", payload)
	}
	details, ok := payload["error_details"].(map[string]any)
	if !ok || details["stage"] != "navigate" || details["retryable"] != true {
		t.Fatalf("error details were lost: %#v", payload)
	}
}

func TestToolResultToMCPPreservesMediaDownloadFailure(t *testing.T) {
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status: "error",
		Error: &protocol.ToolError{
			Code:    "MEDIA_DOWNLOAD_FAILED",
			Message: "media download failed: video size 35865567 bytes exceeds configured limit 10485760 bytes",
			Details: map[string]any{"component": "media_download", "media_type": "video", "reason": "size_limit_exceeded", "retryable": false, "actual_bytes": int64(35865567), "max_bytes": int64(10485760)},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := result.StructuredContent.(map[string]any)
	errorPayload, ok := payload["error"].(map[string]string)
	if !ok || errorPayload["code"] != "MEDIA_DOWNLOAD_FAILED" {
		t.Fatalf("structured error=%#v", payload)
	}
	details, ok := payload["error_details"].(map[string]any)
	if !ok || details["reason"] != "size_limit_exceeded" || details["retryable"] != false {
		t.Fatalf("structured details=%#v", payload)
	}
}

func TestToolResultToMCPReturnsStructuredSuccessOutput(t *testing.T) {
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status: "success",
		Output: json.RawMessage(`{"title":"Article","markdown":"# Article"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %#v", result)
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok || payload["markdown"] != "# Article" {
		t.Fatalf("structured success output was lost: %#v", result.StructuredContent)
	}
}

func TestToolResultToMCPEmbedsPersistedImageArtifacts(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("png-bytes")
	item, err := store.Put(context.Background(), "screenshot", "image/png", "page.png", data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status:    "success",
		Output:    json.RawMessage(`{"title":"Article"}`),
		Artifacts: []protocol.Artifact{item},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok || payload["artifacts"] == nil {
		t.Fatalf("artifact metadata missing from structured content: %#v", result.StructuredContent)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content=%#v", result.Content)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("artifact result must be serializable: %v", err)
	}
	image, ok := result.Content[1].(mcp.ImageContent)
	if !ok || image.MIMEType != "image/png" || image.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("image artifact was not embedded correctly: %#v", result.Content[1])
	}
}

func TestToolResultToMCPReportsUnavailableArtifact(t *testing.T) {
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status:    "success",
		Artifacts: []protocol.Artifact{{ID: "missing", Type: "image/png"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("missing artifact should be visible as a tool error")
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("missing structured error payload: %#v", result.StructuredContent)
	}
	errPayload, ok := payload["error"].(map[string]string)
	if !ok || errPayload["code"] != "ARTIFACT_UNAVAILABLE" {
		t.Fatalf("missing artifact error: %#v", payload)
	}
}

func TestToolResultToMCPRejectsOversizedArtifactWithoutRetrievalPath(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status:    "success",
		Artifacts: []protocol.Artifact{{ID: "too-large", Type: "image/png", Size: artifact.DefaultInlineBytes + 1}},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("MCP must not report an unreachable oversized artifact as success: %#v", result)
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("missing structured artifact error: %#v", result.StructuredContent)
	}
	errPayload, ok := payload["error"].(map[string]string)
	if !ok || errPayload["code"] != "ARTIFACT_UNAVAILABLE" {
		t.Fatalf("oversized artifact error was not surfaced: %#v", payload)
	}
}

func TestToolResultToMCPUsesRuntimeBoundInlineLimit(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Put(context.Background(), "small-video", "video/mp4", "video.mp4", []byte("video"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := toolResultToMCPWithInlineLimit(context.Background(), &protocol.ToolResult{
		Status: "success", Artifacts: []protocol.Artifact{item},
	}, store, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("configured inline limit must reject an artifact MCP cannot retrieve: %#v", result)
	}
}
