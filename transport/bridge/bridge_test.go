package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
)

func TestSocketPathCanonicalizesProfileSymlink(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "profile-alias.yaml")
	if err := os.Symlink(profile, alias); err != nil {
		t.Skipf("symlinks unavailable in this test environment: %v", err)
	}

	direct, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	aliased, err := SocketPath(alias)
	if err != nil {
		t.Fatal(err)
	}
	if aliased != direct {
		t.Fatalf("symlink socket=%q, direct socket=%q", aliased, direct)
	}
}

func TestPrepareManagedConfigDisablesLegacyCDP(t *testing.T) {
	external := true
	cdpMode := true
	cfg := &config.Config{
		Gateway:      config.GatewayConfig{AuthToken: "not-used-on-a-private-socket"},
		LocalBrowser: config.LocalBrowserConfig{CDPConnectExisting: &external, CDPMode: &cdpMode},
	}

	prepareManagedConfig(cfg)
	if cfg.LocalBrowser.CDPConnectExisting == nil || *cfg.LocalBrowser.CDPConnectExisting {
		t.Fatal("managed bridge retained external CDP mode")
	}
	if cfg.LocalBrowser.CDPMode == nil || *cfg.LocalBrowser.CDPMode {
		t.Fatal("managed bridge retained fixed CDP port mode")
	}
	if cfg.Gateway.AuthToken != "" {
		t.Fatal("managed bridge retained an unnecessary HTTP token")
	}
}

func TestServeRunsGatewayPingAndClosesAfterToolIdle(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	profileYAML := "Gateway:\n" +
		"  ArtifactDir: " + filepath.Join(dir, "artifacts") + "\n" +
		"  AllowedTools: [gateway.ping]\n" +
		"LocalBrowser:\n" +
		"  CookieStoreDir: " + filepath.Join(dir, "cookies") + "\n" +
		"  UserDataDir: " + filepath.Join(dir, "browser-profile") + "\n"
	if err := os.WriteFile(profile, []byte(profileYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}

	// macOS has a short Unix-domain socket path limit; the production socket
	// is hashed under the user cache for this reason. Keep this fixture short
	// too instead of accidentally testing an impossible listener path.
	socketDir, err := os.MkdirTemp("/private/tmp", "digbridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "bridge.sock")
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), cfg, socket, 80*time.Millisecond) }()

	var cReady bool
	var lastProbeErr error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, manifest, probeErr := probe(context.Background(), socket)
		if probeErr == nil {
			cReady = true
			if len(manifest.Tools) != 1 || manifest.Tools[0].Name != "gateway.ping" {
				t.Fatalf("manifest=%+v, want only gateway.ping", manifest.Tools)
			}
			result, callErr := c.Call(context.Background(), protocol.ToolCall{Type: protocol.TypeToolCall, ID: "ping", Tool: "gateway.ping", Input: []byte(`{}`)})
			if callErr != nil {
				t.Fatalf("bridge ping: %v", callErr)
			}
			if result.Status != "success" {
				t.Fatalf("bridge ping result=%+v", result)
			}
			break
		}
		lastProbeErr = probeErr
		select {
		case err := <-done:
			t.Fatalf("bridge exited before becoming ready: %v (last probe: %v)", err, lastProbeErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cReady {
		t.Fatalf("bridge did not become ready: %v", lastProbeErr)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge did not stop cleanly after idle timeout: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge stayed alive after its tool-idle deadline")
	}
}

func TestServeRejectsSecondBridgeForSamePersistentBrowserProfile(t *testing.T) {
	dir := t.TempDir()
	cfg := bridgeTestConfig(t, dir)
	sharedProfile := filepath.Join(dir, "shared-browser-profile")
	if err := os.MkdirAll(sharedProfile, 0o700); err != nil {
		t.Fatal(err)
	}
	profileAlias := filepath.Join(dir, "shared-browser-profile-alias")
	if err := os.Symlink(sharedProfile, profileAlias); err != nil {
		t.Skipf("symlinks unavailable in this test environment: %v", err)
	}
	cfg.LocalBrowser.UserDataDir = profileAlias
	socketDir, err := os.MkdirTemp("/private/tmp", "digbridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	firstDone := make(chan error, 1)
	firstSocket := filepath.Join(socketDir, "first.sock")
	go func() { firstDone <- Serve(ctx, cfg, firstSocket, time.Hour) }()
	if _, _, err := waitForBridge(context.Background(), firstSocket); err != nil {
		t.Fatalf("first bridge did not become ready: %v", err)
	}

	secondCfg := bridgeTestConfig(t, dir)
	secondCfg.LocalBrowser.UserDataDir = sharedProfile
	err = Serve(context.Background(), secondCfg, filepath.Join(socketDir, "second.sock"), time.Hour)
	if !errors.Is(err, ErrBrowserProfileInUse) {
		t.Fatalf("second bridge error=%v, want ErrBrowserProfileInUse", err)
	}

	stop()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first bridge shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first bridge did not stop after cancellation")
	}
}

func TestExecuteBoundsStalledBridgeRPCToCallPolicy(t *testing.T) {
	socketDir, err := os.MkdirTemp("/private/tmp", "digbridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	callStarted := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest":
			_ = json.NewEncoder(w).Encode(protocol.ToolManifest{Type: protocol.TypeToolManifest})
		case "/tools/call":
			close(callStarted)
			<-r.Context().Done()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	executor := &Executor{socket: socket, cfg: &config.Config{}}
	call := &protocol.ToolCall{
		Type:   protocol.TypeToolCall,
		ID:     "stalled",
		Tool:   "gateway.ping",
		Input:  []byte(`{}`),
		Policy: protocol.CallPolicy{TimeoutMs: 75},
	}
	started := time.Now()
	result := executor.Execute(context.Background(), call)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled bridge returned after %s, want call policy bound", elapsed)
	}
	select {
	case <-callStarted:
	default:
		t.Fatal("test bridge never received tool call")
	}
	if result.Status != "error" || result.Error == nil || result.Error.Code != "LOCAL_BRIDGE_UNAVAILABLE" {
		t.Fatalf("result=%+v, want bounded bridge error", result)
	}
	if !strings.Contains(result.Error.Message, "context deadline exceeded") {
		t.Fatalf("bridge error=%q, want call deadline", result.Error.Message)
	}
}

func TestRemoteArtifactStoreBoundsStalledBridgeArtifactFetch(t *testing.T) {
	socketDir, err := os.MkdirTemp("/private/tmp", "digbridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	artifactStarted := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest":
			_ = json.NewEncoder(w).Encode(protocol.ToolManifest{Type: protocol.TypeToolManifest})
		case "/artifacts/stalled":
			close(artifactStarted)
			<-r.Context().Done()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	store := remoteArtifactStore{executor: &Executor{
		socket:          socket,
		cfg:             &config.Config{},
		artifactTimeout: 75 * time.Millisecond,
	}}
	started := time.Now()
	_, _, err = store.Get(context.Background(), "stalled")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled artifact fetch returned after %s, want bounded delivery", elapsed)
	}
	select {
	case <-artifactStarted:
	default:
		t.Fatal("test bridge never received artifact request")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("artifact error=%v, want delivery deadline", err)
	}
}

func TestActivityTrackerKeepsBridgeAliveWhileArtifactStreams(t *testing.T) {
	socketDir, err := os.MkdirTemp("/private/tmp", "digbridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	tracker := newActivityTracker()
	artifactStarted := make(chan struct{})
	releaseArtifact := make(chan struct{})
	server := &http.Server{Handler: tracker.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artifacts/slow" {
			http.NotFound(w, r)
			return
		}
		close(artifactStarted)
		<-releaseArtifact
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("artifact"))
	}))}
	serverDone := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = server.Serve(listener)
		close(serverDone)
	}()
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("bridge test server did not stop")
		}
	})
	t.Cleanup(func() {
		select {
		case <-releaseArtifact:
		default:
			close(releaseArtifact)
		}
	})

	fetchCtx, cancelFetch := context.WithTimeout(context.Background(), time.Second)
	defer cancelFetch()
	fetchDone := make(chan error, 1)
	go func() {
		data, contentType, fetchErr := newSocketClient(socket).FetchArtifact(fetchCtx, "slow")
		if fetchErr == nil && (string(data) != "artifact" || contentType != "text/plain") {
			fetchErr = errors.New("artifact response was incomplete")
		}
		fetchDone <- fetchErr
	}()
	select {
	case <-artifactStarted:
	case <-time.After(time.Second):
		t.Fatal("test bridge never received artifact request")
	}

	idleCtx, cancelIdle := context.WithCancel(context.Background())
	defer cancelIdle()
	const idleTimeout = 50 * time.Millisecond
	go tracker.stopWhenIdle(idleCtx, server, idleTimeout)
	select {
	case <-serverDone:
		t.Fatalf("bridge stopped during active artifact stream: %v", serveErr)
	case <-time.After(2 * idleTimeout):
	}

	close(releaseArtifact)
	select {
	case fetchErr := <-fetchDone:
		if fetchErr != nil {
			t.Fatalf("artifact fetch failed: %v", fetchErr)
		}
	case <-time.After(time.Second):
		t.Fatal("artifact fetch did not finish")
	}
}

func TestWaitForBridgeReportsEarlyStartupExit(t *testing.T) {
	exited := make(chan error, 1)
	exited <- errors.New("exit status 1")
	stderr := &limitedBuffer{limit: startupStderrLimit}
	_, _ = stderr.Write([]byte("bridge: " + ErrBrowserProfileInUse.Error()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := waitForBridgeOrExit(ctx, filepath.Join(t.TempDir(), "missing.sock"), exited, stderr)
	if err == nil || !strings.Contains(err.Error(), ErrBrowserProfileInUse.Error()) {
		t.Fatalf("startup error=%v, want bridge stderr", err)
	}
}

func bridgeTestConfig(t *testing.T, dir string) *config.Config {
	t.Helper()
	profile := filepath.Join(dir, "profile.yaml")
	profileYAML := "Gateway:\n" +
		"  ArtifactDir: " + filepath.Join(dir, "artifacts") + "\n" +
		"  AllowedTools: [gateway.ping]\n" +
		"LocalBrowser:\n" +
		"  CookieStoreDir: " + filepath.Join(dir, "cookies") + "\n" +
		"  UserDataDir: " + filepath.Join(dir, "browser-profile") + "\n"
	if err := os.WriteFile(profile, []byte(profileYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(profile)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
