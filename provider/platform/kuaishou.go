package platformprov

import (
	"github.com/originaleric/digcrawl/internal/platform/kuaishou"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var kuaishouDefaultDomains = []string{"kuaishou.com", "www.kuaishou.com", "v.kuaishou.com"}

func KuaishouVideoReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolKuaishouVideoRead,
		"Read a Kuaishou video page via local browser with structured output.",
		[]string{"browser", "kuaishou", "cookie.local", "platform.read", "video"},
		kuaishouDefaultDomains,
		configDomains,
		artStore,
		kuaishou.ReadVideo,
	)
}
