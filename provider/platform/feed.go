package platformprov

import (
	"context"

	"github.com/originaleric/digcrawl/internal/feed"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

// FeedReadEntry returns registry entry for feed.read.
func FeedReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolFeedRead,
		"Read an RSS or Atom feed and return normalized feed entries.",
		[]string{"feed", "rss", "atom", "platform.read"},
		nil,
		configDomains,
		artStore,
		readFeed,
	)
}

func readFeed(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	doc, err := feed.Service{}.Read(ctx, in.URL)
	if err != nil {
		return nil, err
	}
	return feed.ToContent(doc, in.URL), nil
}
