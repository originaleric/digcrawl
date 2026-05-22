package stub

import (
	"context"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/provider"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const Name = "stub"

// Provider is a minimal third-party provider example for documentation and tests.
type Provider struct{}

func (p *Provider) Name() string { return Name }

func (p *Provider) Register(_ context.Context, reg *registry.Registry) error {
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{
			Name:        "stub.echo",
			Description: "Example third-party provider tool (echo input).",
			InputSchema: registry.MustSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]string{"type": "string"},
				},
			}),
			Capabilities: []string{"stub"},
			Risk:         "low",
		},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			_ = ctx
			return map[string]any{"provider": Name, "echo": string(call.Input)}, nil, nil
		},
	})
	return nil
}

func (p *Provider) Health(ctx context.Context) error {
	_ = ctx
	return nil
}

func (p *Provider) Tools(ctx context.Context) ([]protocol.ToolDescriptor, error) {
	reg := registry.New()
	_ = p.Register(ctx, reg)
	return reg.List(), nil
}

var (
	_ provider.Provider       = (*Provider)(nil)
	_ provider.HealthReporter = (*Provider)(nil)
	_ provider.ToolManifester = (*Provider)(nil)
)
