package example

import (
	"context"
	"os"
	"strings"

	"github.com/originaleric/diggate/runtime/executor"
	"github.com/originaleric/diggate/protocol"
	"github.com/originaleric/diggate/runtime/registry"
	"github.com/originaleric/diggate/provider"
)

const Name = "example"

// Provider exposes minimal built-in tools for host integration testing.
type Provider struct {
	AllowedReadPaths []string
}

func (p *Provider) Name() string { return Name }

func (p *Provider) Register(_ context.Context, reg *registry.Registry) error {
	reg.Register(pingEntry())
	if len(p.allowedReadPaths()) > 0 {
		reg.Register(fileReadEntry(p.allowedReadPaths()))
	}
	return nil
}

func (p *Provider) allowedReadPaths() []string {
	return p.AllowedReadPaths
}

var _ provider.Provider = (*Provider)(nil)

const toolPing = "gateway.ping"

func pingEntry() registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        toolPing,
			Description: "Health check tool that returns gateway runtime info.",
			InputSchema: registry.MustSchema(map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}),
			Capabilities: []string{"gateway.meta"},
			Risk:         "low",
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			_ = ctx
			_ = call
			return map[string]any{"ok": true, "message": "pong"}, nil, nil
		},
	}
}

const toolFileRead = "file.read"

type fileReadInput struct {
	Path string `json:"path"`
}

func fileReadEntry(allowedPaths []string) registry.Entry {
	return registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        toolFileRead,
			Description: "Read a local file from an allowed directory prefix.",
			InputSchema: registry.MustSchema(map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path": map[string]string{"type": "string"},
				},
			}),
			Capabilities:         []string{"file.local"},
			Risk:                 "filesystem",
			RequiresUserApproval: true,
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			_ = ctx
			in, err := decodeInput[fileReadInput](call)
			if err != nil {
				return nil, nil, err
			}
			if err := executor.ValidateReadPath(in.Path, allowedPaths); err != nil {
				return nil, nil, err
			}
			data, err := os.ReadFile(strings.TrimSpace(in.Path))
			if err != nil {
				return nil, nil, err
			}
			return map[string]any{
				"path":    in.Path,
				"content": string(data),
			}, nil, nil
		},
	}
}

func decodeInput[T any](call *protocol.ToolCall) (T, error) {
	return executor.DecodeInput[T](call)
}
