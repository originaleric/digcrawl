package x

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchPostViaSyndicationUsesPublicFixedTokenWithoutCredentials(t *testing.T) {
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id"); got != "42" {
			t.Fatalf("id=%q", got)
		}
		if got := r.URL.Query().Get("token"); got != "0" {
			t.Fatalf("token=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("unexpected authorization=%q", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Fatalf("unexpected cookie=%q", got)
		}
		_, _ = w.Write([]byte(`{"id_str":"42","text":"public post"}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()
	parsed, attempt, _, err := FetchPostViaSyndication(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, config.XAPIConfig{SyndicationURL: srv.URL, ConsumerKey: "must-not-be-used"})
	if err != nil || parsed == nil || parsed.IDStr != "42" || attempt == nil || !attempt.OK {
		t.Fatalf("parsed=%+v attempt=%+v err=%v", parsed, attempt, err)
	}
}

func TestFetchPostViaSyndicationFailureIsNonTerminalAttempt(t *testing.T) {
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	xHTTPClient = srv.Client()
	_, attempt, _, err := FetchPostViaSyndication(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, config.XAPIConfig{SyndicationURL: srv.URL})
	if err == nil || attempt == nil || attempt.Fatal || !attempt.FallbackBrowser || attempt.HTTPStatus != http.StatusForbidden {
		t.Fatalf("attempt=%+v err=%v", attempt, err)
	}
}

func TestFetchPostViaSyndicationRejectsMismatchedPostID(t *testing.T) {
	oldClient := xHTTPClient
	defer func() { xHTTPClient = oldClient }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id_str":"other","text":"wrong post"}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()
	parsed, attempt, _, err := FetchPostViaSyndication(t.Context(), &platform.NormalizedURL{PlatformID: "42"}, config.XAPIConfig{SyndicationURL: srv.URL})
	if err == nil || parsed != nil || attempt == nil || attempt.OK || attempt.ErrorCode != "post_id_mismatch" || !attempt.FallbackBrowser {
		t.Fatalf("parsed=%+v attempt=%+v err=%v", parsed, attempt, err)
	}
}

func TestReadPostUsesSyndicationForMediaOnlyPost(t *testing.T) {
	oldClient := xHTTPClient
	oldConfig := config.Get().Platform.X
	t.Cleanup(func() {
		xHTTPClient = oldClient
		config.Get().Platform.X = oldConfig
	})
	disabled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id_str":"42","text":"","mediaDetails":[{"type":"video","video_info":{"variants":[{"url":"https://video.example/video.mp4","content_type":"video/mp4"}]}}]}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()
	config.Get().Platform.X = config.XAPIConfig{
		SyndicationURL: srv.URL, APIEnabled: &disabled, OEmbedEnabled: &disabled,
	}
	content, err := ReadPost(t.Context(), platform.ReadInput{URL: "https://x.com/u/status/42", IncludeMedia: true})
	if err != nil {
		t.Fatal(err)
	}
	if content.PlatformMetadata["extraction_source"] != "syndication" || len(content.Media) != 1 || content.Media[0].URL != "https://video.example/video.mp4" {
		t.Fatalf("content=%+v", content)
	}
}

func TestReadPostWithConfigUsesItsOwnSyndicationEndpoint(t *testing.T) {
	oldClient := xHTTPClient
	t.Cleanup(func() { xHTTPClient = oldClient })
	disabled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id_str":"42","text":"runtime scoped post"}`))
	}))
	defer srv.Close()
	xHTTPClient = srv.Client()
	content, err := ReadPostWithConfig(t.Context(), platform.ReadInput{URL: "https://x.com/u/status/42"}, config.XAPIConfig{
		SyndicationURL: srv.URL, APIEnabled: &disabled, OEmbedEnabled: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.Text != "runtime scoped post" || content.PlatformMetadata["extraction_source"] != "syndication" {
		t.Fatalf("content=%+v", content)
	}
}
