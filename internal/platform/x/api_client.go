package x

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

type tweetLookupResponse struct {
	Data     tweetData       `json:"data"`
	Includes tweetIncludes   `json:"includes"`
	Errors   []tweetAPIError `json:"errors"`
	Meta     map[string]any  `json:"meta"`
}

type tweetData struct {
	ID                string             `json:"id"`
	Text              string             `json:"text"`
	AuthorID          string             `json:"author_id"`
	CreatedAt         string             `json:"created_at"`
	Lang              string             `json:"lang"`
	ConversationID    string             `json:"conversation_id"`
	PossiblySensitive bool               `json:"possibly_sensitive"`
	Attachments       tweetAttachments   `json:"attachments"`
	PublicMetrics     tweetPublicMetrics `json:"public_metrics"`
	ReferencedTweets  []tweetReference   `json:"referenced_tweets"`
	Entities          map[string]any     `json:"entities"`
}

type tweetAttachments struct {
	MediaKeys []string `json:"media_keys"`
}

type tweetPublicMetrics struct {
	RetweetCount  int `json:"retweet_count"`
	ReplyCount    int `json:"reply_count"`
	LikeCount     int `json:"like_count"`
	QuoteCount    int `json:"quote_count"`
	BookmarkCount int `json:"bookmark_count"`
}

type tweetReference struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type tweetIncludes struct {
	Users  []tweetUser  `json:"users"`
	Media  []tweetMedia `json:"media"`
	Tweets []tweetData  `json:"tweets"`
}

type tweetUser struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Username        string `json:"username"`
	ProfileImageURL string `json:"profile_image_url"`
	Verified        bool   `json:"verified"`
}

type tweetMedia struct {
	MediaKey        string            `json:"media_key"`
	Type            string            `json:"type"`
	URL             string            `json:"url"`
	PreviewImageURL string            `json:"preview_image_url"`
	AltText         string            `json:"alt_text"`
	DurationMS      int               `json:"duration_ms"`
	PublicMetrics   tweetMediaMetrics `json:"public_metrics"`
}

type tweetMediaMetrics struct {
	ViewCount int `json:"view_count"`
}

type tweetAPIError struct {
	Title        string `json:"title"`
	Detail       string `json:"detail"`
	Type         string `json:"type"`
	Reason       string `json:"reason"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	Status       int    `json:"status"`
}

const xTweetLookupPath = "/2/tweets/%s"

// FetchPostViaAPI reads a single X post through the official tweet lookup API.
func FetchPostViaAPI(ctx context.Context, norm *platform.NormalizedURL, cfg config.XAPIConfig) (*tweetLookupResponse, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		attempt := xAPIFallbackAttempt(0, "", "missing_post_id", "missing X post id")
		return nil, attempt, 0, attempt.Error()
	}
	if !cfg.EnabledOrDefault() {
		attempt := xAPIFallbackAttempt(0, "", "api_disabled", "X API disabled")
		return nil, attempt, 0, attempt.Error()
	}

	apiCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.APITimeoutSecOrDefault())*time.Second)
	defer cancel()

	start := time.Now()
	authMode := cfg.AuthModeOrDefault()
	token, attempt, err := xBearerToken(apiCtx, cfg)
	if err != nil {
		return nil, attempt, time.Since(start).Milliseconds(), err
	}

	endpoint, err := xTweetLookupURL(cfg, norm.PlatformID)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, "", "endpoint_build_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}

	var lastAttempt *platform.APIAttempt
	var lastErr error
	totalAttempts := cfg.APIMaxRetriesOrDefault() + 1
	for i := 0; i < totalAttempts; i++ {
		resp, attempt, err := fetchTweetLookupOnce(apiCtx, endpoint, token, authMode)
		attempt.Meta["api_retry_index"] = i
		if err == nil && resp != nil {
			return resp, attempt, time.Since(start).Milliseconds(), nil
		}
		lastAttempt = attempt
		lastErr = err
		if attempt == nil || attempt.Fatal || attempt.HTTPStatus < 500 {
			break
		}
	}
	if lastAttempt == nil {
		lastAttempt = xAPIFallbackAttempt(0, endpoint, "unknown", "X API failed")
		lastErr = lastAttempt.Error()
	}
	return nil, lastAttempt, time.Since(start).Milliseconds(), lastErr
}

func xTweetLookupURL(cfg config.XAPIConfig, id string) (string, error) {
	base, err := url.Parse(cfg.APIBaseURLOrDefault())
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + fmt.Sprintf(xTweetLookupPath, id)
	q := base.Query()
	q.Set("expansions", "author_id,attachments.media_keys,referenced_tweets.id")
	q.Set("tweet.fields", "attachments,author_id,created_at,entities,lang,public_metrics,referenced_tweets,source,possibly_sensitive,conversation_id")
	q.Set("user.fields", "id,name,username,profile_image_url,verified")
	q.Set("media.fields", "media_key,type,url,preview_image_url,duration_ms,public_metrics,alt_text")
	base.RawQuery = q.Encode()
	return base.String(), nil
}

func fetchTweetLookupOnce(ctx context.Context, endpoint, token, authMode string) (*tweetLookupResponse, *platform.APIAttempt, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, endpoint, "request_build_failed", err.Error())
		return nil, attempt, attempt.Error()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "digcrawl/1.0")

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, endpoint, "request_failed", err.Error())
		return nil, attempt, attempt.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		attempt := xAPIFallbackAttempt(resp.StatusCode, endpoint, "response_read_failed", "X API response could not be read")
		return nil, attempt, attempt.Error()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		attempt := classifyTweetLookupResponse(endpoint, resp.StatusCode, nil, authMode)
		return nil, attempt, attempt.Error()
	}

	var parsed tweetLookupResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		attempt := xAPIFallbackAttempt(resp.StatusCode, endpoint, "decode_failed", "X API response is not valid JSON")
		return nil, attempt, attempt.Error()
	}
	attempt := classifyTweetLookupResponse(endpoint, resp.StatusCode, &parsed, authMode)
	if attempt.OK {
		return &parsed, attempt, nil
	}
	return nil, attempt, attempt.Error()
}

func classifyTweetLookupResponse(endpoint string, status int, parsed *tweetLookupResponse, authMode string) *platform.APIAttempt {
	authMode = strings.TrimSpace(authMode)
	if authMode == "" {
		authMode = "app_only"
	}
	meta := map[string]any{
		"platform":          PlatformName,
		"api_endpoint":      endpoint,
		"api_http_status":   status,
		"api_response_type": "json",
		"api_auth_mode":     authMode,
	}
	if parsed != nil && len(parsed.Errors) > 0 {
		meta["api_error_title"] = strings.TrimSpace(parsed.Errors[0].Title)
		meta["api_error_reason"] = strings.TrimSpace(parsed.Errors[0].Reason)
	}
	if status == http.StatusUnauthorized {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrLoginRequired, Message: "X API authentication failed", HTTPStatus: status, Meta: meta}
	}
	if status == http.StatusForbidden {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrScopeViolation, Message: "X API permission is insufficient for tweet lookup", HTTPStatus: status, Meta: meta}
	}
	if status == http.StatusPaymentRequired {
		meta["api_error_code"] = "PAYMENT_REQUIRED"
		return &platform.APIAttempt{FallbackBrowser: true, ErrorCode: platform.ErrScopeViolation, Message: "X API requires paid read access for tweet lookup", HTTPStatus: status, Meta: meta}
	}
	if status == http.StatusNotFound {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrContentNotFound, Message: "X post not found", HTTPStatus: status, Meta: meta}
	}
	if status == http.StatusTooManyRequests {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrRateLimited, Message: "X API rate limited", HTTPStatus: status, Meta: meta}
	}
	if status >= 500 {
		return &platform.APIAttempt{FallbackBrowser: true, Message: fmt.Sprintf("X API temporary HTTP %d", status), HTTPStatus: status, Meta: meta}
	}
	if status < 200 || status >= 300 {
		return &platform.APIAttempt{FallbackBrowser: true, Message: fmt.Sprintf("X API HTTP %d", status), HTTPStatus: status, Meta: meta}
	}
	if parsed == nil || strings.TrimSpace(parsed.Data.ID) == "" {
		meta["api_error_code"] = "empty_payload"
		return &platform.APIAttempt{FallbackBrowser: true, Message: "X API returned empty tweet payload", HTTPStatus: status, Meta: meta}
	}
	return &platform.APIAttempt{OK: true, HTTPStatus: status, Meta: meta}
}

func xAPIFallbackAttempt(status int, endpoint, code, message string) *platform.APIAttempt {
	meta := map[string]any{
		"platform":          PlatformName,
		"api_endpoint":      endpoint,
		"api_response_type": code,
	}
	if status > 0 {
		meta["api_http_status"] = status
	}
	return &platform.APIAttempt{FallbackBrowser: true, Message: message, HTTPStatus: status, Meta: meta}
}

func xAPIFatalAttempt(code string, status int, endpoint, reason, message string) *platform.APIAttempt {
	meta := map[string]any{
		"platform":          PlatformName,
		"api_endpoint":      endpoint,
		"api_response_type": reason,
	}
	if status > 0 {
		meta["api_http_status"] = status
	}
	return &platform.APIAttempt{Fatal: true, ErrorCode: code, Message: message, HTTPStatus: status, Meta: meta}
}
