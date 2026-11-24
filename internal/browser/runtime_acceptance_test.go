package browser

import (
	"context"
	"encoding/json"
	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
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
)

// Uses pinned vendor archives and temporary profiles only. No desktop browser
// is launched by this test; real macOS application coexistence is checked
// separately against the running user application.
func TestIsolatedRuntimeInstallAndBrowserAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_RUNTIME_ACCEPTANCE") != "1" {
		t.Skip("pinned vendor runtime acceptance is opt-in")
	}
	archives := os.Getenv("DIGCRAWL_RUNTIME_ARCHIVES")
	if archives == "" {
		t.Fatal("set DIGCRAWL_RUNTIME_ARCHIVES to offline vendor ZIP directory")
	}
	t.Setenv("DIGCRAWL_BROWSER_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := InstallBrowserRuntime(ctx, archives); err != nil {
		t.Fatal(err)
	}
	shell, err := RuntimeExecutable(false)
	if err != nil {
		t.Fatal(err)
	}
	headed, err := RuntimeExecutable(true)
	if err != nil || headed == shell {
		t.Fatalf("headed runtime %q err=%v", headed, err)
	}
	if err := InstallBrowserRuntime(ctx, archives); err != nil {
		t.Fatal("idempotent installation:", err)
	}
	if err := validateOwnedRuntimePath(shell, false); err != nil {
		t.Fatal(err)
	}
	if err := validateOwnedRuntimePath(headed, false); err == nil {
		t.Fatal("headed runtime accepted as background shell")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cookie" {
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "local-only", Path: "/"})
		}
		_, _ = w.Write([]byte("<html><head><title>isolated runtime</title></head><body><article>confirmed local body</article></body></html>"))
	}))
	defer server.Close()
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{UserDataDir: t.TempDir(), CookieStoreDir: t.TempDir(), TotalTimeoutSec: 30})
	manager := &browserManager{cfg: cfg, slots: make(chan struct{}, 3), sessions: make(map[uint64]*browserSession), stopCh: make(chan struct{})}
	defer manager.shutdownImmediately(context.Background())
	c, err := manager.openBrowserConnection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), browserShutdownTimeout)
		defer stop()
		if err := c.closeResources(cleanup); err != nil {
			t.Error(err)
		}
	}()
	page, err := c.browser.Page(proto.TargetCreateTarget{URL: server.URL + "/cookie"})
	if err != nil {
		t.Fatal(err)
	}
	if err = page.WaitLoad(); err != nil {
		t.Fatal(err)
	}
	result, err := page.Eval("() => document.body.innerText")
	if err != nil || result.Value.Str() != "confirmed local body" {
		t.Fatalf("body read %v %v", result, err)
	}
	png, err := page.Screenshot(false, nil)
	if err != nil || len(png) < 100 {
		t.Fatalf("screenshot len=%d err=%v", len(png), err)
	}
	cookies, err := page.Cookies([]string{server.URL})
	if err != nil || len(cookies) == 0 {
		t.Fatalf("cookie collection err=%v", err)
	}
	target, _ := url.Parse(server.URL)
	if err := saveDomainCookies(page, target.Hostname(), cfg.CookieStoreDir, target); err != nil {
		t.Fatal("persist session:", err)
	}
	header, err := CookieHeaderForURL(cfg.CookieStoreDir, server.URL)
	if err != nil || header != "fixture=local-only" {
		t.Fatal("reuse saved session failed:", err)
	}
	_ = page.Close()

	// Exercise the public multi-instance lifecycle with the same release runtime.
	fixedCDP := true
	instanceCfg := cfg
	instanceCfg.CDPMode = &fixedCDP
	instanceCfg.CDPPort = 9222
	instances := &InstanceManager{cfg: instanceCfg, baseDir: t.TempDir(), instances: make(map[string]*BrowserInstance)}
	instance, err := instances.CreateInstance(ctx, "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	second, err := instances.CreateInstance(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}
	if instance.connection.cdpPort != 0 || second.connection.cdpPort != 0 {
		t.Fatal("fixed CDP port inherited")
	}
	if err := instances.StopInstance(second.ID); err != nil {
		t.Fatal(err)
	}
	if err := instance.connection.probe(ctx); err != nil {
		t.Fatal("stopping second broke first", err)
	}
	if err := instances.StopInstance(instance.ID); err != nil {
		t.Fatal(err)
	}
	if len(instances.instances) != 0 {
		t.Fatal("stopped instance owner retained")
	}
	if err := instances.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := instances.CreateInstance(ctx, "after-stop"); err == nil {
		t.Fatal("instance started after shutdown")
	}

	// Prove external CDP attachment is permitted only to a verified shell and
	// closing the attachment never closes its independently owned service.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	serviceCfg := cfg
	serviceCfg.UserDataDir = isolatedBrowserProfile(t.TempDir())
	enabled := true
	serviceCfg.CDPMode = &enabled
	serviceCfg.CDPPort = port
	service := &browserManager{cfg: serviceCfg}
	owned, err := service.openBrowserConnection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), browserShutdownTimeout)
		defer stop()
		if err := owned.closeResources(cleanup); err != nil {
			t.Error(err)
		}
	}()
	attachCfg := serviceCfg
	attachCfg.CDPConnectExisting = &enabled
	attached, err := (&browserManager{cfg: attachCfg}).openBrowserConnection(ctx)
	if err != nil {
		t.Fatal("verified CDP attach:", err)
	}
	if err := attached.closeResources(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := owned.browser.Version(); err != nil {
		t.Fatal("attachment cleanup terminated service:", err)
	}
	t.Log("isolated shell read/screenshot/cookies, instance cleanup, and CDP ownership passed at port " + strconv.Itoa(port))

	// Damaged/missing binaries must fail without discovering desktop Chrome.
	if err := os.Rename(shell, shell+".missing"); err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeExecutable(false); err == nil {
		t.Fatal("missing shell accepted")
	}
	if err := os.Rename(shell+".missing", shell); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(headed) == filepath.Base(shell) {
		t.Fatal("headed/headless runtimes conflated")
	}
}

func TestIsolatedRuntimeRepairAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_RUNTIME_ACCEPTANCE") != "1" {
		t.Skip("vendor runtime acceptance is opt-in")
	}
	t.Setenv("DIGCRAWL_BROWSER_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	archives := os.Getenv("DIGCRAWL_RUNTIME_ARCHIVES")
	if err := InstallBrowserRuntime(ctx, archives); err != nil {
		t.Fatal(err)
	}
	shell, err := RuntimeExecutable(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shell, []byte("corrupt fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := InstallBrowserRuntime(ctx, archives); err == nil {
		t.Fatal("implicit replacement of corrupt immutable version")
	}
	if err := RepairBrowserRuntime(ctx, archives); err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeExecutable(false); err != nil {
		t.Fatal(err)
	}
	root, _ := browserRuntimeRoot()
	previous, err := filepath.Glob(filepath.Join(root, ".previous-*"))
	if err != nil || len(previous) != 1 {
		t.Fatal("previous runtime not retained", err)
	}
	preserved := filepath.Join(previous[0], runtimeExecutableRelative("chrome-headless-shell", browserRuntimePlatform()))
	data, err := os.ReadFile(preserved)
	if err != nil || string(data) != "corrupt fixture" {
		t.Fatal("repair discarded previous runtime", err)
	}
	// An intact executable does not prove its runtime libraries/resources are
	// intact. Explicit repair must bypass the executable-only fast path.
	resource := filepath.Join(filepath.Dir(shell), "icudtl.dat")
	if err := os.WriteFile(resource, []byte("damaged resource"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeExecutable(false); err != nil {
		t.Fatal("executable pin unexpectedly changed", err)
	}
	if err := RepairBrowserRuntime(ctx, archives); err != nil {
		t.Fatal("resource repair:", err)
	}
	info, err := os.Stat(resource)
	if err != nil || info.Size() < 1000 {
		t.Fatal("resource repair did not restore distribution", err)
	}

}

func TestInstallProbeCleanupFailureRetainsOwnerAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_RUNTIME_ACCEPTANCE") != "1" {
		t.Skip("vendor runtime acceptance is opt-in")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix ps failure fixture")
	}
	root := t.TempDir()
	t.Setenv("DIGCRAWL_BROWSER_DIR", root)
	path := os.Getenv("PATH")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ps"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+path)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	archives := os.Getenv("DIGCRAWL_RUNTIME_ARCHIVES")
	err := InstallBrowserRuntime(ctx, archives)
	if err == nil || !strings.Contains(err.Error(), "cleanup incomplete") {
		t.Fatalf("unknown cleanup did not fail visibly: %v", err)
	}
	markers, _ := filepath.Glob(filepath.Join(root, ".install-*", "probe-owner.json"))
	if len(markers) != 1 {
		t.Fatal("unreleased ownership evidence was removed")
	}
	if err := InstallBrowserRuntime(ctx, archives); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatal("retry bypassed unresolved owner", err)
	}
	t.Setenv("PATH", path)
	var owner struct {
		PID int `json:"pid"`
	}
	data, _ := os.ReadFile(markers[0])
	if err := json.Unmarshal(data, &owner); err != nil || owner.PID <= 0 {
		t.Fatal("retained stage has no exact owner PID")
	}
	// Browser.close precedes the failed verification. Confirm that it really
	// exited before this test's temporary-directory cleanup is allowed.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		alive, err := profileProcessAlive(owner.PID)
		if err != nil {
			t.Fatal(err)
		}
		if !alive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("probe process remained live after its graceful close")
}
