package mcpgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
)

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

func TestToolResultToMCPRejectsOversizedArtifactBeforeEmbedding(t *testing.T) {
	result, err := toolResultToMCP(context.Background(), &protocol.ToolResult{
		Status:    "success",
		Artifacts: []protocol.Artifact{{ID: "too-large", Type: "image/png", Size: artifact.DefaultInlineBytes + 1}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("oversized artifact should be visible as a tool error")
	}
	payload, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("missing structured error payload: %#v", result.StructuredContent)
	}
	errPayload, ok := payload["error"].(map[string]string)
	if !ok || errPayload["code"] != "ARTIFACT_UNAVAILABLE" {
		t.Fatalf("missing oversized artifact error: %#v", payload)
	}
}
