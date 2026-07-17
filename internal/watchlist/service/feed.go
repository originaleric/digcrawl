package service

import (
	"context"
	"net/http"

	"github.com/originaleric/digcrawl/internal/source"
)

type FeedRegistry struct {
	Client *http.Client
}

func (r FeedRegistry) ListLatest(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	items, err := source.Service{Client: r.Client}.ListLatest(ctx, sourceFromWatchItem(item))
	if err != nil {
		return nil, err
	}
	return feedItemsFromSource(items), nil
}

func sourceFromWatchItem(item WatchItem) source.Source {
	return source.Source{
		ID:       item.ID,
		Platform: item.Platform,
		Type:     item.Type,
		URL:      item.URL,
		Name:     item.Name,
		Options:  item.Options,
	}
}

func feedItemsFromSource(items []source.Item) []FeedItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]FeedItem, 0, len(items))
	for _, item := range items {
		out = append(out, FeedItem{
			ID:           item.ID,
			ContentKey:   item.ContentKey,
			URL:          item.URL,
			CanonicalURL: item.CanonicalURL,
			Title:        item.Title,
			Author:       item.Author,
			PublishedAt:  item.PublishedAt,
			ContentType:  item.ContentType,
			Raw:          item.Raw,
		})
	}
	return out
}
