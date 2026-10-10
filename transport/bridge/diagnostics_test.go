package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/version"
	httpgw "github.com/originaleric/digcrawl/transport/http"
)

func TestBuildMismatchDoesNotStartOrReplaceExistingBridge(t *testing.T) {
	dir, err := os.MkdirTemp("/private/tmp", "dig-build-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		_ = json.NewEncoder(w).Encode(protocol.ToolManifest{Runtime: version.RuntimeName, RuntimeVersion: version.RuntimeVersion, BuildID: "old-build"})
	})}
	go server.Serve(listener)
	defer server.Close()
	e := &Executor{socket: socket, executable: "/must-not-execute", profile: "/must-not-start"}
	if _, _, err = e.clientAndManifest(context.Background()); !errors.Is(err, ErrBuildMismatch) {
		t.Fatalf("mismatch=%v", err)
	}
	if probes.Load() != 1 {
		t.Fatalf("probes=%d", probes.Load())
	}
	if _, _, err = probe(context.Background(), socket); err != nil {
		t.Fatal("old listener was replaced")
	}
}
func TestPublicGatewayDoesNotExposeBrowserDiagnostic(t *testing.T) {
	gateway := httpgw.NewServer(nil, nil, "")
	r := httptest.NewRequest(http.MethodPost, "/internal/browser-check", nil)
	w := httptest.NewRecorder()
	gateway.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("public diagnostic status=%d", w.Code)
	}
}
func TestDoctorAbsentBridgeDoesNotStartBrowser(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "skill.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := CheckBrowserOperation(ctx, profile)
	if err == nil || !strings.Contains(err.Error(), "not executed") {
		t.Fatalf("doctor=%v", err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("doctor created a listener: %v", err)
	}
}
