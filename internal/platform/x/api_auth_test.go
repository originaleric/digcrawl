package x

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestXBearerTokenAppOnlyCachesToken(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Fatalf("authorization=%q", got)
		} else {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "Basic "))
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != "key:secret" {
				t.Fatalf("basic credential=%q", string(raw))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token_type":"bearer","access_token":"token-1"}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		AuthMode:          "app_only",
		TokenURL:          srv.URL,
		ConsumerKey:       "key",
		ConsumerKeySecret: "secret",
	}
	token, attempt, err := xBearerToken(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if attempt != nil {
		t.Fatalf("unexpected attempt: %+v", attempt)
	}
	if token != "token-1" {
		t.Fatalf("token=%q", token)
	}
	token, _, err = xBearerToken(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if token != "token-1" || calls != 1 {
		t.Fatalf("token=%q calls=%d", token, calls)
	}
}

func TestXBearerTokenDoesNotLeakSecretInError(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"bad auth"}]}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		AuthMode:          "app_only",
		TokenURL:          srv.URL,
		ConsumerKey:       "key",
		ConsumerKeySecret: "super-secret",
	}
	_, attempt, err := xBearerToken(t.Context(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempt == nil || !attempt.Fatal {
		t.Fatalf("attempt=%+v", attempt)
	}
	joined := err.Error()
	if attempt.Message != "" {
		joined += " " + attempt.Message
	}
	for _, v := range attempt.Meta {
		joined += " " + strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(vString(v), "\n", " "), "\t", " "))
	}
	if strings.Contains(joined, "super-secret") {
		t.Fatalf("secret leaked in error metadata: %s", joined)
	}
}

func TestXBearerTokenNonJSONStatusClassifiedBeforeDecode(t *testing.T) {
	resetXTokenCacheForTest()
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("<html>unauthorized</html>"))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	cfg := config.XAPIConfig{
		AuthMode:          "app_only",
		TokenURL:          srv.URL,
		ConsumerKey:       "key",
		ConsumerKeySecret: "secret",
	}
	_, attempt, err := xBearerToken(t.Context(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempt == nil || !attempt.Fatal || attempt.ErrorCode != platform.ErrLoginRequired {
		t.Fatalf("attempt=%+v", attempt)
	}
	if attempt.Meta["api_response_type"] != "token_unauthorized" {
		t.Fatalf("metadata=%+v", attempt.Meta)
	}
}

func vString(v any) string {
	return fmt.Sprint(v)
}
