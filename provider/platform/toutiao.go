package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/toutiao"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var toutiaoDefaultDomains = []string{"toutiao.com", "www.toutiao.com", "m.toutiao.com"}

// ToutiaoArticleReadEntry returns registry entry for toutiao.article.read.
func ToutiaoArticleReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolToutiaoArticleRead,
		"Read a Toutiao article page via local browser with structured output.",
		[]string{"browser", "toutiao", "cookie.local", "platform.read"},
		toutiaoDefaultDomains,
		configDomains,
		artStore,
		toutiao.ReadArticle,
		optionalRuntimeConfig(runtimeCfg),
	)
}

// ToutiaoVideoReadEntry returns registry entry for toutiao.video.read.
func ToutiaoVideoReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolToutiaoVideoRead,
		"Read a Toutiao video page via local browser with structured output and media URLs.",
		[]string{"browser", "toutiao", "cookie.local", "platform.read", "video"},
		toutiaoDefaultDomains,
		configDomains,
		artStore,
		toutiao.ReadVideo,
		optionalRuntimeConfig(runtimeCfg),
	)
}
