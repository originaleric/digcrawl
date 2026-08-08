package asrprov

import (
	"context"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/provider"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const Name = "asr"

// Provider registers ASR transcription tools.
type Provider struct {
	Config            config.ASRConfig
	AllowedReadPaths  []string
	SkillProfilePath  string
	DisableTranscribe bool
}

func (p *Provider) Name() string { return Name }

func (p *Provider) Register(_ context.Context, reg *registry.Registry) error {
	cfg := p.Config
	if cfg.Provider == "" {
		cfg = config.Get().ASR
	}
	if !p.DisableTranscribe {
		reg.Register(TranscribeEntry(cfg, p.AllowedReadPaths))
	}
	if config.IsDefaultSkillProfile(p.SkillProfilePath) {
		reg.Register(EnableSkillASREntry())
	}
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
