package x

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

var (
	xHTTPClient = http.DefaultClient

	xTokenMu    sync.Mutex
	xTokenCache = map[string]string{}
)

func xBearerToken(ctx context.Context, cfg config.XAPIConfig) (string, *platform.APIAttempt, error) {
	if !cfg.EnabledOrDefault() {
		attempt := xAPIFallbackAttempt(0, "", "api_disabled", "X API disabled")
		return "", attempt, attempt.Error()
	}
	if token := strings.TrimSpace(cfg.BearerToken); token != "" {
		return token, nil, nil
	}
	if cfg.AuthModeOrDefault() != "app_only" {
		attempt := xAPIFatalAttempt(platform.ErrLoginRequired, 0, "", "missing bearer token", "X API bearer token is empty")
		return "", attempt, attempt.Error()
	}

	key := strings.TrimSpace(cfg.ConsumerKey)
	secret := strings.TrimSpace(cfg.ConsumerKeySecret)
	if key == "" || secret == "" {
		attempt := xAPIFatalAttempt(platform.ErrLoginRequired, 0, "", "missing app credentials", "X API consumer key or secret is empty")
		return "", attempt, attempt.Error()
	}

	cacheKey := cfg.TokenURLOrDefault() + "|" + key
	xTokenMu.Lock()
	cached := xTokenCache[cacheKey]
	xTokenMu.Unlock()
	if cached != "" {
		return cached, nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURLOrDefault(), bytes.NewBufferString("grant_type=client_credentials"))
	if err != nil {
		attempt := xAPIFallbackAttempt(0, cfg.TokenURLOrDefault(), "request_build_failed", err.Error())
		return "", attempt, attempt.Error()
	}
	credential := url.QueryEscape(key) + ":" + url.QueryEscape(secret)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credential)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("User-Agent", "digcrawl/1.0")

	resp, err := xHTTPClient.Do(req)
	if err != nil {
		attempt := xAPIFallbackAttempt(0, cfg.TokenURLOrDefault(), "token_request_failed", err.Error())
		return "", attempt, attempt.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		attempt := xAPIFallbackAttempt(resp.StatusCode, cfg.TokenURLOrDefault(), "token_read_failed", "X API token response could not be read")
		return "", attempt, attempt.Error()
	}
	if attempt := classifyTokenHTTPStatus(cfg.TokenURLOrDefault(), resp.StatusCode); attempt != nil {
		return "", attempt, attempt.Error()
	}

	var parsed struct {
		TokenType   string `json:"token_type"`
		AccessToken string `json:"access_token"`
		Errors      []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		attempt := xAPIFallbackAttempt(resp.StatusCode, cfg.TokenURLOrDefault(), "token_decode_failed", "X API token response is not valid JSON")
		return "", attempt, attempt.Error()
	}
	token := strings.TrimSpace(parsed.AccessToken)
	if token == "" {
		attempt := xAPIFallbackAttempt(resp.StatusCode, cfg.TokenURLOrDefault(), "token_missing", "X API token response missing access_token")
		return "", attempt, attempt.Error()
	}

	xTokenMu.Lock()
	xTokenCache[cacheKey] = token
	xTokenMu.Unlock()
	return token, nil, nil
}

func classifyTokenHTTPStatus(endpoint string, status int) *platform.APIAttempt {
	if status == http.StatusTooManyRequests {
		return xAPIFatalAttempt(platform.ErrRateLimited, status, endpoint, "rate_limited", "X API token endpoint rate limited")
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return xAPIFatalAttempt(platform.ErrLoginRequired, status, endpoint, "token_unauthorized", "X API token endpoint rejected credentials")
	}
	if status >= 500 {
		return xAPIFallbackAttempt(status, endpoint, "token_temporary_http_error", fmt.Sprintf("X API token endpoint temporary HTTP %d", status))
	}
	if status < 200 || status >= 300 {
		return xAPIFallbackAttempt(status, endpoint, "token_http_error", fmt.Sprintf("X API token endpoint HTTP %d", status))
	}
	return nil
}

func resetXTokenCacheForTest() {
	xTokenMu.Lock()
	defer xTokenMu.Unlock()
	xTokenCache = map[string]string{}
}
