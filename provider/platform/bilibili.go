package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/bilibili"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var bilibiliDefaultDomains = []string{"bilibili.com", "www.bilibili.com", "m.bilibili.com", "space.bilibili.com", "b23.tv"}

func BilibiliVideoReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolBilibiliVideoRead,
		"Read a Bilibili video page API-first with local browser fallback and structured output.",
		[]string{"browser", "bilibili", "cookie.local", "platform.read", "video"},
		bilibiliDefaultDomains,
		configDomains,
		artStore,
		bilibili.ReadVideo,
		optionalRuntimeConfig(runtimeCfg),
	)
}
