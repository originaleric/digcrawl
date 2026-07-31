package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/registry"
)

func TestExecuteUnknownTool(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := New(reg, Options{InstanceID: "test"})
	call := &protocol.ToolCall{
		Type: protocol.TypeToolCall,
		ID:   "call_1",
		Tool: "missing.tool",
	}
	res := rt.Execute(context.Background(), call)
	if res.Status != "error" || res.Error == nil {
		t.Fatalf("expected error result, got %+v", res)
	}
}

func TestManifest(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "browser.browse", Description: "test"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{"ok": true}, nil, nil
		},
	})
	rt := New(reg, Options{InstanceID: "inst_test"})
	m := rt.Manifest()
	if m.InstanceID != "inst_test" || len(m.Tools) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestValidateCallRequiresID(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := New(reg, Options{})
	res := rt.Execute(context.Background(), &protocol.ToolCall{Tool: "browser.browse"})
	if res.Status != "error" {
		t.Fatalf("expected error, got %+v", res)
	}
	_, _ = json.Marshal(res)
}

func TestExecuteNilCallReturnsError(t *testing.T) {
	t.Parallel()
	rt := New(registry.New(), Options{})
	res := rt.Execute(context.Background(), nil)
	if res.Status != "error" || res.Error == nil || res.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %+v", res)
	}
}

func TestExecuteRequiresApproval(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "danger.tool", RequiresUserApproval: true},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{"ok": true}, nil, nil
		},
	})
	rt := New(reg, Options{RequireApproval: true})
	res := rt.Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c1", Tool: "danger.tool"})
	if res.Status != "error" || res.Error == nil || res.Error.Code != "USER_APPROVAL_REQUIRED" {
		t.Fatalf("expected approval error, got %+v", res)
	}
	res = rt.Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c2", Tool: "danger.tool", Approved: true})
	if res.Status != "success" {
		t.Fatalf("expected success after approval, got %+v", res)
	}
}

func TestExecutePreservesStructuredOutputOnHandlerError(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "structured.error"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{"platform_metadata": map[string]any{"error_code": "LOGIN_REQUIRED"}}, nil, testStructuredError{code: "LOGIN_REQUIRED", msg: "login required"}
		},
	})
	rt := New(reg, Options{})
	res := rt.Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "c1", Tool: "structured.error"})
	if res.Status != "error" || res.Error == nil || res.Error.Code != "LOGIN_REQUIRED" {
		t.Fatalf("expected structured error result, got %+v", res)
	}
	if len(res.Output) == 0 {
		t.Fatalf("expected structured output to be preserved")
	}
}

type testStructuredError struct {
	code string
	msg  string
}

func (e testStructuredError) Error() string            { return e.msg }
func (e testStructuredError) ToolErrorCode() string    { return e.code }
func (e testStructuredError) ToolErrorMessage() string { return e.msg }

func TestDefaultRegistryIncludesASRTranscribe(t *testing.T) {
	t.Parallel()
	cfg := config.Get()
	reg, err := NewRegistry(cfg, RegistryOptions{AllowedReadPaths: []string{t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("asr.transcribe"); !ok {
		t.Fatalf("asr.transcribe not registered")
	}
}
