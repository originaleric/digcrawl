package httpgw

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/registry"
)

func TestManifestEndpoint(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "browser.browse"},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})
	srv := NewServer(rt, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/manifest", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var m protocol.ToolManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != protocol.TypeToolManifest || len(m.Tools) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestToolCallAuth(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})
	srv := NewServer(rt, nil, "secret")

	body, _ := json.Marshal(protocol.ToolCall{ID: "1", Tool: "x"})
	req := httptest.NewRequest(http.MethodPost, "/tools/call", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/tools/call", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("expected authorized request to pass auth")
	}
}

func TestWatchlistAdminSourceCRUD(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})
	cfg := &config.Config{
		Watchlist: config.WatchlistConfig{
			Persistence: config.WatchlistPersistenceConfig{
				Driver:     "sqlite",
				SQLitePath: filepath.Join(t.TempDir(), "watchlist.db"),
			},
		},
	}
	srv := NewServer(rt, nil, "secret")
	srv.EnableWatchlistAdmin(cfg)

	body := []byte(`{"id":"reddit-ai","name":"Reddit AI","platform":"reddit","kind":"reddit_subreddit","url":"https://www.reddit.com/r/artificial/new/","every":"30m","notify_to":"feishu-default","options":{"sort":"new"}}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/watchlist/sources", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected auth to protect admin API, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/watchlist/sources", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/watchlist/sources/reddit-ai", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status %d body %s", rec.Code, rec.Body.String())
	}
	var one struct {
		ID      string `json:"id"`
		Every   string `json:"every"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.ID != "reddit-ai" || one.Every != "30m0s" || !one.Enabled {
		t.Fatalf("unexpected source response: %+v", one)
	}

	req = httptest.NewRequest(http.MethodPatch, "/admin/watchlist/sources/reddit-ai/disable", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status %d body %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/watchlist/sources?enabled_only=true", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d body %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Sources []any `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != 0 {
		t.Fatalf("disabled source should be filtered: %+v", list.Sources)
	}

	req = httptest.NewRequest(http.MethodDelete, "/admin/watchlist/sources/reddit-ai", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestWatchlistAdminRequiresConfiguredGatewayToken(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})
	cfg := &config.Config{
		Watchlist: config.WatchlistConfig{
			Persistence: config.WatchlistPersistenceConfig{
				Driver:     "sqlite",
				SQLitePath: filepath.Join(t.TempDir(), "watchlist.db"),
			},
		},
	}
	srv := NewServer(rt, nil, "")
	srv.EnableWatchlistAdmin(cfg)

	req := httptest.NewRequest(http.MethodGet, "/admin/watchlist/sources", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("admin API should not register without gateway token, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestWatchlistAdminSourceDiscover(t *testing.T) {
	t.Parallel()
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a</link><guid>g1</guid></item></channel></rss>`))
	}))
	defer feedSrv.Close()

	reg := registry.New()
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})
	cfg := &config.Config{
		Watchlist: config.WatchlistConfig{
			Source: config.WatchlistSourceConfig{
				Mode: "database",
			},
			Persistence: config.WatchlistPersistenceConfig{
				Driver:     "sqlite",
				SQLitePath: filepath.Join(t.TempDir(), "watchlist.db"),
			},
		},
	}
	srv := NewServer(rt, nil, "secret")
	srv.EnableWatchlistAdmin(cfg)

	body := []byte(`{"id":"rss-local","name":"RSS Local","platform":"rss","kind":"rss","url":"` + feedSrv.URL + `","every":"30m"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/watchlist/sources", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/watchlist/sources/rss-local/discover", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discover status %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Count int `json:"count"`
		Items []struct {
			ContentKey string `json:"content_key"`
			URL        string `json:"url"`
			Title      string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || len(out.Items) != 1 || out.Items[0].ContentKey != "g1" || out.Items[0].Title != "A" {
		t.Fatalf("unexpected discover response: %+v", out)
	}
}
