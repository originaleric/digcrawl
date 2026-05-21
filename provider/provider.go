package provider

import (
	"context"

	"github.com/originaleric/diggate/gateway/registry"
)

// Provider registers tool entries into a gateway registry.
// DigEino and other tool packs implement this interface.
type Provider interface {
	Name() string
	Register(ctx context.Context, reg *registry.Registry) error
}

// Loader applies a list of providers to a registry.
func Load(ctx context.Context, reg *registry.Registry, providers ...Provider) error {
	for _, p := range providers {
		if p == nil {
			continue
		}
		if err := p.Register(ctx, reg); err != nil {
			return err
		}
	}
	return nil
}
