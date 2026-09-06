package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/instagram"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var instagramDefaultDomains = []string{"instagram.com", "www.instagram.com"}

func InstagramContentReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolInstagramContentRead,
		"Read an Instagram post or reel via local browser with structured output.",
		[]string{"browser", "instagram", "platform.read"},
		instagramDefaultDomains,
		configDomains,
		artStore,
		instagram.ReadContent,
		optionalRuntimeConfig(runtimeCfg),
	)
}
