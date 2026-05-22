package platformprov

import (
	"github.com/originaleric/digcrawl/internal/platform/toutiao"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var toutiaoDefaultDomains = []string{"toutiao.com", "www.toutiao.com", "m.toutiao.com"}

// ToutiaoArticleReadEntry returns registry entry for toutiao.article.read.
func ToutiaoArticleReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolToutiaoArticleRead,
		"Read a Toutiao article page via local browser with structured output.",
		[]string{"browser", "toutiao", "cookie.local", "platform.read"},
		toutiaoDefaultDomains,
		configDomains,
		artStore,
		toutiao.ReadArticle,
	)
}
