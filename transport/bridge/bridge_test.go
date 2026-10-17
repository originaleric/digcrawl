package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/version"
)

// The failing child is a native executable on every platform. It reports the
// private startup diagnostic without depending on a shell interpreter.
func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "bridge-failure" {
		for i, arg := range os.Args {
			if arg == "--startup-status" && i+1 < len(os.Args) {
				_ = os.WriteFile(os.Args[i+1], []byte("configured Chrome profile is already owned by another local DigCrawl bridge"), 0o600)
				break
			}
		}
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// bridgeSocketTempDir keeps fixtures within the Unix socket path limit while
// using a temporary root that exists on the current CI platform.
func bridgeSocketTempDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS == "darwin" {
		// macOS's default per-user temporary root can itself be too long.
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "digbridge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

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

func TestServeKeepsBridgeAvailableAcrossIdleGaps(t *testing.T) {
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
	socketDir := bridgeSocketTempDir(t)
	socket := filepath.Join(socketDir, "bridge.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, socket) }()

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

	// The bridge transport must survive an otherwise idle period. Stdio
	// sessions own stale-process cleanup; closing this singleton on a timer
	// would race the next manifest -> tools/call transition.
	time.Sleep(160 * time.Millisecond)
	c, manifest, err := probe(context.Background(), socket)
	if err != nil {
		t.Fatalf("bridge disappeared after an idle gap: %v", err)
	}
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != "gateway.ping" {
		t.Fatalf("manifest after idle gap=%+v", manifest)
	}
	result, err := c.Call(context.Background(), protocol.ToolCall{Type: protocol.TypeToolCall, ID: "ping-after-idle", Tool: "gateway.ping", Input: []byte(`{}`)})
	if err != nil || result.Status != "success" {
		t.Fatalf("bridge call after idle gap result=%+v err=%v", result, err)
	}

	select {
	case err := <-done:
		t.Fatalf("bridge stopped without an explicit lifecycle signal: %v", err)
	case <-time.After(80 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge did not stop cleanly after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after cancellation")
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
	socketDir := bridgeSocketTempDir(t)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	firstDone := make(chan error, 1)
	firstSocket := filepath.Join(socketDir, "first.sock")
	go func() { firstDone <- Serve(ctx, cfg, firstSocket) }()
	if _, _, err := waitForBridge(context.Background(), firstSocket); err != nil {
		t.Fatalf("first bridge did not become ready: %v", err)
	}

	secondCfg := bridgeTestConfig(t, dir)
	secondCfg.LocalBrowser.UserDataDir = sharedProfile
	err := Serve(context.Background(), secondCfg, filepath.Join(socketDir, "second.sock"))
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

func TestStopDoesNotFallBackToAnInProcessControlRequest(t *testing.T) {
	dir := t.TempDir()
	cfg := bridgeTestConfig(t, dir)
	profile := filepath.Join(dir, "profile.yaml")
	socket, err := SocketPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(socket) })
	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	done := make(chan error, 1)
	go func() { done <- Serve(serveCtx, cfg, socket) }()
	if _, _, err := waitForBridge(context.Background(), socket); err != nil {
		t.Fatalf("bridge did not become ready: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped, err := Stop(ctx, profile)
	if stopped || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() = (%v, %v), want (false, deadline exceeded)", stopped, err)
	}
	cancelServe()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridge shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not exit after cancellation")
	}
	if _, _, err := probe(context.Background(), socket); err == nil {
		t.Fatal("bridge still answered after Stop")
	}
}

func TestExecuteBoundsStalledBridgeRPCToCallPolicy(t *testing.T) {
	socketDir := bridgeSocketTempDir(t)
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
			_ = json.NewEncoder(w).Encode(protocol.ToolManifest{Type: protocol.TypeToolManifest, Runtime: version.RuntimeName, RuntimeVersion: version.RuntimeVersion, BuildID: version.BuildIdentity()})
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
	socketDir := bridgeSocketTempDir(t)
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
			_ = json.NewEncoder(w).Encode(protocol.ToolManifest{Type: protocol.TypeToolManifest, Runtime: version.RuntimeName, RuntimeVersion: version.RuntimeVersion, BuildID: version.BuildIdentity()})
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

func TestWaitForBridgeReportsEarlyStartupExit(t *testing.T) {
	exited := make(chan error, 1)
	exited <- errors.New("exit status 1")
	var stderr strings.Builder
	_, _ = stderr.WriteString("bridge: " + ErrBrowserProfileInUse.Error())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := waitForBridgeOrExit(ctx, filepath.Join(t.TempDir(), "missing.sock"), exited, &stderr)
	if err == nil || !strings.Contains(err.Error(), ErrBrowserProfileInUse.Error()) {
		t.Fatalf("startup error=%v, want bridge stderr", err)
	}
}

func TestWaitForBridgeReportsStartupStatusBeforeChildWait(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "bridge.startup-status")
	if err := os.WriteFile(statusPath, []byte("configured Chrome profile is already owned by another local DigCrawl bridge"), 0o600); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := waitForBridgeOrExit(ctx, filepath.Join(dir, "missing.sock"), exited, startupStatusFile{path: statusPath})
	if err == nil || !strings.Contains(err.Error(), "configured Chrome profile is already owned") {
		t.Fatalf("startup error=%v, want private startup diagnostic", err)
	}
}

func TestNewExecutorReportsPrivateStartupStatus(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "bridge-failure")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	// This matches the production startup budget plus a small scheduling margin:
	// the assertion is about a deterministic private diagnostic, not whether a
	// busy local test runner schedules a child in an arbitrary three seconds.
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout+2*time.Second)
	defer cancel()
	_, err = NewExecutor(ctx, executable, profile, &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "configured Chrome profile is already owned") {
		t.Fatalf("startup error=%v, want private startup diagnostic", err)
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
