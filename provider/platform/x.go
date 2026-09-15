package platformprov

import (
	"context"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
	xplatform "github.com/originaleric/digcrawl/internal/platform/x"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var xDefaultDomains = []string{"x.com", "twitter.com"}

// XPostReadEntry returns registry entry for x.post.read.
func XPostReadEntry(configDomains []string, artStore artifact.Store, runtimeConfigs ...*config.Config) registry.Entry {
	runtimeCfg := optionalRuntimeConfig(runtimeConfigs)
	read := xplatform.ReadPost
	if runtimeCfg != nil {
		xCfg := runtimeCfg.Platform.X
		read = func(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
			return xplatform.ReadPostWithConfig(ctx, in, xCfg)
		}
	}
	return platformReadEntryWithConfig(
		ToolXPostRead,
		"Read a public X (Twitter) post through public syndication with API, oEmbed, and local-browser fallbacks.",
		[]string{"browser", "x", "cookie.local", "platform.read"},
		xDefaultDomains,
		configDomains,
		artStore,
		read,
		runtimeCfg,
	)
}
