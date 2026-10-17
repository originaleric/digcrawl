package reddit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

var (
	redditHTTPClient = http.DefaultClient
	redditTokenMu    sync.Mutex
	redditTokenCache = map[string]cachedToken{}
	redditRateMu     sync.Mutex
	redditNextAt     = map[string]time.Time{}
)

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

type PostReadOptions struct {
	IncludeComments bool
	CommentLimit    int
	CommentDepth    int
	CommentSort     string
	ExpandMore      bool
	ExpandMoreLimit int
}

type ListingOptions struct {
	Subreddit string
	Username  string
	Sort      string
	TimeRange string
	Where     string
	Limit     int
	After     string
}

type ListingItem struct {
	ID           string
	Kind         string
	Fullname     string
	Subreddit    string
	Author       string
	Title        string
	Body         string
	URL          string
	Permalink    string
	CreatedUTC   int64
	Score        int
	NumComments  int
	Over18       bool
	Stickied     bool
	ContentKey   string
	CanonicalURL string
}

type ListingResult struct {
	Items []ListingItem
	After string
	// SourceURL is the confirmed page source for an RSS fallback. API results
	// use the configured API backend and do not masquerade as page reads.
	SourceURL string `json:"-"`
}

type redditListing struct {
	Data struct {
		Children []redditThing `json:"children"`
		After    string        `json:"after"`
	} `json:"data"`
}

type redditThing struct {
	Kind string          `json:"kind"`
	Data redditThingData `json:"data"`
}

type redditThingData struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Subreddit     string          `json:"subreddit"`
	Author        string          `json:"author"`
	Title         string          `json:"title"`
	SelfText      string          `json:"selftext"`
	Body          string          `json:"body"`
	URL           string          `json:"url"`
	Permalink     string          `json:"permalink"`
	CreatedUTC    float64         `json:"created_utc"`
	Score         int             `json:"score"`
	Ups           int             `json:"ups"`
	Downs         int             `json:"downs"`
	UpvoteRatio   float64         `json:"upvote_ratio"`
	NumComments   int             `json:"num_comments"`
	Over18        bool            `json:"over_18"`
	Spoiler       bool            `json:"spoiler"`
	Stickied      bool            `json:"stickied"`
	Locked        bool            `json:"locked"`
	IsSelf        bool            `json:"is_self"`
	LinkFlairText string          `json:"link_flair_text"`
	Thumbnail     string          `json:"thumbnail"`
	ParentID      string          `json:"parent_id"`
	Replies       json.RawMessage `json:"replies"`
	Children      []string        `json:"children"`
	Count         int             `json:"count"`
	Preview       struct {
		Images []struct {
			Source struct {
				URL string `json:"url"`
			} `json:"source"`
		} `json:"images"`
	} `json:"preview"`
}

func fetchPostViaAPI(ctx context.Context, norm *platform.NormalizedURL, cfg config.RedditConfig, opts PostReadOptions) (*redditThing, []redditThing, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		attempt := redditAPIFallbackAttempt(0, "", "missing_post_id", "missing reddit post id")
		return nil, nil, attempt, 0, attempt.Error()
	}
	if !cfg.EnabledOrDefault() {
		attempt := redditAPIFallbackAttempt(0, "", "api_disabled", "Reddit API disabled")
		return nil, nil, attempt, 0, attempt.Error()
	}

	apiCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.APITimeoutSecOrDefault())*time.Second)
	defer cancel()
	start := time.Now()
	token, attempt, err := redditBearerToken(apiCtx, cfg)
	if err != nil {
		return nil, nil, attempt, time.Since(start).Milliseconds(), err
	}

	endpoint, err := redditPostURL(cfg, norm.PlatformID, opts)
	if err != nil {
		attempt := redditAPIFallbackAttempt(0, "", "endpoint_build_failed", err.Error())
		return nil, nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	var lastAttempt *platform.APIAttempt
	var lastErr error
	totalAttempts := cfg.APIMaxRetriesOrDefault() + 1
	for i := 0; i < totalAttempts; i++ {
		post, comments, attempt, err := fetchPostOnce(apiCtx, endpoint, token, cfg)
		if attempt != nil {
			attempt.Meta["api_retry_index"] = i
		}
		if err == nil && post != nil {
			if opts.ExpandMore && opts.ExpandMoreLimit > 0 {
				expanded, expandAttempt, expandErr := expandMoreComments(apiCtx, cfg, token, post.Data.ID, comments, opts)
				if expandAttempt != nil && attempt != nil {
					attempt.Meta["morechildren_status"] = expandAttempt.HTTPStatus
					if msg := strings.TrimSpace(expandAttempt.Message); msg != "" {
						attempt.Meta["morechildren_message"] = msg
					}
				}
				if expandErr == nil && len(expanded) > 0 {
					comments = append(comments, expanded...)
				} else if expandErr != nil && attempt != nil {
					attempt.Meta["morechildren_error"] = expandErr.Error()
				}
			}
			return post, comments, attempt, time.Since(start).Milliseconds(), nil
		}
		lastAttempt, lastErr = attempt, err
		if attempt == nil || attempt.Fatal || attempt.HTTPStatus < 500 {
			break
		}
	}
	if lastAttempt == nil {
		lastAttempt = redditAPIFallbackAttempt(0, endpoint, "unknown", "Reddit API failed")
		lastErr = lastAttempt.Error()
	}
	return nil, nil, lastAttempt, time.Since(start).Milliseconds(), lastErr
}

func fetchListing(ctx context.Context, cfg config.RedditConfig, endpoint string) (*ListingResult, *platform.APIAttempt, int64, error) {
	if !cfg.EnabledOrDefault() {
		attempt := redditAPIFallbackAttempt(0, "", "api_disabled", "Reddit API disabled")
		return nil, attempt, 0, attempt.Error()
	}
	apiCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.APITimeoutSecOrDefault())*time.Second)
	defer cancel()
	start := time.Now()
	token, attempt, err := redditBearerToken(apiCtx, cfg)
	if err != nil {
		return nil, attempt, time.Since(start).Milliseconds(), err
	}
	var lastAttempt *platform.APIAttempt
	var lastErr error
	totalAttempts := cfg.APIMaxRetriesOrDefault() + 1
	for i := 0; i < totalAttempts; i++ {
		res, attempt, err := fetchListingOnce(apiCtx, endpoint, token, cfg)
		if attempt != nil {
			attempt.Meta["api_retry_index"] = i
		}
		if err == nil && res != nil {
			return res, attempt, time.Since(start).Milliseconds(), nil
		}
		lastAttempt, lastErr = attempt, err
		if attempt == nil || attempt.Fatal || attempt.HTTPStatus < 500 {
			break
		}
	}
	if lastAttempt == nil {
		lastAttempt = redditAPIFallbackAttempt(0, endpoint, "unknown", "Reddit listing failed")
		lastErr = lastAttempt.Error()
	}
	return nil, lastAttempt, time.Since(start).Milliseconds(), lastErr
}

func redditBearerToken(ctx context.Context, cfg config.RedditConfig) (string, *platform.APIAttempt, error) {
	clientID := os.ExpandEnv(strings.TrimSpace(cfg.ClientID))
	clientSecret := os.ExpandEnv(strings.TrimSpace(cfg.ClientSecret))
	if clientID == "" {
		attempt := redditAPIFatalAttempt(platform.ErrLoginRequired, 0, cfg.TokenURLOrDefault(), "missing_client_id", "Reddit API client id is empty")
		return "", attempt, attempt.Error()
	}
	cacheKey := cfg.TokenURLOrDefault() + "|" + clientID + "|" + cfg.OAuthModeOrDefault()
	redditTokenMu.Lock()
	cached := redditTokenCache[cacheKey]
	redditTokenMu.Unlock()
	if cached.Token != "" && time.Now().Before(cached.ExpiresAt.Add(-30*time.Second)) {
		return cached.Token, nil, nil
	}

	form := url.Values{}
	switch cfg.OAuthModeOrDefault() {
	case "refresh_token":
		refresh := os.ExpandEnv(strings.TrimSpace(cfg.RefreshToken))
		if refresh == "" {
			attempt := redditAPIFatalAttempt(platform.ErrLoginRequired, 0, cfg.TokenURLOrDefault(), "missing_refresh_token", "Reddit refresh token is empty")
			return "", attempt, attempt.Error()
		}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", refresh)
	default:
		if clientSecret == "" {
			attempt := redditAPIFatalAttempt(platform.ErrLoginRequired, 0, cfg.TokenURLOrDefault(), "missing_client_secret", "Reddit API client secret is empty")
			return "", attempt, attempt.Error()
		}
		form.Set("grant_type", "client_credentials")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURLOrDefault(), bytes.NewBufferString(form.Encode()))
	if err != nil {
		attempt := redditAPIFallbackAttempt(0, cfg.TokenURLOrDefault(), "request_build_failed", err.Error())
		return "", attempt, attempt.Error()
	}
	credential := url.QueryEscape(clientID) + ":" + url.QueryEscape(clientSecret)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", cfg.UserAgentOrDefault())
	resp, err := redditHTTPClient.Do(req)
	if err != nil {
		attempt := redditAPIFallbackAttempt(0, cfg.TokenURLOrDefault(), "token_request_failed", err.Error())
		return "", attempt, attempt.Error()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		attempt := redditAPIFallbackAttempt(resp.StatusCode, cfg.TokenURLOrDefault(), "token_read_failed", "Reddit token response could not be read")
		return "", attempt, attempt.Error()
	}
	if attempt := classifyRedditHTTPStatus(cfg.TokenURLOrDefault(), resp.StatusCode, nil); attempt != nil {
		return "", attempt, attempt.Error()
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		attempt := redditAPIFallbackAttempt(resp.StatusCode, cfg.TokenURLOrDefault(), "token_decode_failed", "Reddit token response is not valid JSON")
		return "", attempt, attempt.Error()
	}
	token := strings.TrimSpace(parsed.AccessToken)
	if token == "" {
		attempt := redditAPIFallbackAttempt(resp.StatusCode, cfg.TokenURLOrDefault(), "token_missing", "Reddit token response missing access_token")
		return "", attempt, attempt.Error()
	}
	expiresIn := parsed.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	redditTokenMu.Lock()
	redditTokenCache[cacheKey] = cachedToken{Token: token, ExpiresAt: time.Now().Add(time.Duration(expiresIn) * time.Second)}
	redditTokenMu.Unlock()
	return token, nil, nil
}

func redditPostURL(cfg config.RedditConfig, id string, opts PostReadOptions) (string, error) {
	base, err := url.Parse(cfg.APIBaseURLOrDefault())
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/comments/" + strings.TrimPrefix(id, "t3_")
	q := base.Query()
	q.Set("raw_json", "1")
	q.Set("limit", strconv.Itoa(opts.CommentLimit))
	q.Set("depth", strconv.Itoa(opts.CommentDepth))
	q.Set("sort", opts.CommentSort)
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func subredditListingURL(cfg config.RedditConfig, opts ListingOptions) (string, error) {
	sub := normalizeSubredditName(opts.Subreddit)
	if sub == "" {
		return "", fmt.Errorf("subreddit is required")
	}
	sort := normalizeListingSort(opts.Sort)
	base, err := url.Parse(cfg.APIBaseURLOrDefault())
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/r/" + sub + "/" + sort
	q := base.Query()
	q.Set("raw_json", "1")
	q.Set("limit", strconv.Itoa(normalizeLimit(opts.Limit)))
	if t := normalizeTimeRange(opts.TimeRange); t != "" {
		q.Set("t", t)
	}
	if after := strings.TrimSpace(opts.After); after != "" {
		q.Set("after", after)
	}
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func moreChildrenURL(cfg config.RedditConfig, postID string, children []string, opts PostReadOptions) (string, error) {
	base, err := url.Parse(cfg.APIBaseURLOrDefault())
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/morechildren"
	q := base.Query()
	q.Set("api_type", "json")
	q.Set("raw_json", "1")
	q.Set("link_id", "t3_"+strings.TrimPrefix(postID, "t3_"))
	q.Set("children", strings.Join(children, ","))
	q.Set("sort", opts.CommentSort)
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func userListingURL(cfg config.RedditConfig, opts ListingOptions) (string, error) {
	user := normalizeUsername(opts.Username)
	if user == "" {
		return "", fmt.Errorf("username is required")
	}
	where := strings.ToLower(strings.TrimSpace(opts.Where))
	switch where {
	case "comments", "overview", "submitted":
	default:
		where = "submitted"
	}
	base, err := url.Parse(cfg.APIBaseURLOrDefault())
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/user/" + user + "/" + where
	q := base.Query()
	q.Set("raw_json", "1")
	q.Set("limit", strconv.Itoa(normalizeLimit(opts.Limit)))
	if after := strings.TrimSpace(opts.After); after != "" {
		q.Set("after", after)
	}
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func fetchPostOnce(ctx context.Context, endpoint, token string, cfg config.RedditConfig) (*redditThing, []redditThing, *platform.APIAttempt, error) {
	data, status, headers, err := doRedditGET(ctx, endpoint, token, cfg)
	attempt := classifyRedditHTTPStatus(endpoint, status, headers)
	if err != nil {
		if attempt == nil {
			attempt = redditAPIFallbackAttempt(status, endpoint, "request_failed", err.Error())
		}
		return nil, nil, attempt, attempt.Error()
	}
	if attempt != nil {
		return nil, nil, attempt, attempt.Error()
	}
	var listings []redditListing
	if err := json.Unmarshal(data, &listings); err != nil {
		attempt := redditAPIFallbackAttempt(status, endpoint, "decode_failed", "Reddit post response is not valid JSON")
		return nil, nil, attempt, attempt.Error()
	}
	if len(listings) == 0 || len(listings[0].Data.Children) == 0 {
		attempt := redditAPIFallbackAttempt(status, endpoint, "empty_post", "Reddit post response missing post")
		return nil, nil, attempt, attempt.Error()
	}
	post := listings[0].Data.Children[0]
	var comments []redditThing
	if len(listings) > 1 {
		comments = listings[1].Data.Children
	}
	ok := redditAPIOKAttempt(endpoint, status, headers)
	return &post, comments, ok, nil
}

func fetchListingOnce(ctx context.Context, endpoint, token string, cfg config.RedditConfig) (*ListingResult, *platform.APIAttempt, error) {
	data, status, headers, err := doRedditGET(ctx, endpoint, token, cfg)
	attempt := classifyRedditHTTPStatus(endpoint, status, headers)
	if err != nil {
		if attempt == nil {
			attempt = redditAPIFallbackAttempt(status, endpoint, "request_failed", err.Error())
		}
		return nil, attempt, attempt.Error()
	}
	if attempt != nil {
		return nil, attempt, attempt.Error()
	}
	var listing redditListing
	if err := json.Unmarshal(data, &listing); err != nil {
		attempt := redditAPIFallbackAttempt(status, endpoint, "decode_failed", "Reddit listing response is not valid JSON")
		return nil, attempt, attempt.Error()
	}
	items := make([]ListingItem, 0, len(listing.Data.Children))
	for _, child := range listing.Data.Children {
		if child.Kind != "t3" && child.Kind != "t1" {
			continue
		}
		items = append(items, listingItemFromThing(child))
	}
	ok := redditAPIOKAttempt(endpoint, status, headers)
	return &ListingResult{Items: items, After: listing.Data.After}, ok, nil
}

func expandMoreComments(ctx context.Context, cfg config.RedditConfig, token, postID string, comments []redditThing, opts PostReadOptions) ([]redditThing, *platform.APIAttempt, error) {
	children := collectMoreChildren(comments, opts.ExpandMoreLimit)
	if len(children) == 0 {
		return nil, nil, nil
	}
	endpoint, err := moreChildrenURL(cfg, postID, children, opts)
	if err != nil {
		attempt := redditAPIFallbackAttempt(0, "", "morechildren_endpoint_failed", err.Error())
		return nil, attempt, attempt.Error()
	}
	data, status, headers, err := doRedditGET(ctx, endpoint, token, cfg)
	attempt := classifyRedditHTTPStatus(endpoint, status, headers)
	if err != nil {
		if attempt == nil {
			attempt = redditAPIFallbackAttempt(status, endpoint, "morechildren_request_failed", err.Error())
		}
		return nil, attempt, attempt.Error()
	}
	if attempt != nil {
		return nil, attempt, attempt.Error()
	}
	var parsed struct {
		JSON struct {
			Data struct {
				Things []redditThing `json:"things"`
			} `json:"data"`
		} `json:"json"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		attempt := redditAPIFallbackAttempt(status, endpoint, "morechildren_decode_failed", "Reddit morechildren response is not valid JSON")
		return nil, attempt, attempt.Error()
	}
	out := make([]redditThing, 0, len(parsed.JSON.Data.Things))
	for _, thing := range parsed.JSON.Data.Things {
		if thing.Kind == "t1" {
			out = append(out, thing)
		}
	}
	return out, redditAPIOKAttempt(endpoint, status, headers), nil
}

func collectMoreChildren(comments []redditThing, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, limit)
	var walk func([]redditThing)
	walk = func(items []redditThing) {
		for _, item := range items {
			if len(out) >= limit {
				return
			}
			if item.Kind == "more" {
				for _, id := range item.Data.Children {
					id = strings.TrimSpace(id)
					if id == "" || seen[id] {
						continue
					}
					seen[id] = true
					out = append(out, id)
					if len(out) >= limit {
						return
					}
				}
				continue
			}
			if len(item.Data.Replies) == 0 || string(item.Data.Replies) == `""` {
				continue
			}
			var listing redditListing
			if err := json.Unmarshal(item.Data.Replies, &listing); err == nil {
				walk(listing.Data.Children)
			}
		}
	}
	walk(comments)
	return out
}

func doRedditGET(ctx context.Context, endpoint, token string, cfg config.RedditConfig) ([]byte, int, http.Header, error) {
	if err := waitRedditRate(cfg); err != nil {
		return nil, 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgentOrDefault())
	resp, err := redditHTTPClient.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	updateRedditRateFromHeaders(cfg, resp.Header, resp.StatusCode)
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return data, resp.StatusCode, resp.Header, err
}

func waitRedditRate(cfg config.RedditConfig) error {
	rpm := cfg.RateLimit.RequestsPerMinute
	if rpm <= 0 {
		return nil
	}
	key := cfg.APIBaseURLOrDefault()
	redditRateMu.Lock()
	next := redditNextAt[key]
	now := time.Now()
	if now.Before(next) {
		wait := next.Sub(now)
		redditRateMu.Unlock()
		time.Sleep(wait)
		redditRateMu.Lock()
		now = time.Now()
	}
	redditNextAt[key] = now.Add(time.Minute / time.Duration(rpm))
	redditRateMu.Unlock()
	return nil
}

func updateRedditRateFromHeaders(cfg config.RedditConfig, headers http.Header, status int) {
	if headers == nil || !redditHeaderDriven(cfg) {
		return
	}
	var wait time.Duration
	if status == http.StatusTooManyRequests && redditBackoffOn429(cfg) {
		wait = retryAfterDuration(headers.Get("retry-after"), cfg.RateLimit.MinRetryAfterSec)
	} else if remaining, ok := parseHeaderFloat(headers.Get("x-ratelimit-remaining")); ok && remaining <= 1 {
		if reset, ok := parseHeaderFloat(headers.Get("x-ratelimit-reset")); ok && reset > 0 {
			wait = time.Duration(reset * float64(time.Second))
		}
	}
	if wait <= 0 {
		return
	}
	key := cfg.APIBaseURLOrDefault()
	redditRateMu.Lock()
	next := time.Now().Add(wait)
	if next.After(redditNextAt[key]) {
		redditNextAt[key] = next
	}
	redditRateMu.Unlock()
}

func retryAfterDuration(raw string, minSec int) time.Duration {
	seconds := 0.0
	if v, ok := parseHeaderFloat(raw); ok {
		seconds = v
	}
	if minSec > 0 && seconds < float64(minSec) {
		seconds = float64(minSec)
	}
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds * float64(time.Second))
}

func parseHeaderFloat(raw string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func redditHeaderDriven(cfg config.RedditConfig) bool {
	return cfg.RateLimit.HeaderDriven == nil || *cfg.RateLimit.HeaderDriven
}

func redditBackoffOn429(cfg config.RedditConfig) bool {
	return cfg.RateLimit.BackoffOn429 == nil || *cfg.RateLimit.BackoffOn429
}

func classifyRedditHTTPStatus(endpoint string, status int, headers http.Header) *platform.APIAttempt {
	if status == 0 {
		return nil
	}
	meta := redditAPIMeta(endpoint, status, headers)
	switch status {
	case http.StatusUnauthorized:
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrLoginRequired, Message: "Reddit API authentication failed", HTTPStatus: status, Meta: meta}
	case http.StatusForbidden:
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrScopeViolation, Message: "Reddit API permission is insufficient", HTTPStatus: status, Meta: meta}
	case http.StatusTooManyRequests:
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrRateLimited, Message: "Reddit API rate limited", HTTPStatus: status, Meta: meta}
	case http.StatusNotFound:
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrContentNotFound, Message: "Reddit content not found", HTTPStatus: status, Meta: meta}
	}
	if status >= 500 {
		return &platform.APIAttempt{FallbackBrowser: true, Message: fmt.Sprintf("Reddit API temporary HTTP %d", status), HTTPStatus: status, Meta: meta}
	}
	if status < 200 || status >= 300 {
		return &platform.APIAttempt{FallbackBrowser: true, Message: fmt.Sprintf("Reddit API HTTP %d", status), HTTPStatus: status, Meta: meta}
	}
	return nil
}

func redditAPIOKAttempt(endpoint string, status int, headers http.Header) *platform.APIAttempt {
	return &platform.APIAttempt{OK: true, HTTPStatus: status, Meta: redditAPIMeta(endpoint, status, headers)}
}

func redditAPIMeta(endpoint string, status int, headers http.Header) map[string]any {
	meta := map[string]any{
		"platform":          PlatformName,
		"api_endpoint":      endpoint,
		"api_http_status":   status,
		"api_response_type": "json",
	}
	if headers != nil {
		for _, h := range []string{"x-ratelimit-used", "x-ratelimit-remaining", "x-ratelimit-reset", "retry-after"} {
			if v := strings.TrimSpace(headers.Get(h)); v != "" {
				meta[strings.ReplaceAll(h, "-", "_")] = v
			}
		}
	}
	return meta
}

func redditAPIFallbackAttempt(status int, endpoint, code, msg string) *platform.APIAttempt {
	meta := map[string]any{"platform": PlatformName, "api_endpoint": endpoint, "api_error_code": code}
	if status > 0 {
		meta["api_http_status"] = status
	}
	return &platform.APIAttempt{FallbackBrowser: true, Message: msg, HTTPStatus: status, Meta: meta}
}

func redditAPIFatalAttempt(errorCode string, status int, endpoint, code, msg string) *platform.APIAttempt {
	meta := map[string]any{"platform": PlatformName, "api_endpoint": endpoint, "api_error_code": code}
	if status > 0 {
		meta["api_http_status"] = status
	}
	return &platform.APIAttempt{Fatal: true, ErrorCode: errorCode, Message: msg, HTTPStatus: status, Meta: meta}
}

func normalizeListingSort(v string) string {
	sort := strings.ToLower(strings.TrimSpace(v))
	switch sort {
	case "hot", "new", "top", "rising", "controversial":
		return sort
	default:
		return "new"
	}
}

func normalizeTimeRange(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "hour", "day", "week", "month", "year", "all":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func normalizeLimit(v int) int {
	switch {
	case v <= 0:
		return 25
	case v > 100:
		return 100
	default:
		return v
	}
}
