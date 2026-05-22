package runtime

import (
	"context"
	"encoding/json"
	"testing"

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
