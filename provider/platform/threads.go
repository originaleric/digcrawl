package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/threads"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var threadsDefaultDomains = []string{"threads.net", "www.threads.net", "threads.com", "www.threads.com"}

func ThreadsPostReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolThreadsPostRead,
		"Read a Threads post via local browser with structured output.",
		[]string{"browser", "threads", "platform.read"},
		threadsDefaultDomains,
		configDomains,
		artStore,
		threads.ReadPost,
		optionalRuntimeConfig(runtimeCfg),
	)
}
