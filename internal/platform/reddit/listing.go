package reddit

import (
	"context"
	"fmt"

	"github.com/originaleric/digcrawl/config"
)

func ReadSubreddit(ctx context.Context, opts ListingOptions) (*ListingResult, error) {
	cfg := config.Get().Platform.Reddit
	endpoint, err := subredditListingURL(cfg, opts)
	if err != nil {
		return nil, err
	}
	res, _, _, err := fetchListing(ctx, cfg, endpoint)
	if err == nil {
		return res, nil
	}
	rssRes, rssErr := readSubredditViaRSS(ctx, opts)
	if rssErr == nil {
		return rssRes, nil
	}
	return nil, fmt.Errorf("reddit subreddit API failed: %v; RSS fallback failed: %w", err, rssErr)
}

func ReadUser(ctx context.Context, opts ListingOptions) (*ListingResult, error) {
	cfg := config.Get().Platform.Reddit
	endpoint, err := userListingURL(cfg, opts)
	if err != nil {
		return nil, err
	}
	res, _, _, err := fetchListing(ctx, cfg, endpoint)
	if err == nil {
		return res, nil
	}
	rssRes, rssErr := readUserViaRSS(ctx, opts)
	if rssErr == nil {
		return rssRes, nil
	}
	return nil, fmt.Errorf("reddit user API failed: %v; RSS fallback failed: %w", err, rssErr)
}
