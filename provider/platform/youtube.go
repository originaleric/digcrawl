package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/youtube"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var youtubeDefaultDomains = []string{"youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be", "www.youtu.be"}

func YouTubeVideoReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg *config.Config) registry.Entry {
	entry := platformReadEntry(
		ToolYouTubeVideoRead,
		"Read a public YouTube video page and its available captions with structured output.",
		[]string{"browser", "youtube", "platform.read", "video"},
		youtubeDefaultDomains,
		configDomains,
		artStore,
		youtube.ReadVideo,
	)
	entry.Handler = platformReadHandlerNamedWithConfig(ToolYouTubeVideoRead, youtubeDefaultDomains, configDomains, artStore, youtube.ReadVideo, runtimeCfg)
	return entry
}
