package platformprov

import (
	"github.com/originaleric/diggate/internal/platform/douyin"
	"github.com/originaleric/diggate/runtime/artifact"
	"github.com/originaleric/diggate/runtime/registry"
)

var douyinDefaultDomains = []string{"douyin.com", "www.douyin.com", "iesdouyin.com", "v.douyin.com"}

// DouyinVideoReadEntry returns registry entry for douyin.video.read.
func DouyinVideoReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolDouyinVideoRead,
		"Read a Douyin video page via local browser with structured output.",
		[]string{"browser", "douyin", "cookie.local", "platform.read"},
		douyinDefaultDomains,
		configDomains,
		artStore,
		douyin.ReadVideo,
	)
}
