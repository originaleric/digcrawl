package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/douyin"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var douyinDefaultDomains = []string{"douyin.com", "www.douyin.com", "iesdouyin.com", "v.douyin.com"}

// DouyinVideoReadEntry returns registry entry for douyin.video.read.
func DouyinVideoReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolDouyinVideoRead,
		"Read a Douyin video page via local browser with structured output.",
		[]string{"browser", "douyin", "cookie.local", "platform.read"},
		douyinDefaultDomains,
		configDomains,
		artStore,
		douyin.ReadVideo,
		optionalRuntimeConfig(runtimeCfg),
	)
}
