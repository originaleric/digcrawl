package x

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchPostViaAPI(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	var lookupPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"token_type":"bearer","access_token":"token-1"}`))
		case "/2/tweets/42":
			lookupPath = r.URL.String()
			if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
				t.Fatalf("authorization=%q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":         "42",
					"text":       "hello x",
					"author_id":  "u1",
					"created_at": "2026-07-04T00:00:00.000Z",
					"public_metrics": map[string]any{
						"like_count":    3,
						"reply_count":   1,
						"retweet_count": 2,
						"quote_count":   4,
					},
				},
				"includes": map[string]any{
					"users": []map[string]any{{"id": "u1", "name": "Tester", "username": "tester"}},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		AuthMode:          "app_only",
		APIBaseURL:        srv.URL,
		TokenURL:          srv.URL + "/oauth2/token",
		ConsumerKey:       "key",
		ConsumerKeySecret: "secret",
		APITimeoutSec:     2,
	}
	resp, attempt, _, err := FetchPostViaAPI(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if attempt == nil || !attempt.OK {
		t.Fatalf("attempt=%+v", attempt)
	}
	if resp.Data.Text != "hello x" {
		t.Fatalf("text=%q", resp.Data.Text)
	}
	if !strings.Contains(lookupPath, "tweet.fields=") || !strings.Contains(lookupPath, "expansions=") {
		t.Fatalf("lookup query missing fields: %s", lookupPath)
	}
}

func TestFetchPostViaAPIForbiddenIsScopeViolation(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_, _ = w.Write([]byte(`{"token_type":"bearer","access_token":"token-1"}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"title":"Client Forbidden","reason":"ClientForbidden"}]}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		AuthMode:          "app_only",
		APIBaseURL:        srv.URL,
		TokenURL:          srv.URL + "/oauth2/token",
		ConsumerKey:       "key",
		ConsumerKeySecret: "secret",
		APITimeoutSec:     2,
	}
	_, attempt, _, err := FetchPostViaAPI(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempt == nil || !attempt.Fatal || attempt.ErrorCode != platform.ErrScopeViolation {
		t.Fatalf("attempt=%+v", attempt)
	}
}

func TestClassifyTweetLookupPaymentRequiredFallsBackWithScopeCode(t *testing.T) {
	attempt := classifyTweetLookupResponse("https://api.twitter.com/2/tweets/42", http.StatusPaymentRequired, &tweetLookupResponse{}, "app_only")
	if attempt == nil || !attempt.FallbackBrowser || attempt.Fatal {
		t.Fatalf("attempt=%+v", attempt)
	}
	if attempt.ErrorCode != platform.ErrScopeViolation {
		t.Fatalf("error code=%q", attempt.ErrorCode)
	}
	if attempt.Meta["api_error_code"] != "PAYMENT_REQUIRED" {
		t.Fatalf("metadata=%+v", attempt.Meta)
	}
}

func TestFetchPostViaAPINonJSONStatusClassifiedBeforeDecode(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_, _ = w.Write([]byte(`{"token_type":"bearer","access_token":"token-1"}`))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		AuthMode:          "app_only",
		APIBaseURL:        srv.URL,
		TokenURL:          srv.URL + "/oauth2/token",
		ConsumerKey:       "key",
		ConsumerKeySecret: "secret",
		APITimeoutSec:     2,
	}
	_, attempt, _, err := FetchPostViaAPI(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempt == nil || !attempt.Fatal || attempt.ErrorCode != platform.ErrRateLimited {
		t.Fatalf("attempt=%+v", attempt)
	}
	if attempt.Meta["api_auth_mode"] != "app_only" {
		t.Fatalf("metadata=%+v", attempt.Meta)
	}
}

func TestFetchPostViaAPIBearerModeMetadata(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			t.Fatal("bearer token mode should not call token endpoint")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer static-token" {
			t.Fatalf("authorization=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"42","text":"hello bearer","author_id":"u1"},"includes":{"users":[{"id":"u1","name":"Tester","username":"tester"}]}}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		APIBaseURL:    srv.URL,
		BearerToken:   "static-token",
		APITimeoutSec: 2,
	}
	_, attempt, _, err := FetchPostViaAPI(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if attempt == nil || !attempt.OK {
		t.Fatalf("attempt=%+v", attempt)
	}
	if attempt.Meta["api_auth_mode"] != "bearer" {
		t.Fatalf("metadata=%+v", attempt.Meta)
	}
}
