package provider

import (
	"context"

	"github.com/originaleric/diggate/protocol"
	"github.com/originaleric/diggate/runtime/registry"
)

// Provider registers tool entries into a gateway registry.
type Provider interface {
	Name() string
	Register(ctx context.Context, reg *registry.Registry) error
}

// HealthReporter optional provider health check.
type HealthReporter interface {
	Health(ctx context.Context) error
}

// ToolManifester optional tool descriptor listing without full registration.
type ToolManifester interface {
	Tools(ctx context.Context) ([]protocol.ToolDescriptor, error)
}

// Loader applies providers; runs optional Health on each HealthReporter.
func Load(ctx context.Context, reg *registry.Registry, providers ...Provider) error {
	for _, p := range providers {
		if p == nil {
			continue
		}
		if hr, ok := p.(HealthReporter); ok {
			if err := hr.Health(ctx); err != nil {
				return err
			}
		}
		if err := p.Register(ctx, reg); err != nil {
			return err
		}
	}
	return nil
}
