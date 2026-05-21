package platformprov

import (
	"github.com/originaleric/diggate/internal/platform/toutiao"
	"github.com/originaleric/diggate/runtime/artifact"
	"github.com/originaleric/diggate/runtime/registry"
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
