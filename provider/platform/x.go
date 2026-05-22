package platformprov

import (
	xplatform "github.com/originaleric/digcrawl/internal/platform/x"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var xDefaultDomains = []string{"x.com", "twitter.com"}

// XPostReadEntry returns registry entry for x.post.read.
func XPostReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolXPostRead,
		"Read an X (Twitter) post via local browser with structured output.",
		[]string{"browser", "x", "cookie.local", "platform.read"},
		xDefaultDomains,
		configDomains,
		artStore,
		xplatform.ReadPost,
	)
}
