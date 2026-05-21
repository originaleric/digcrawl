package httpgw

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/originaleric/diggate/gateway/protocol"
	"github.com/originaleric/diggate/gateway/registry"
	"github.com/originaleric/diggate/gateway/runtime"
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
