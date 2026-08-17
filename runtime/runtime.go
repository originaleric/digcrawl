package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/audit"
	"github.com/originaleric/digcrawl/runtime/policy"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/runtime/version"
)

// Options configures the gateway runtime.
type Options struct {
	InstanceID      string
	AllowedTools    []string
	ConfigDomains   []string
	ArtifactStore   artifact.Store
	Audit           *audit.Logger
	RequireApproval bool
	// TimeoutForCall supplies a configuration-aware default when a transport
	// did not set ToolCall.Policy.TimeoutMs explicitly.
	TimeoutForCall func(*protocol.ToolCall) time.Duration
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
		Tools:          r.allowedToolDescriptors(),
	}
}

// allowedToolDescriptors returns exactly the tools that this runtime will accept.
// The manifest is consumed by MCP and HTTP clients as the callable capability set,
// so it must not advertise tools rejected later by the runtime allowlist.
func (r *Runtime) allowedToolDescriptors() []protocol.ToolDescriptor {
	tools := r.reg.List()
	if len(r.opts.AllowedTools) == 0 {
		return tools
	}
	allowed := make(map[string]struct{}, len(r.opts.AllowedTools))
	for _, name := range r.opts.AllowedTools {
		allowed[strings.TrimSpace(name)] = struct{}{}
	}
	out := make([]protocol.ToolDescriptor, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Name]; ok {
			out = append(out, tool)
		}
	}
	return out
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

	timeout := r.ExecutionTimeout(call)
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, artifacts, err := entry.Handler(execCtx, call)
	if err != nil {
		result.Status = "error"
		result.Error = mapError(err)
		if len(output) > 0 {
			if outBytes, marshalErr := json.Marshal(output); marshalErr == nil {
				if maxOut := call.Policy.MaxOutputBytes; maxOut <= 0 || len(outBytes) <= maxOut {
					result.Output = outBytes
					result.Artifacts = artifacts
				}
			}
		}
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

// ExecutionTimeout resolves the effective runtime deadline without modifying a
// call. HTTP uses it to align its response write deadline with Runtime's
// context, while MCP and Collector rely on the same value through Execute.
func (r *Runtime) ExecutionTimeout(call *protocol.ToolCall) time.Duration {
	if call != nil && call.Policy.TimeoutMs > 0 {
		return time.Duration(call.Policy.TimeoutMs) * time.Millisecond
	}
	if r != nil && r.opts.TimeoutForCall != nil {
		if timeout := r.opts.TimeoutForCall(call); timeout > 0 {
			return timeout
		}
	}
	return 60 * time.Second
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
	var coded interface {
		ToolErrorCode() string
		ToolErrorMessage() string
	}
	if errors.As(err, &coded) && strings.TrimSpace(coded.ToolErrorCode()) != "" {
		msg := strings.TrimSpace(coded.ToolErrorMessage())
		if msg == "" {
			msg = err.Error()
		}
		mapped := &protocol.ToolError{Code: strings.TrimSpace(coded.ToolErrorCode()), Message: msg}
		var detailed interface {
			ToolErrorDetails() map[string]any
		}
		if errors.As(err, &detailed) {
			mapped.Details = detailed.ToolErrorDetails()
		}
		return mapped
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
