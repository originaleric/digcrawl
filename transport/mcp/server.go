package mcpgw

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/runtime/version"
)

const (
	// DefaultStdioIdleTimeout preserves the standard stdio-MCP lifecycle: a
	// connected client owns the session until it closes stdin. Hosts can opt in
	// to a positive timeout explicitly.
	DefaultStdioIdleTimeout time.Duration = 0
	// DefaultStdioSessionIdleTimeout bounds every initialized session started
	// by the CLI. A host can create many stdio children while one human uses
	// DigCrawl, so clientInfo must not decide whether a forgotten proxy lives
	// forever. An explicit --idle-timeout=0 remains the opt-out for a host that
	// truly requires an unbounded connection.
	DefaultStdioSessionIdleTimeout = 15 * time.Minute
	// Deprecated: CLI recovery now applies to every initialized stdio session.
	// Use DefaultStdioSessionIdleTimeout instead.
	DefaultCodexStdioIdleTimeout = DefaultStdioSessionIdleTimeout
	// DefaultStdioInitializeTimeout only applies while the client has not sent
	// MCP initialize. A conforming stdio client initializes immediately, so this
	// recovers an abandoned pipe without imposing an idle limit on normal hosts.
	DefaultStdioInitializeTimeout = time.Minute
)

// ToolRuntime is the small execution boundary required by the MCP transport.
// The local bridge implements it through the existing HTTP gateway, while an
// in-process Runtime continues to satisfy it for standalone users and tests.
// Keeping the MCP process at this boundary prevents every stdio child from
// becoming an independent browser owner.
type ToolRuntime interface {
	Manifest() protocol.ToolManifest
	Execute(context.Context, *protocol.ToolCall) *protocol.ToolResult
	ExecutionTimeout(*protocol.ToolCall) time.Duration
	ArtifactStore() artifact.Store
	ArtifactInlineLimit() int64
}

// ServeStdio exposes DigEino tools as an MCP server over stdio (for IDE / Claude Desktop).
//
// Keep this concrete parameter for source compatibility with existing Go
// integrations that pass or assign this function by its original type.
func ServeStdio(rt *runtime.Runtime) error {
	return serveRuntimeStdio(rt, DefaultStdioIdleTimeout, false)
}

// ServeStdioWithIdleTimeout exposes DigEino tools as an MCP server over stdio.
// A zero idleTimeout disables the guard for hosts that intentionally keep one
// long-lived, inactive MCP session open.
// Keep this concrete parameter for source compatibility with the original API.
func ServeStdioWithIdleTimeout(rt *runtime.Runtime, idleTimeout time.Duration) error {
	return serveRuntimeStdio(rt, idleTimeout, false)
}

// ServeStdioWithCodexRecovery is retained for callers of the original CLI
// helper. Despite its historical name, it reclaims every initialized session:
// an MCP client name is presentation metadata, not a safe lifecycle boundary.
// Keep this concrete parameter for source compatibility with the original API.
func ServeStdioWithCodexRecovery(rt *runtime.Runtime) error {
	return serveRuntimeStdio(rt, DefaultStdioIdleTimeout, true)
}

// ServeToolRuntimeStdio exposes an abstract ToolRuntime over stdio. It is the
// Bridge entry point: the stdio child remains a thin forwarding process while
// the bridge owns the browser Runtime. CLI sessions always use message-aware
// reclamation: uninitialized pipes expire quickly and initialized sessions
// expire after actual tool inactivity, never after MCP ping traffic. An
// explicit zero with automaticRecovery disabled keeps the legacy unbounded
// session behavior for hosts that require it.
func ServeToolRuntimeStdio(rt ToolRuntime, idleTimeout time.Duration, automaticRecovery bool) error {
	if idleTimeout == 0 && !automaticRecovery {
		return serveRuntimeStdio(rt, DefaultStdioIdleTimeout, false)
	}
	if idleTimeout <= 0 {
		idleTimeout = DefaultStdioSessionIdleTimeout
	}
	return serveToolRuntimeStdioWithSessionLifecycle(rt, idleTimeout)
}

func serveToolRuntimeStdioWithSessionLifecycle(rt ToolRuntime, sessionIdleTimeout time.Duration) error {
	guard := newIdleGuard()
	s := mcpserver.NewMCPServer(
		version.RuntimeName,
		version.RuntimeVersion,
		mcpserver.WithToolCapabilities(true),
	)
	registerTools(s, rt, guard)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveStdioWithSessionLifecycle(ctx, s, os.Stdin, os.Stdout, guard, sessionIdleTimeout)
}

func serveStdioWithSessionLifecycle(parent context.Context, server *mcpserver.MCPServer, stdin io.Reader, stdout io.Writer, guard *idleGuard, sessionIdleTimeout time.Duration) error {
	return serveStdioWithGuardOptions(parent, server, stdin, stdout, DefaultStdioIdleTimeout, guard, true, DefaultStdioInitializeTimeout, sessionIdleTimeout)
}

func serveRuntimeStdio(rt ToolRuntime, idleTimeout time.Duration, automaticCodexRecovery bool) error {
	guard := newIdleGuard()
	s := mcpserver.NewMCPServer(
		version.RuntimeName,
		version.RuntimeVersion,
		mcpserver.WithToolCapabilities(true),
	)
	registerTools(s, rt, guard)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveStdioWithGuardOptions(ctx, s, os.Stdin, os.Stdout, idleTimeout, guard, automaticCodexRecovery, DefaultStdioInitializeTimeout, DefaultStdioSessionIdleTimeout)
}

// serveStdio owns the process-level shutdown behavior separately from MCP
// request handling so it can be exercised with in-memory streams in tests.
func serveStdio(parent context.Context, server *mcpserver.MCPServer, stdin io.Reader, stdout io.Writer, idleTimeout time.Duration) error {
	return serveStdioWithGuard(parent, server, stdin, stdout, idleTimeout, newIdleGuard())
}

func serveStdioWithGuard(parent context.Context, server *mcpserver.MCPServer, stdin io.Reader, stdout io.Writer, idleTimeout time.Duration, guard *idleGuard) error {
	return serveStdioWithGuardOptions(parent, server, stdin, stdout, idleTimeout, guard, false, 0, 0)
}

func serveStdioWithAutomaticCodexRecovery(parent context.Context, server *mcpserver.MCPServer, stdin io.Reader, stdout io.Writer, guard *idleGuard, initializeTimeout, codexIdleTimeout time.Duration) error {
	return serveStdioWithGuardOptions(parent, server, stdin, stdout, DefaultStdioIdleTimeout, guard, true, initializeTimeout, codexIdleTimeout)
}

func serveStdioWithGuardOptions(parent context.Context, server *mcpserver.MCPServer, stdin io.Reader, stdout io.Writer, idleTimeout time.Duration, guard *idleGuard, automaticCodexRecovery bool, initializeTimeout, codexIdleTimeout time.Duration) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	reader := newActivityReader(stdin, guard, automaticCodexRecovery)
	if idleTimeout > 0 {
		go cancelWhenIdle(ctx, cancel, guard, idleTimeout)
	} else if automaticCodexRecovery {
		go cancelUninitializedOrIdle(ctx, cancel, guard, initializeTimeout, codexIdleTimeout)
	}

	err := mcpserver.NewStdioServer(server).Listen(ctx, reader, stdout)
	// SIGTERM, SIGINT, and the idle guard are expected lifecycle events, not
	// server failures. In particular, returning nil avoids a noisy non-zero
	// process exit when an abandoned pipe is reclaimed.
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// activityReader observes the subset of MCP messages that represent useful
// work without changing stdio's byte stream. In particular, an MCP ping is a
// transport liveness check, not user activity: a host must not be able to keep
// an abandoned stdio child alive indefinitely just by sending pings.
type activityReader struct {
	reader             io.Reader
	guard              *idleGuard
	observeInitialize  bool
	partialMessageLine []byte
	mu                 sync.Mutex
}

func newActivityReader(reader io.Reader, guard *idleGuard, observeInitialize bool) *activityReader {
	return &activityReader{reader: reader, guard: guard, observeInitialize: observeInitialize}
}

func (r *activityReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && r.guard != nil {
		r.observeMCPMessages(p[:n])
	}
	return n, err
}

// observeMCPMessages recognizes activity-bearing JSON-RPC requests. Stdio MCP
// is line-delimited, while Reader.Read may split or combine messages
// arbitrarily. Keep this observer bounded; the MCP server remains responsible
// for reporting malformed input to the client.
func (r *activityReader) observeMCPMessages(data []byte) {
	const maxObservedMCPLineBytes = 1 << 20

	r.mu.Lock()
	r.partialMessageLine = append(r.partialMessageLine, data...)
	for {
		newline := bytes.IndexByte(r.partialMessageLine, '\n')
		if newline < 0 {
			if len(r.partialMessageLine) > maxObservedMCPLineBytes {
				r.partialMessageLine = nil
			}
			r.mu.Unlock()
			return
		}
		line := append([]byte(nil), r.partialMessageLine[:newline]...)
		r.partialMessageLine = r.partialMessageLine[newline+1:]
		r.mu.Unlock()
		r.observeMCPMessage(line)
		r.mu.Lock()
	}
}

func (r *activityReader) observeMCPMessage(line []byte) {
	var message struct {
		Method string `json:"method"`
		Params struct {
			ClientInfo struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &message) != nil {
		return
	}

	switch message.Method {
	case "initialize":
		// A successful initialization is meaningful activity for every host.
		// The automatic recovery path additionally needs its client identity.
		r.guard.touch()
		if !r.observeInitialize {
			return
		}
		r.guard.noteMCPInitialize(message.Params.ClientInfo.Name)
	case "tools/call":
		// Tool calls are the only normal post-initialization client action that
		// consumes this server's runtime. Other protocol traffic (notably ping)
		// must not defeat idle reclamation.
		r.guard.touch()
	}
}

// idleGuard tracks both incoming stdio traffic and in-flight tool work. A
// long-running video or ASR call can legitimately be silent on stdio, so it
// must prevent the abandoned-session guard from treating the process as idle.
type idleGuard struct {
	mu               sync.Mutex
	activity         chan struct{}
	initializeEvents chan string
	initializedOnce  sync.Once
	initialized      bool
	clientName       string
	lastActivity     time.Time
	active           int
}

func newIdleGuard() *idleGuard {
	return &idleGuard{
		activity:         make(chan struct{}, 1),
		initializeEvents: make(chan string, 1),
		lastActivity:     time.Now(),
	}
}

func (g *idleGuard) touch() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.lastActivity = time.Now()
	g.mu.Unlock()
	g.notify()
}

func (g *idleGuard) notify() {
	select {
	case g.activity <- struct{}{}:
	default:
	}
}

func (g *idleGuard) startCall() func() {
	if g == nil {
		return func() {}
	}
	g.mu.Lock()
	g.active++
	g.lastActivity = time.Now()
	g.mu.Unlock()
	g.notify()
	return func() {
		g.mu.Lock()
		g.active--
		g.lastActivity = time.Now()
		g.mu.Unlock()
		g.notify()
	}
}

func (g *idleGuard) noteMCPInitialize(clientName string) {
	if g == nil {
		return
	}
	g.initializedOnce.Do(func() {
		clientName = strings.TrimSpace(clientName)
		g.mu.Lock()
		g.initialized = true
		g.clientName = clientName
		g.mu.Unlock()
		g.initializeEvents <- clientName
	})
}

func (g *idleGuard) initializedClient() (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.clientName, g.initialized
}

func (g *idleGuard) shouldCancelForIdle(timeout time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active == 0 && time.Since(g.lastActivity) >= timeout
}

func (g *idleGuard) nextIdleCheck(timeout time.Duration) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active > 0 {
		return timeout
	}
	remaining := timeout - time.Since(g.lastActivity)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (g *idleGuard) nextInitializationCheck(timeout time.Duration) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	remaining := timeout - time.Since(g.lastActivity)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (g *idleGuard) shouldCancelBeforeInitialize(timeout time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.initialized && time.Since(g.lastActivity) >= timeout
}

func cancelWhenIdle(ctx context.Context, cancel context.CancelFunc, guard *idleGuard, timeout time.Duration) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		delay := guard.nextIdleCheck(timeout)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
		select {
		case <-ctx.Done():
			return
		case <-guard.activity:
		case <-timer.C:
			// An input read can race this timer. shouldCancelForIdle takes the
			// same lock as touch, so activity observed before this check wins
			// even when select chose timer.C first.
			if guard.shouldCancelForIdle(timeout) {
				cancel()
				return
			}
		}
	}
}

func cancelUninitializedOrIdle(ctx context.Context, cancel context.CancelFunc, guard *idleGuard, initializeTimeout, sessionIdleTimeout time.Duration) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		if _, initialized := guard.initializedClient(); initialized {
			if sessionIdleTimeout > 0 {
				cancelWhenIdle(ctx, cancel, guard, sessionIdleTimeout)
			}
			return
		}

		delay := guard.nextInitializationCheck(initializeTimeout)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)

		select {
		case <-ctx.Done():
			return
		case <-guard.activity:
		case <-guard.initializeEvents:
		case <-timer.C:
			// touch and this predicate use the same lock. If an initialize
			// request has been read, or bytes are still arriving to complete
			// one, it wins over an equally-ready grace timer.
			if guard.shouldCancelBeforeInitialize(initializeTimeout) {
				cancel()
				return
			}
		}
	}
}

func registerTools(s *mcpserver.MCPServer, rt ToolRuntime, guard *idleGuard) {
	manifest := rt.Manifest()
	for _, desc := range manifest.Tools {
		desc := desc
		tool := buildMCPTool(desc)
		s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			done := guard.startCall()
			defer done()

			args := req.GetArguments()
			input, err := json.Marshal(args)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			call := &protocol.ToolCall{
				Type:  protocol.TypeToolCall,
				ID:    "mcp_" + uuid.NewString(),
				Tool:  desc.Name,
				Input: input,
			}
			call.Policy.TimeoutMs = mcpTimeoutMs(rt, call)
			progress := req.Params.Meta != nil && req.Params.Meta.ProgressToken != nil
			if progress {
				_ = s.SendNotificationToClient(ctx, "notifications/progress", map[string]any{"progressToken": req.Params.Meta.ProgressToken, "progress": 0, "total": 1, "message": "正在执行工具"})
			}
			result := rt.Execute(ctx, call)
			if progress {
				message := "工具执行完成"
				if result.Status != "success" {
					message = "工具执行失败，请查看结构化错误中的 next_actions"
				}
				_ = s.SendNotificationToClient(ctx, "notifications/progress", map[string]any{"progressToken": req.Params.Meta.ProgressToken, "progress": 1, "total": 1, "message": message})
			}
			return toolResultToMCPWithInlineLimit(ctx, result, rt.ArtifactStore(), rt.ArtifactInlineLimit())
		})
	}
}

// mcpTimeoutMs gives MCP the execution budget already bound to this Runtime.
// In particular, a user-selected Skill profile may enable bounded local video
// enrichment; consulting config.Get here would instead use an unrelated global
// default and prematurely cancel that work.
func mcpTimeoutMs(rt ToolRuntime, call *protocol.ToolCall) int {
	if rt == nil {
		return int((60 * time.Second).Milliseconds())
	}
	return int(rt.ExecutionTimeout(call).Milliseconds())
}

func buildMCPTool(desc protocol.ToolDescriptor) mcp.Tool {
	// Do not start with mcp.NewTool: it applies pessimistic annotations that are
	// still explicit claims in the MCP protocol. Runtime descriptors are the
	// source of truth for both schemas and behavior hints.
	tool := mcp.Tool{Name: desc.Name, Description: desc.Description}
	if desc.Name == "gateway.ping" {
		tool.Annotations.ReadOnlyHint = mcp.ToBoolPtr(true)
		tool.Annotations.DestructiveHint = mcp.ToBoolPtr(false)
		tool.Annotations.IdempotentHint = mcp.ToBoolPtr(true)
	} else if desc.RequiresUserApproval || hasCapability(desc.Capabilities, "config.local") {
		tool.Annotations.DestructiveHint = mcp.ToBoolPtr(true)
	} else if !hasCapability(desc.Capabilities, "web.interact") {
		// Content reads can update local session cookies or artifacts, so they
		// must not advertise read-only/idempotent behavior to an MCP host. They
		// remain non-destructive unless the descriptor requires approval.
		tool.Annotations.DestructiveHint = mcp.ToBoolPtr(false)
	}
	if len(desc.InputSchema) > 0 {
		// MCP permits either InputSchema or RawInputSchema, not both; Runtime
		// descriptors own the authoritative schema.
		tool.RawInputSchema = desc.InputSchema
	} else {
		tool.InputSchema = mcp.ToolInputSchema{Type: "object", Properties: map[string]any{}}
	}
	if len(desc.OutputSchema) > 0 {
		tool.RawOutputSchema = desc.OutputSchema
	}
	return tool
}

func hasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func toolResultToMCP(ctx context.Context, result *protocol.ToolResult, store artifact.Store) (*mcp.CallToolResult, error) {
	return toolResultToMCPWithInlineLimit(ctx, result, store, artifact.DefaultInlineBytes)
}

func toolResultToMCPWithInlineLimit(ctx context.Context, result *protocol.ToolResult, store artifact.Store, inlineLimit int64) (*mcp.CallToolResult, error) {
	if result == nil {
		return mcp.NewToolResultError("nil result"), nil
	}
	output := map[string]any{}
	if len(result.Output) > 0 {
		if err := json.Unmarshal(result.Output, &output); err != nil {
			return mcp.NewToolResultError("invalid tool output"), nil
		}
	}
	if len(result.Artifacts) > 0 {
		output["artifacts"] = result.Artifacts
	}
	artifactContent, artifactErr := mcpArtifactContent(ctx, result.Artifacts, store, inlineLimit)
	if result.Status == "error" {
		code := "TOOL_EXEC_FAILED"
		message := "tool failed"
		if result.Error != nil {
			code = result.Error.Code
			message = result.Error.Message
		}
		// Keep the established string-only error object compatible with MCP
		// consumers that decode it as map[string]string. Rich diagnostics live in
		// a sibling field instead of changing that object's value types.
		errorOutput := map[string]string{"code": code, "message": message}
		if result.Error != nil && len(result.Error.Details) > 0 {
			output["error_details"] = result.Error.Details
		}
		output["error"] = errorOutput
		mcpResult := mcp.NewToolResultStructuredOnly(output)
		mcpResult.Content = append(mcpResult.Content, artifactContent...)
		mcpResult.IsError = true
		return mcpResult, nil
	}
	if artifactErr != nil {
		output["error"] = map[string]string{"code": "ARTIFACT_UNAVAILABLE", "message": artifactErr.Error()}
		mcpResult := mcp.NewToolResultStructuredOnly(output)
		mcpResult.IsError = true
		return mcpResult, nil
	}
	mcpResult := mcp.NewToolResultStructuredOnly(output)
	mcpResult.Content = append(mcpResult.Content, artifactContent...)
	return mcpResult, nil
}

func mcpArtifactContent(ctx context.Context, artifacts []protocol.Artifact, store artifact.Store, inlineLimit int64) ([]mcp.Content, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("artifact store is unavailable")
	}
	limit := artifact.InlineLimit(inlineLimit)
	content := make([]mcp.Content, 0, len(artifacts))
	for _, item := range artifacts {
		if item.Size > limit {
			return nil, fmt.Errorf("artifact %q exceeds %d byte inline limit for this MCP transport", item.ID, limit)
		}
		data, mimeType, err := store.Get(ctx, item.ID)
		if err != nil {
			return nil, fmt.Errorf("artifact %q is unavailable: %w", item.ID, err)
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("artifact %q exceeds %d byte inline limit for this MCP transport", item.ID, limit)
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
			content = append(content, mcp.NewImageContent(encoded, mimeType))
			continue
		}
		content = append(content, mcp.NewEmbeddedResource(mcp.BlobResourceContents{
			URI:      item.URI,
			MIMEType: mimeType,
			Blob:     encoded,
		}))
	}
	return content, nil
}

// RegisterFromRegistry is a test helper to register tools from a registry directly.
func RegisterFromRegistry(s *mcpserver.MCPServer, reg *registry.Registry, rt *runtime.Runtime) {
	_ = reg
	registerTools(s, rt, nil)
}
