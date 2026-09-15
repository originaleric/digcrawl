package platformprov

import (
	"context"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/provider"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

const Name = "platform"

// Provider registers platform content read tools.
type Provider struct {
	Config        *config.Config
	ConfigDomains []string
	ArtifactStore artifact.Store
}

func (p *Provider) Name() string { return Name }

func (p *Provider) Register(_ context.Context, reg *registry.Registry) error {
	domains := p.ConfigDomains
	if len(domains) == 0 {
		cfg := p.Config
		if cfg == nil {
			cfg = config.Get()
		}
		domains = cfg.Gateway.AllowedDomains
	}
	store := p.ArtifactStore
	reg.Register(WechatArticleReadEntry(domains, store, p.Config))
	reg.Register(XiaohongshuNoteReadEntry(domains, store, p.Config))
	reg.Register(DouyinVideoReadEntry(domains, store, p.Config))
	reg.Register(XPostReadEntry(domains, store, p.Config))
	reg.Register(ToutiaoArticleReadEntry(domains, store, p.Config))
	reg.Register(ToutiaoVideoReadEntry(domains, store, p.Config))
	reg.Register(FeedReadEntry(domains, store))
	reg.Register(RedditPostReadEntry(domains, store, p.Config))
	reg.Register(RedditSubredditReadEntry(domains))
	reg.Register(RedditUserReadEntry(domains))
	reg.Register(ThreadsPostReadEntry(domains, store, p.Config))
	reg.Register(InstagramContentReadEntry(domains, store, p.Config))
	reg.Register(BilibiliVideoReadEntry(domains, store, p.Config))
	reg.Register(WeiboPostReadEntry(domains, store, p.Config))
	reg.Register(KuaishouVideoReadEntry(domains, store, p.Config))
	reg.Register(ZhihuContentReadEntry(domains, store, p.Config))
	reg.Register(YouTubeVideoReadEntry(domains, store, p.Config))
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
