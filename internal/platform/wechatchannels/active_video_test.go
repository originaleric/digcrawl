package wechatchannels

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveVideoResponseBuildsContentWithoutMediaOrSecrets(t *testing.T) {
	raw, err := decodeActiveVideo(bytes.NewBufferString(`{"object_id":"object/1","object_nonce_id":"nonce 1","title":"Title","description":"Description","author_id":"author-1","author_name":"Author","published_at":"2026-08-20T12:00:00Z","cover_url":"https://cover.example/image.jpg","cookie":"must-not-be-read"}`))
	if err != nil {
		t.Fatalf("decodeActiveVideo() error = %v", err)
	}
	content, err := contentFromActiveVideo(raw)
	if err != nil {
		t.Fatalf("contentFromActiveVideo() error = %v", err)
	}
	if content.Platform != PlatformName || content.ContentType != ContentType {
		t.Fatalf("content platform/type = %q/%q", content.Platform, content.ContentType)
	}
	if content.SourceURL != "wechat-channels://video/6f626a6563742f31?nonce=6e6f6e63652031" {
		t.Fatalf("source=%q", content.SourceURL)
	}
	if content.Title != "Title" || content.Text != "Description" || content.Author == nil || content.Author.Name != "Author" {
		t.Fatalf("unexpected content: %#v", content)
	}
	if len(content.Media) != 0 {
		t.Fatalf("media must not be imported without explicit download: %#v", content.Media)
	}
	if got := content.PlatformMetadata["cover_url"]; got != "https://cover.example/image.jpg" {
		t.Fatalf("cover_url=%v", got)
	}
	if _, ok := content.PlatformMetadata["cookie"]; ok {
		t.Fatalf("unexpected bridge field was persisted")
	}
}

func TestImportActiveRejectsNonLoopbackBridge(t *testing.T) {
	_, err := ImportActive(context.Background(), "https://bridge.example")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error=%v, want loopback validation", err)
	}
}

func TestImportActiveFetchesLoopbackBridgeResponse(t *testing.T) {
	srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != activeVideoAPIPath {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object_id":"object-1","object_nonce_id":"nonce-1","title":"Title"}`))
	}))
	defer srv.Close()

	content, err := ImportActive(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("ImportActive() error = %v", err)
	}
	if content.Title != "Title" || content.PlatformMetadata["object_id"] != "object-1" {
		t.Fatalf("content=%#v", content)
	}
}

func TestImportActiveFromStateSendsControlToken(t *testing.T) {
	stateDir := t.TempDir()
	if err := writeBridgeState(filepath.Join(stateDir, bridgeStateFile), bridgeState{StateMachine: true, Service: "Wi-Fi", Port: 20261, ProxyMode: "pac", ControlToken: "token", OwnerPID: 1}); err != nil {
		t.Fatal(err)
	}
	srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridgeTokenHeader) != "token" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"object_id":"object-1","object_nonce_id":"nonce-1","title":"Title"}`))
	}))
	defer srv.Close()
	content, err := ImportActiveFromState(context.Background(), srv.URL, stateDir)
	if err != nil || content.Title != "Title" {
		t.Fatalf("content=%#v err=%v", content, err)
	}
}

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func TestImportActiveRequiresVideoIdentity(t *testing.T) {
	_, err := contentFromActiveVideo(activeVideoResponse{ObjectID: "only-id"})
	if err == nil || !strings.Contains(err.Error(), "object_id and object_nonce_id") {
		t.Fatalf("error=%v", err)
	}
}

func TestBridgeEndpointDoesNotKeepCallerPathOrQuery(t *testing.T) {
	got, err := bridgeEndpoint("http://localhost:20261/custom?token=secret")
	if err != nil {
		t.Fatalf("bridgeEndpoint() error = %v", err)
	}
	if got != "http://127.0.0.1:20261/v1/wechat-channels/active-video" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestBridgeEndpointPinsLocalhostToLiteralLoopback(t *testing.T) {
	got, err := bridgeEndpoint("http://localhost:20261")
	if err != nil {
		t.Fatalf("bridgeEndpoint() error = %v", err)
	}
	if got != "http://127.0.0.1:20261/v1/wechat-channels/active-video" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestCanonicalVideoURLPreservesCaseSensitiveIdentity(t *testing.T) {
	upper := canonicalVideoURL("ObjectID", "NonceID")
	lower := canonicalVideoURL("objectid", "nonceid")
	if upper == lower {
		t.Fatalf("case-sensitive video identities must not collide: %q", upper)
	}
}

func TestDecodeActiveVideoRejectsMultipleJSONValues(t *testing.T) {
	_, err := decodeActiveVideo(bytes.NewBufferString(`{"object_id":"a","object_nonce_id":"b"}{}`))
	if err == nil || !strings.Contains(err.Error(), "one JSON object") {
		t.Fatalf("error=%v", err)
	}
}
