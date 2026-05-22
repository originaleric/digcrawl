package browser

import (
	"context"
	"fmt"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/provider"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const Name = "browser"

// Provider exposes generic local browser tools.
type Provider struct {
	ConfigDomains []string
	ArtifactStore artifact.Store
}

func (p *Provider) Name() string { return Name }

func (p *Provider) Register(_ context.Context, reg *registry.Registry) error {
	domains := p.ConfigDomains
	if len(domains) == 0 {
		domains = config.Get().MergedAllowedDomains()
	}
	store := p.ArtifactStore
	reg.Register(BrowseEntry(domains, store))
	reg.Register(SnapshotEntry(domains))
	reg.Register(ActionEntry(domains))
	return nil
}

func (p *Provider) Health(ctx context.Context) error {
	cfg := config.Get().LocalBrowser
	if cfg.Enabled != nil && !*cfg.Enabled {
		return fmt.Errorf("local browser is disabled in config")
	}
	_ = ctx
	return nil
}

func (p *Provider) Tools(_ context.Context) ([]protocol.ToolDescriptor, error) {
	reg := registry.New()
	_ = p.Register(context.Background(), reg)
	return reg.List(), nil
}

var (
	_ provider.Provider       = (*Provider)(nil)
	_ provider.HealthReporter = (*Provider)(nil)
	_ provider.ToolManifester = (*Provider)(nil)
)
