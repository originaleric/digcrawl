package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/kuaishou"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var kuaishouDefaultDomains = []string{"kuaishou.com", "www.kuaishou.com", "v.kuaishou.com"}

func KuaishouVideoReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolKuaishouVideoRead,
		"Read a Kuaishou video through Web GraphQL with local-cookie browser fallback.",
		[]string{"api", "browser", "kuaishou", "cookie.local", "platform.read", "video"},
		kuaishouDefaultDomains,
		configDomains,
		artStore,
		kuaishou.ReadVideo,
		optionalRuntimeConfig(runtimeCfg),
	)
}
