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
	ConfigDomains []string
	ArtifactStore artifact.Store
}

func (p *Provider) Name() string { return Name }

func (p *Provider) Register(_ context.Context, reg *registry.Registry) error {
	domains := p.ConfigDomains
	if len(domains) == 0 {
		domains = config.Get().LocalBrowser.AllowedDomains
	}
	store := p.ArtifactStore
	reg.Register(WechatArticleReadEntry(domains, store))
	reg.Register(XiaohongshuNoteReadEntry(domains, store))
	reg.Register(DouyinVideoReadEntry(domains, store))
	reg.Register(XPostReadEntry(domains, store))
	reg.Register(ToutiaoArticleReadEntry(domains, store))
	reg.Register(ToutiaoVideoReadEntry(domains, store))
	reg.Register(FeedReadEntry(domains, store))
	reg.Register(RedditPostReadEntry(domains, store))
	reg.Register(RedditSubredditReadEntry(domains))
	reg.Register(RedditUserReadEntry(domains))
	reg.Register(BilibiliVideoReadEntry(domains, store))
	reg.Register(WeiboPostReadEntry(domains, store))
	reg.Register(KuaishouVideoReadEntry(domains, store))
	reg.Register(ZhihuContentReadEntry(domains, store))
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
