package mcpgw

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/originaleric/diggate/runtime/version"
	"github.com/originaleric/diggate/protocol"
	"github.com/originaleric/diggate/runtime/registry"
	"github.com/originaleric/diggate/runtime"
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
			result := rt.Execute(ctx, call)
			return toolResultToMCP(result)
		})
	}
}

func buildMCPTool(desc protocol.ToolDescriptor) mcp.Tool {
	return mcp.NewTool(desc.Name, mcp.WithDescription(desc.Description))
}

func toolResultToMCP(result *protocol.ToolResult) (*mcp.CallToolResult, error) {
	if result == nil {
		return mcp.NewToolResultError("nil result"), nil
	}
	if result.Status == "error" {
		msg := "tool failed"
		if result.Error != nil {
			msg = fmt.Sprintf("[%s] %s", result.Error.Code, result.Error.Message)
		}
		return mcp.NewToolResultError(msg), nil
	}
	if len(result.Output) == 0 {
		return mcp.NewToolResultText("{}"), nil
	}
	return mcp.NewToolResultText(string(result.Output)), nil
}

// RegisterFromRegistry is a test helper to register tools from a registry directly.
func RegisterFromRegistry(s *mcpserver.MCPServer, reg *registry.Registry, rt *runtime.Runtime) {
	_ = reg
	registerTools(s, rt)
}
