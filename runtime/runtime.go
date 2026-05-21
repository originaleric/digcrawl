package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/diggate/protocol"
	"github.com/originaleric/diggate/runtime/artifact"
	"github.com/originaleric/diggate/runtime/audit"
	"github.com/originaleric/diggate/runtime/policy"
	"github.com/originaleric/diggate/runtime/registry"
	"github.com/originaleric/diggate/runtime/version"
)

// Options configures the gateway runtime.
type Options struct {
	InstanceID      string
	AllowedTools    []string
	ConfigDomains   []string
	ArtifactStore   artifact.Store
	Audit           *audit.Logger
	RequireApproval bool
}

// Runtime executes ToolCall against a tool registry.
type Runtime struct {
	reg       *registry.Registry
	opts      Options
	audit     *audit.Logger
	artifacts artifact.Store
}

// ArtifactStore returns the configured artifact store (may be nil).
func (r *Runtime) ArtifactStore() artifact.Store {
	return r.artifacts
}

func New(reg *registry.Registry, opts Options) *Runtime {
	lg := opts.Audit
	if lg == nil {
		lg = audit.NewLogger()
	}
	return &Runtime{reg: reg, opts: opts, audit: lg, artifacts: opts.ArtifactStore}
}

// Manifest builds the current tool manifest.
func (r *Runtime) Manifest() protocol.ToolManifest {
	return protocol.ToolManifest{
		Type:           protocol.TypeToolManifest,
		Runtime:        version.RuntimeName,
		RuntimeVersion: version.RuntimeVersion,
		InstanceID:     r.opts.InstanceID,
		Tools:          r.reg.List(),
	}
}

// Execute runs a tool call and returns a ToolResult.
func (r *Runtime) Execute(ctx context.Context, call *protocol.ToolCall) *protocol.ToolResult {
	start := time.Now()
	callID := ""
	if call != nil {
		callID = call.ID
	}
	result := &protocol.ToolResult{
		Type: protocol.TypeToolResult,
		ID:   callID,
	}

	defer func() {
		result.Usage.DurationMs = time.Since(start).Milliseconds()
		r.audit.LogCall(call, result)
	}()

	if err := r.validateCall(call); err != nil {
		result.Status = "error"
		result.Error = mapError(err)
		return result
	}

	entry, ok := r.reg.Get(call.Tool)
	if !ok {
		result.Status = "error"
		result.Error = &protocol.ToolError{Code: policy.CodeToolNotAllowed, Message: fmt.Sprintf("unknown tool %q", call.Tool)}
		return result
	}
	if r.opts.RequireApproval && entry.Descriptor.RequiresUserApproval && !call.Approved {
		result.Status = "error"
		result.Error = &protocol.ToolError{Code: "USER_APPROVAL_REQUIRED", Message: fmt.Sprintf("tool %q requires approval", call.Tool)}
		return result
	}

	timeout := time.Duration(call.Policy.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, artifacts, err := entry.Handler(execCtx, call)
	if err != nil {
		result.Status = "error"
		result.Error = mapError(err)
		return result
	}

	outBytes, err := json.Marshal(output)
	if err != nil {
		result.Status = "error"
		result.Error = &protocol.ToolError{Code: "INTERNAL", Message: err.Error()}
		return result
	}

	maxOut := call.Policy.MaxOutputBytes
	if maxOut > 0 && len(outBytes) > maxOut {
		result.Status = "error"
		result.Error = &protocol.ToolError{
			Code:    "OUTPUT_TOO_LARGE",
			Message: fmt.Sprintf("output exceeds max_output_bytes (%d)", maxOut),
		}
		return result
	}

	result.Status = "success"
	result.Output = outBytes
	result.Artifacts = artifacts
	return result
}

func (r *Runtime) validateCall(call *protocol.ToolCall) error {
	if call == nil {
		return fmt.Errorf("%s: nil tool call", policy.CodeInvalidInput)
	}
	if strings.TrimSpace(call.ID) == "" {
		return fmt.Errorf("%s: call id is required", policy.CodeInvalidInput)
	}
	if strings.TrimSpace(call.Tool) == "" {
		return fmt.Errorf("%s: tool name is required", policy.CodeInvalidInput)
	}
	if call.Type != "" && call.Type != protocol.TypeToolCall {
		return fmt.Errorf("%s: unexpected type %q", policy.CodeInvalidInput, call.Type)
	}
	return policy.ValidateToolAllowed(call.Tool, r.opts.AllowedTools)
}

func mapError(err error) *protocol.ToolError {
	if err == nil {
		return &protocol.ToolError{Code: "UNKNOWN", Message: "unknown error"}
	}
	msg := err.Error()
	for _, code := range []string{
		policy.CodeDomainNotAllowed,
		policy.CodeToolNotAllowed,
		policy.CodeInvalidInput,
	} {
		if strings.HasPrefix(msg, code+":") || strings.HasPrefix(msg, code) {
			return &protocol.ToolError{Code: code, Message: strings.TrimPrefix(strings.TrimPrefix(msg, code+":"), code)}
		}
	}
	return &protocol.ToolError{Code: "TOOL_EXEC_FAILED", Message: msg}
}
