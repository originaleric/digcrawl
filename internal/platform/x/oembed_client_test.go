package x

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchPostViaOEmbed(t *testing.T) {
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Query().Get("url")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://x.com/jack/status/20","author_name":"jack","author_url":"https://x.com/jack","html":"<blockquote><p>just setting up my twttr</p><a href=\"https://x.com/jack/status/20\">March 21, 2006</a></blockquote>","provider_name":"X"}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	resp, attempt, _, err := FetchPostViaOEmbed(t.Context(), &platform.NormalizedURL{
		PlatformID:   "20",
		CanonicalURL: "https://x.com/jack/status/20",
	}, config.XAPIConfig{OEmbedURL: srv.URL, APITimeoutSec: 2})
	if err != nil {
		t.Fatal(err)
	}
	if attempt == nil || !attempt.OK {
		t.Fatalf("attempt=%+v", attempt)
	}
	if gotURL != "https://x.com/jack/status/20" {
		t.Fatalf("url query=%q", gotURL)
	}
	if resp.AuthorName != "jack" {
		t.Fatalf("author=%q", resp.AuthorName)
	}
}

func TestFetchPostViaOEmbedNotFoundBeforeJSONDecode(t *testing.T) {
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>not found</html>"))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()

	_, attempt, _, err := FetchPostViaOEmbed(t.Context(), &platform.NormalizedURL{
		PlatformID:   "missing",
		CanonicalURL: "https://x.com/example/status/missing",
	}, config.XAPIConfig{OEmbedURL: srv.URL, APITimeoutSec: 2})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempt == nil || !attempt.Fatal || attempt.ErrorCode != platform.ErrContentNotFound {
		t.Fatalf("attempt=%+v", attempt)
	}
}
