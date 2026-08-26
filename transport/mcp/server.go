package mcpgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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

// ServeStdio exposes DigEino tools as an MCP server over stdio (for IDE / Claude Desktop).
func ServeStdio(rt *runtime.Runtime) error {
	s := mcpserver.NewMCPServer(
		version.RuntimeName,
		version.RuntimeVersion,
		mcpserver.WithToolCapabilities(true),
	)
	registerTools(s, rt)
	return mcpserver.ServeStdio(s)
}

func registerTools(s *mcpserver.MCPServer, rt *runtime.Runtime) {
	manifest := rt.Manifest()
	for _, desc := range manifest.Tools {
		desc := desc
		tool := buildMCPTool(desc)
		s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			result := rt.Execute(ctx, call)
			return toolResultToMCPWithInlineLimit(ctx, result, rt.ArtifactStore(), rt.ArtifactInlineLimit())
		})
	}
}

// mcpTimeoutMs gives MCP the execution budget already bound to this Runtime.
// In particular, a user-selected Skill profile may enable bounded local video
// enrichment; consulting config.Get here would instead use an unrelated global
// default and prematurely cancel that work.
func mcpTimeoutMs(rt *runtime.Runtime, call *protocol.ToolCall) int {
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
	} else if desc.RequiresUserApproval || desc.Name == "skill.asr.enable" {
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
	registerTools(s, rt)
}
