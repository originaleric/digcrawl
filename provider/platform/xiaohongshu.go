package platformprov

import (
	"github.com/originaleric/digcrawl/internal/platform/xiaohongshu"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var xiaohongshuDefaultDomains = []string{"xiaohongshu.com", "www.xiaohongshu.com", "xhslink.com"}

// XiaohongshuNoteReadEntry returns registry entry for xiaohongshu.note.read.
func XiaohongshuNoteReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolXiaohongshuNoteRead,
		"Read a Xiaohongshu (RED) note via local browser with structured output.",
		[]string{"browser", "xiaohongshu", "cookie.local", "platform.read"},
		xiaohongshuDefaultDomains,
		configDomains,
		artStore,
		xiaohongshu.ReadNote,
	)
}
