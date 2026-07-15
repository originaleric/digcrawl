package reddit

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
)

func TestCollectMoreChildrenDedupesAndLimits(t *testing.T) {
	t.Parallel()
	replies := json.RawMessage(`{"data":{"children":[{"kind":"more","data":{"children":["c2","c3"]}}]}}`)
	ids := collectMoreChildren([]redditThing{
		{Kind: "more", Data: redditThingData{Children: []string{"c1", "c2"}}},
		{Kind: "t1", Data: redditThingData{Replies: replies}},
	}, 3)
	want := []string{"c1", "c2", "c3"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %+v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %+v, want %+v", ids, want)
		}
	}
}

func TestNormalizeTimeRange(t *testing.T) {
	t.Parallel()
	if got := normalizeTimeRange("week"); got != "week" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeTimeRange("forever"); got != "" {
		t.Fatalf("unsupported range should be empty, got %q", got)
	}
}

func TestUpdateRedditRateFromHeadersHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	enabled := true
	cfg := config.RedditConfig{
		APIBaseURL: "https://oauth.reddit.test",
		RateLimit: config.RedditRateLimitConfig{
			HeaderDriven:     &enabled,
			BackoffOn429:     &enabled,
			MinRetryAfterSec: 2,
		},
	}
	redditRateMu.Lock()
	delete(redditNextAt, cfg.APIBaseURLOrDefault())
	redditRateMu.Unlock()

	headers := http.Header{}
	headers.Set("retry-after", "1")
	updateRedditRateFromHeaders(cfg, headers, http.StatusTooManyRequests)

	redditRateMu.Lock()
	next := redditNextAt[cfg.APIBaseURLOrDefault()]
	redditRateMu.Unlock()
	if time.Until(next) < 1500*time.Millisecond {
		t.Fatalf("retry-after backoff not applied, next=%s", next)
	}
}
