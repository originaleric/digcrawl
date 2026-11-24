package platformprov

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourceDiscoverFeedSnapshotPaging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss><channel><title>feed</title><item><title>A</title><guid>a</guid><link>https://example.org/a</link></item><item><title>B</title><guid>b</guid><link>https://example.org/b</link></item></channel></rss>`)
	}))
	defer srv.Close()
	store, e := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	h := SourceDiscoverEntry(nil, store).Handler
	call := func(url, cp string) (map[string]any, error) {
		raw, _ := json.Marshal(map[string]any{"url": url, "source_kind": "rss", "limit": 1, "checkpoint": cp})
		out, _, e := h(context.Background(), &protocol.ToolCall{Input: raw})
		return out, e
	}
	a, e := call(srv.URL, "")
	if e != nil || a["has_more"] != true || a["coverage"] != "complete" {
		t.Fatalf("%v %v", a, e)
	}
	cp := a["next_checkpoint"].(string)
	srv.Close()
	b, e := call(srv.URL, cp)
	if e != nil || b["has_more"] != false {
		t.Fatalf("%v %v", b, e)
	}
	if _, e = call(srv.URL+"/other", cp); e == nil {
		t.Fatal("checkpoint source mismatch accepted")
	}
}
func TestSourceDiscoverPolicyAndUnsupported(t *testing.T) {
	h := SourceDiscoverEntry([]string{"allowed.test"}, nil).Handler
	for _, in := range []string{`{"url":"https://blocked.test","source_kind":"article","platform":"web"}`, `{"url":"https://allowed.test","source_kind":"unknown"}`} {
		_, _, e := h(context.Background(), &protocol.ToolCall{Input: json.RawMessage(in)})
		if e == nil {
			t.Fatal("invalid discovery accepted")
		}
	}
	raw := json.RawMessage(`{"url":"https://allowed.test/post","source_kind":"article","platform":"web"}`)
	out, _, e := h(context.Background(), &protocol.ToolCall{Input: raw})
	if e != nil || out["coverage"] != "complete" {
		t.Fatalf("%v %v", out, e)
	}
	_, _, e = h(context.Background(), &protocol.ToolCall{Input: json.RawMessage(`{"url":"https://allowed.test","source_kind":"rss","checkpoint":"bad"}`)})
	if e == nil || !strings.Contains(e.Error(), "CHECKPOINT_UNAVAILABLE") {
		t.Fatal(e)
	}
}

func TestSourceDiscoverSchemaStableAcrossConnections(t *testing.T) {
	first := string(SourceDiscoverEntry(nil, nil).Descriptor.InputSchema)
	for i := 0; i < 50; i++ {
		if string(SourceDiscoverEntry(nil, nil).Descriptor.InputSchema) != first {
			t.Fatal("nondeterministic capability schema")
		}
	}
}
func TestSourceDiscoverRejectsTrailingInputAndRedirectDomain(t *testing.T) {
	h := SourceDiscoverEntry(nil, nil).Handler
	_, _, e := h(context.Background(), &protocol.ToolCall{Input: json.RawMessage(`{"url":"https://example.test","source_kind":"article","platform":"web"} {}`)})
	if e == nil {
		t.Fatal("accepted trailing input")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://127.0.0.1:1/feed", 302) }))
	defer srv.Close()
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	b, _ := json.Marshal(map[string]any{"url": u, "source_kind": "rss"})
	_, _, e = h(context.Background(), &protocol.ToolCall{Input: b, Policy: protocol.CallPolicy{AllowedDomains: []string{"localhost"}}})
	if e == nil || !strings.Contains(e.Error(), "DOMAIN_NOT_ALLOWED") {
		t.Fatal(e)
	}
}

func TestSourceDiscoverExpiredCursorAndCurrentPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss><channel><item><guid>a</guid><link>https://example.org/a</link></item><item><guid>b</guid><link>https://example.org/b</link></item></channel></rss>`)
	}))
	defer srv.Close()
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := SourceDiscoverEntry(nil, store).Handler
	raw, _ := json.Marshal(map[string]any{"url": srv.URL, "source_kind": "rss", "limit": 1})
	out, _, err := h(context.Background(), &protocol.ToolCall{Input: raw})
	if err != nil {
		t.Fatal(err)
	}
	cp := out["next_checkpoint"].(string)
	raw, _ = json.Marshal(map[string]any{"url": srv.URL, "source_kind": "rss", "checkpoint": cp})
	_, _, err = h(context.Background(), &protocol.ToolCall{Input: raw, Policy: protocol.CallPolicy{AllowedDomains: []string{"127.0.0.1"}}})
	if err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") {
		t.Fatalf("cached entries bypass current policy: %v", err)
	}
	h = SourceDiscoverEntry(nil, mustEmptyDiscoveryStore(t)).Handler
	_, _, err = h(context.Background(), &protocol.ToolCall{Input: raw})
	if err == nil || !strings.Contains(err.Error(), "CHECKPOINT_EXPIRED") {
		t.Fatalf("lost checkpoint falsely successful: %v", err)
	}
}
func mustEmptyDiscoveryStore(t *testing.T) artifact.Store {
	t.Helper()
	s, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestSourceDiscoverEmptyVisibleListIsNotComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>No public entries</body></html>`)
	}))
	defer srv.Close()
	raw, _ := json.Marshal(map[string]any{"url": srv.URL, "source_kind": "web_list"})
	out, _, err := SourceDiscoverEntry(nil, nil).Handler(context.Background(), &protocol.ToolCall{Input: raw})
	if err != nil {
		if !strings.Contains(err.Error(), "CONTENT_NOT_FOUND") {
			t.Fatal(err)
		}
		return
	}
	if out["coverage"] != "unknown" {
		t.Fatalf("empty visible page falsely complete: %v", out)
	}
}
