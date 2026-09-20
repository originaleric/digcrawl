package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/originaleric/digcrawl/config"
)

func TestBackgroundLauncherPolicyKeepsAudioMutedAfterExtraArgs(t *testing.T) {
	args := applyBackgroundLauncherPolicy(launcher.New(), []string{"--mute-audio=false"}).FormatArgs()
	for _, arg := range args {
		if arg == "--mute-audio" {
			return
		}
	}
	t.Fatalf("background browser launch arguments must include --mute-audio: %v", args)
}

func TestNormalizeLocalBrowserConfigKeepsExplicitChromePath(t *testing.T) {
	explicit := browserTestExecutablePath(t.TempDir(), "explicit-chrome")
	envPath := browserTestExecutablePath(t.TempDir(), "env-chrome")
	writeExecutable(t, explicit)
	writeExecutable(t, envPath)
	t.Setenv("DIGCRAWL_CHROME_PATH", envPath)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: explicit})
	if cfg.ChromePath != explicit {
		t.Fatalf("explicit ChromePath should be preserved, got %q", cfg.ChromePath)
	}
}

func TestNormalizeLocalBrowserConfigFallsBackWhenExplicitChromePathIsStale(t *testing.T) {
	stale := filepath.Join(t.TempDir(), "removed-chrome")
	fallback := browserTestExecutablePath(t.TempDir(), "env-chrome")
	writeExecutable(t, fallback)
	t.Setenv("DIGCRAWL_CHROME_PATH", fallback)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: stale})
	if cfg.ChromePath != fallback {
		t.Fatalf("stale ChromePath should fall back to discovered browser %q, got %q", fallback, cfg.ChromePath)
	}
}

func TestNormalizeLocalBrowserConfigUsesEnvChromePath(t *testing.T) {
	path := browserTestExecutablePath(t.TempDir(), "chrome")
	writeExecutable(t, path)
	t.Setenv("DIGCRAWL_CHROME_PATH", path)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{})
	if cfg.ChromePath != path {
		t.Fatalf("expected env ChromePath %q, got %q", path, cfg.ChromePath)
	}
}

func TestFirstExecutablePathPrefersInstalledBrowserOverPlaywrightShell(t *testing.T) {
	root := t.TempDir()
	headlessShell := filepath.Join(root, playwrightHeadlessShellRelPath())
	installed := browserTestExecutablePath(root, "Google Chrome")
	writeExecutable(t, headlessShell)
	writeExecutable(t, installed)

	if got := firstExecutablePath([]string{installed}, []string{headlessShell}); got != installed {
		t.Fatalf("expected installed browser %q, got %q", installed, got)
	}
}

func TestFirstExecutablePathUsesNewestPlaywrightHeadlessShellWhenNeeded(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, playwrightHeadlessShellRelPathVersion("1217"))
	newer := filepath.Join(root, playwrightHeadlessShellRelPathVersion("1223"))
	writeExecutable(t, older)
	writeExecutable(t, newer)
	if got := firstExecutablePath(nil, []string{newer, older}); got != newer {
		t.Fatalf("expected newest Playwright headless shell %q, got %q", newer, got)
	}
}

func TestNormalizeLocalBrowserConfigDoesNotDuplicateHeadlessShellArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), playwrightHeadlessShellRelPath())
	writeExecutable(t, path)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{
		ChromePath: path,
		ExtraArgs:  []string{"--disable-dev-shm-usage"},
	})
	count := 0
	for _, arg := range cfg.ExtraArgs {
		if arg == "--disable-dev-shm-usage" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected compatibility arg once, got %d in %+v", count, cfg.ExtraArgs)
	}
}

func TestBrowserProcessOwnershipFollowsCDPMode(t *testing.T) {
	t.Parallel()
	external := true
	if browserProcessOwned(config.LocalBrowserConfig{CDPConnectExisting: &external}) {
		t.Fatal("an existing CDP browser must remain owned by its service manager")
	}
	internal := false
	if !browserProcessOwned(config.LocalBrowserConfig{CDPConnectExisting: &internal}) {
		t.Fatal("a locally launched browser must remain owned by DigCrawl")
	}
	if !browserProcessOwned(config.LocalBrowserConfig{}) {
		t.Fatal("the default launch mode must remain owned by DigCrawl")
	}
}

func TestCheckAvailableProbesConfiguredExistingCDP(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/test",
		})
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	existing := true
	if err := CheckAvailable(config.LocalBrowserConfig{CDPConnectExisting: &existing, CDPPort: port}); err != nil {
		t.Fatalf("attached CDP health check: %v", err)
	}
}

func TestCheckAvailableRejectsUnavailableExistingCDP(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	existing := true
	err = CheckAvailable(config.LocalBrowserConfig{CDPConnectExisting: &existing, CDPPort: port})
	if err == nil || !strings.Contains(err.Error(), "configured CDP endpoint") {
		t.Fatalf("unavailable attached CDP error=%v", err)
	}
}

func TestCheckAvailableRejectsNonCDPHTTPService(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"not a DevTools endpoint"}`))
	}))
	defer server.Close()
	port := testServerPort(t, server.URL)
	existing := true
	if err := CheckAvailable(config.LocalBrowserConfig{CDPConnectExisting: &existing, CDPPort: port}); err == nil || !strings.Contains(err.Error(), "WebSocket debugger URL") {
		t.Fatalf("non-CDP endpoint error=%v", err)
	}
}

func TestResolveExistingCDPHonorsCallerDeadline(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := resolveExistingCDP(ctx, testServerPort(t, server.URL)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled CDP discovery error=%v, want context deadline exceeded", err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("CDP probe never reached the test server")
	}
}

func TestManagedConfigDisablesExternalCDPAndPrefersConfiguredChrome(t *testing.T) {
	t.Parallel()
	connected := true
	cdpMode := true
	chrome := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	managed := ManagedConfig(config.LocalBrowserConfig{CDPConnectExisting: &connected, CDPMode: &cdpMode, ChromePath: chrome})
	if managed.CDPConnectExisting == nil || *managed.CDPConnectExisting || managed.CDPMode == nil || *managed.CDPMode {
		t.Fatalf("managed config kept an external CDP contract: %+v", managed)
	}
	if managed.ChromePath != chrome {
		t.Fatalf("managed config discarded configured Chrome: %q", managed.ChromePath)
	}
}

func testServerPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestBrowserOperationContextPreservesCallerDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer parentCancel()
	ctx, cancel := browserOperationContext(parent, time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("context error=%v", ctx.Err())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("browser operation context ignored the caller deadline")
	}
}

func TestConnectCDPBrowserHonorsDeadlineDuringStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	release := make(chan struct{})
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		close(accepted)
		<-release // accept the WebSocket request but intentionally never reply.
	}()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err = connectCDPBrowser(ctx, "ws://"+listener.Addr().String()+"/devtools/browser/stalled", time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connect error=%v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("stalled handshake exceeded caller deadline: %s", elapsed)
	}
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("test listener never observed the CDP handshake")
	}
}

func TestDiscoverInteractiveChromePathPrefersVisibleExecutable(t *testing.T) {
	path := browserTestExecutablePath(t.TempDir(), "Chrome")
	writeExecutable(t, path)
	if got := discoverInteractiveChromePath(path); got != path {
		t.Fatalf("interactive path=%q want %q", got, path)
	}
}

func TestDiscoverInteractiveChromePathRejectsHeadlessShell(t *testing.T) {
	path := browserTestExecutablePath(t.TempDir(), "chrome-headless-shell")
	writeExecutable(t, path)
	for _, key := range []string{"DIGCRAWL_CHROME_PATH", "CHROME_PATH", "CHROME_BIN"} {
		t.Setenv(key, path)
	}
	if got := discoverInteractiveChromePath(path); got == path {
		t.Fatalf("headless shell must not be selected for interactive login: %q", got)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("browser"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func browserTestExecutablePath(dir, name string) string {
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func playwrightHeadlessShellRelPath() string {
	return playwrightHeadlessShellRelPathVersion("9999")
}

func playwrightHeadlessShellRelPathVersion(version string) string {
	switch runtime.GOOS {
	case "darwin":
		return "chromium_headless_shell-" + version + "/chrome-headless-shell-mac-arm64/chrome-headless-shell"
	case "windows":
		return "chromium_headless_shell-" + version + "/chrome-headless-shell-win64/chrome-headless-shell.exe"
	default:
		return "chromium_headless_shell-" + version + "/chrome-headless-shell-linux-x64/chrome-headless-shell"
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
