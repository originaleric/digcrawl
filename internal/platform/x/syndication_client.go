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

const syndicationResponseMaxBytes int64 = 1 << 20

// FetchPostViaSyndication reads the public embed distribution payload. Its
// errors are intentionally non-terminal: ReadPost preserves the established
// API, oEmbed, and browser fallback chain after every syndication failure.
func FetchPostViaSyndication(ctx context.Context, norm *platform.NormalizedURL, cfg config.XAPIConfig) (*syndicationTweetResponse, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		attempt := syndicationAttempt(0, "", "missing_post_id", "missing X post id")
		return nil, attempt, 0, attempt.Error()
	}
	if !cfg.SyndicationEnabledOrDefault() {
		attempt := syndicationAttempt(0, "", "syndication_disabled", "X syndication disabled")
		return nil, attempt, 0, attempt.Error()
	}
	endpoint, err := xSyndicationURL(cfg, norm.PlatformID)
	if err != nil {
		attempt := syndicationAttempt(0, "", "endpoint_build_failed", "syndication endpoint could not be built")
		return nil, attempt, 0, attempt.Error()
	}

	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.SyndicationTimeoutSecOrDefault())*time.Second)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		attempt := syndicationAttempt(0, endpoint, "request_build_failed", "syndication request could not be built")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "digcrawl/1.0")
	resp, err := xHTTPClient.Do(req)
	if err != nil {
		attempt := syndicationAttempt(0, endpoint, "request_failed", "syndication request failed")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, syndicationResponseMaxBytes+1))
	if err != nil || int64(len(body)) > syndicationResponseMaxBytes {
		attempt := syndicationAttempt(resp.StatusCode, endpoint, "response_read_failed", "syndication response could not be read")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		attempt := syndicationAttempt(resp.StatusCode, endpoint, fmt.Sprintf("syndication_http_%d", resp.StatusCode), fmt.Sprintf("syndication HTTP %d", resp.StatusCode))
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	var parsed syndicationTweetResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		attempt := syndicationAttempt(resp.StatusCode, endpoint, "decode_failed", "syndication response is not valid JSON")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	if strings.TrimSpace(parsed.IDStr) == "" {
		attempt := syndicationAttempt(resp.StatusCode, endpoint, "empty_payload", "syndication response has no post")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	if strings.TrimSpace(parsed.IDStr) != strings.TrimSpace(norm.PlatformID) {
		attempt := syndicationAttempt(resp.StatusCode, endpoint, "post_id_mismatch", "syndication response does not match requested post")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	return &parsed, syndicationSuccessAttempt(resp.StatusCode, endpoint), time.Since(start).Milliseconds(), nil
}

func xSyndicationURL(cfg config.XAPIConfig, id string) (string, error) {
	u, err := url.Parse(cfg.SyndicationURLOrDefault())
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("syndication endpoint must be HTTP(S)")
	}
	q := u.Query()
	q.Set("id", strings.TrimSpace(id))
	q.Set("token", "0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func syndicationSuccessAttempt(status int, endpoint string) *platform.APIAttempt {
	return &platform.APIAttempt{OK: true, HTTPStatus: status, Meta: map[string]any{
		"platform": PlatformName, "api_endpoint": endpoint, "api_http_status": status,
		"api_response_type": "syndication_json", "api_auth_mode": "public_syndication",
	}}
}

func syndicationAttempt(status int, endpoint, code, message string) *platform.APIAttempt {
	return &platform.APIAttempt{FallbackBrowser: true, ErrorCode: code, HTTPStatus: status, Message: message, Meta: map[string]any{
		"platform": PlatformName, "api_endpoint": endpoint, "api_http_status": status,
		"api_response_type": "syndication_json", "api_auth_mode": "public_syndication", "api_error_code": code,
	}}
}
