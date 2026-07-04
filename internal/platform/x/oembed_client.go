package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

type oEmbedResponse struct {
	URL          string `json:"url"`
	AuthorName   string `json:"author_name"`
	AuthorURL    string `json:"author_url"`
	HTML         string `json:"html"`
	Width        int    `json:"width"`
	Type         string `json:"type"`
	ProviderName string `json:"provider_name"`
	ProviderURL  string `json:"provider_url"`
	Version      string `json:"version"`
}

// FetchPostViaOEmbed reads public X posts from the official publish oEmbed endpoint.
func FetchPostViaOEmbed(ctx context.Context, norm *platform.NormalizedURL, cfg config.XAPIConfig) (*oEmbedResponse, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		attempt := xAPIFallbackAttempt(0, "", "missing_post_id", "missing X post id")
		return nil, attempt, 0, attempt.Error()
	}
	if !cfg.OEmbedEnabledOrDefault() {
		attempt := xAPIFallbackAttempt(0, "", "oembed_disabled", "X oEmbed disabled")
		return nil, attempt, 0, attempt.Error()
	}

	apiCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.APITimeoutSecOrDefault())*time.Second)
	defer cancel()

	endpoint, err := xOEmbedURL(cfg, norm)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, "", "oembed_endpoint_build_failed", err.Error())
		return nil, attempt, 0, attempt.Error()
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(apiCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, endpoint, "oembed_request_build_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "digcrawl/1.0")

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, endpoint, "oembed_request_failed", err.Error())
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		attempt := classifyOEmbedResponse(endpoint, resp.StatusCode, nil)
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}

	var parsed oEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		attempt := xAPIFallbackAttempt(resp.StatusCode, endpoint, "oembed_decode_failed", "X oEmbed response is not valid JSON")
		return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
	}
	attempt := classifyOEmbedResponse(endpoint, resp.StatusCode, &parsed)
	if attempt.OK {
		return &parsed, attempt, time.Since(start).Milliseconds(), nil
	}
	return nil, attempt, time.Since(start).Milliseconds(), attempt.Error()
}

func xOEmbedURL(cfg config.XAPIConfig, norm *platform.NormalizedURL) (string, error) {
	u, err := url.Parse(cfg.OEmbedURLOrDefault())
	if err != nil {
		return "", err
	}
	target := strings.TrimSpace(norm.CanonicalURL)
	if target == "" {
		target = fmt.Sprintf("https://x.com/i/status/%s", norm.PlatformID)
	}
	q := u.Query()
	q.Set("url", target)
	q.Set("omit_script", "true")
	q.Set("dnt", "true")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func classifyOEmbedResponse(endpoint string, status int, parsed *oEmbedResponse) *platform.APIAttempt {
	meta := map[string]any{
		"platform":          PlatformName,
		"api_endpoint":      endpoint,
		"api_http_status":   status,
		"api_response_type": "oembed_json",
		"api_auth_mode":     "public_oembed",
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrLoginRequired, Message: "X oEmbed requires access", HTTPStatus: status, Meta: meta}
	}
	if status == http.StatusNotFound {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrContentNotFound, Message: "X post not found by oEmbed", HTTPStatus: status, Meta: meta}
	}
	if status == http.StatusTooManyRequests {
		return &platform.APIAttempt{Fatal: true, ErrorCode: platform.ErrRateLimited, Message: "X oEmbed rate limited", HTTPStatus: status, Meta: meta}
	}
	if status >= 500 {
		return &platform.APIAttempt{FallbackBrowser: true, Message: fmt.Sprintf("X oEmbed temporary HTTP %d", status), HTTPStatus: status, Meta: meta}
	}
	if status < 200 || status >= 300 {
		return &platform.APIAttempt{FallbackBrowser: true, Message: fmt.Sprintf("X oEmbed HTTP %d", status), HTTPStatus: status, Meta: meta}
	}
	if parsed == nil || strings.TrimSpace(parsed.HTML) == "" {
		meta["api_error_code"] = "empty_oembed_payload"
		return &platform.APIAttempt{FallbackBrowser: true, Message: "X oEmbed returned empty payload", HTTPStatus: status, Meta: meta}
	}
	return &platform.APIAttempt{OK: true, HTTPStatus: status, Meta: meta}
}
