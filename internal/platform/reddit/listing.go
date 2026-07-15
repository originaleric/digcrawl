package reddit

import (
	"context"

	"github.com/originaleric/digcrawl/config"
)

func ReadSubreddit(ctx context.Context, opts ListingOptions) (*ListingResult, error) {
	cfg := config.Get().Platform.Reddit
	endpoint, err := subredditListingURL(cfg, opts)
	if err != nil {
		return nil, err
	}
	res, _, _, err := fetchListing(ctx, cfg, endpoint)
	return res, err
}

func ReadUser(ctx context.Context, opts ListingOptions) (*ListingResult, error) {
	cfg := config.Get().Platform.Reddit
	endpoint, err := userListingURL(cfg, opts)
	if err != nil {
		return nil, err
	}
	res, _, _, err := fetchListing(ctx, cfg, endpoint)
	return res, err
}
