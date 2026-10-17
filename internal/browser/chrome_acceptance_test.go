package browser

import (
	"context"
	"errors"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
)

// Opt-in, real-process acceptance. It uses only a temporary dedicated profile.
func TestChromeLifecycleAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	headless := true
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 3, TotalTimeoutSec: 90, NavigateTimeoutSec: 10, WaitSelectorTimeoutSec: 5})
	if cfg.ChromePath == "" {
		t.Fatal("Chrome dependency missing")
	}
	previous := *config.Get()
	config.Get().LocalBrowser = cfg
	config.Get().Gateway.AllowedDomains = nil
	defer func() { *config.Get() = previous }()
	m := &browserManager{cfg: cfg, slots: make(chan struct{}, 3), sessions: make(map[uint64]*browserSession), stopCh: make(chan struct{})}
	defer m.shutdownImmediately(context.Background())
	postFinished := make(chan struct{})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-postFinished:
			}
			return
		}
		_, _ = w.Write([]byte("<html><head><title>lifecycle acceptance</title></head><body><article>confirmed browser body</article></body></html>"))
	}))
	defer server.Close()
	read := func() (*BrowserBrowseResponse, error) {
		return browserBrowseWithSessionRunner(context.Background(), &BrowserBrowseRequest{URL: server.URL, Mode: "full"}, func() *browserManager { return m }, nil)
	}
	first, err := read()
	if err != nil || first == nil || first.Title != "lifecycle acceptance" {
		t.Fatalf("first read=%v response=%+v", err, first)
	}
	m.mu.Lock()
	initial := m.active
	m.mu.Unlock()
	// Concurrent operation checks exercise the same admission and release path.
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := read(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	// Exercise the production page-context fetch path, without a remote site.
	oldDomains := config.Get().LocalBrowser.AllowedDomains
	config.Get().LocalBrowser.AllowedDomains = nil
	defer func() { config.Get().LocalBrowser.AllowedDomains = oldDomains }()
	fetchCtx := WithBrowseRetryContext(context.Background())
	result, apiErr := pageFetchWithSessionRunner(fetchCtx, &PageFetchRequest{WarmURL: server.URL, FetchURL: server.URL}, func() *browserManager { return m }, nil)
	if apiErr != nil || result == nil || result.Status != 200 || !strings.Contains(result.Body, "confirmed browser body") || result.Diagnostics["attempts"] != 1 {
		t.Fatalf("API-first result=%+v error=%v", result, apiErr)
	}
	postCtx, postCancel := context.WithTimeout(WithBrowseRetryContext(context.Background()), 4*time.Second)
	_, postErr := pageFetchWithSessionRunner(postCtx, &PageFetchRequest{WarmURL: server.URL, FetchURL: server.URL, Method: "POST", Body: `{}`}, func() *browserManager { return m }, nil)
	postCancel()
	close(postFinished)
	var be *BrowseError
	if !errors.As(postErr, &be) || be.Stage != "page_fetch" || be.Diagnostics["retry_count"] != 0 || posts.Load() != 1 {
		t.Fatalf("POST outcome err=%v posts=%d", postErr, posts.Load())
	}
	t.Log("API-first GET shares diagnostics; cancelled POST sent once and was not replayed")
	if os.Getenv("DIGCRAWL_BROWSER_LONG_IDLE") == "1" {
		time.Sleep(92 * time.Second)
		if _, err = read(); err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		same := m.active == initial
		m.mu.Unlock()
		if !same {
			t.Fatal("shared connection replaced after idle deadline")
		}
		t.Log("real 92-second idle reuse passed")
	}
	baseGoroutines := runtime.NumGoroutine()
	baseFD := acceptanceFDCount(t, os.Getpid())
	var baselineTargets int
	for round := 1; round <= 20; round++ {
		m.mu.Lock()
		old := m.active
		m.mu.Unlock()
		old.launcher.Kill() // owned process-group crash, leaves Chromium Singleton residue
		resp, err := read()
		if err != nil {
			var e *BrowseError
			errors.As(err, &e)
			if e != nil {
				t.Fatalf("round %d: %v; isolated fixture cause: %v", round, err, e.Cause)
			}
			t.Fatalf("round %d: %v", round, err)
		}
		if resp.Diagnostics["retry_count"] != 1 || resp.Diagnostics["recovery_status"] != "succeeded" {
			t.Fatalf("round %d diagnostics=%v", round, resp.Diagnostics)
		}
		m.mu.Lock()
		current := m.active
		leases := current.leases
		slots := len(m.slots)
		sessions := len(m.sessions)
		m.mu.Unlock()
		if current == old || leases != 0 || slots != 0 || sessions != 0 {
			t.Fatalf("round %d ownership leases=%d slots=%d sessions=%d", round, leases, slots, sessions)
		}
		groupCtx, groupCancel := context.WithTimeout(context.Background(), time.Second)
		groupErr := waitForOwnedProcessRelease(groupCtx, old)
		groupCancel()
		if groupErr != nil {
			t.Fatalf("round %d old owned group remains: %v", round, groupErr)
		}
		fd := acceptanceFDCount(t, os.Getpid())
		if fd > baseFD+12 {
			t.Fatalf("round %d FD growth: before=%d current=%d", round, baseFD, fd)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pages, pageErr := current.browser.Context(ctx).Pages()
		cancel()
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if round == 1 {
			baselineTargets = len(pages)
		} else if len(pages) > baselineTargets {
			t.Fatalf("target growth at round %d: %d > %d", round, len(pages), baselineTargets)
		}
		t.Logf("round=%d old_group_released=true targets=%d leases=%d slots=%d goroutines=%d fd=%d", round, len(pages), leases, slots, runtime.NumGoroutine(), fd)
	}
	if runtime.NumGoroutine() > baseGoroutines+12 {
		t.Fatalf("goroutine growth: before=%d after=%d", baseGoroutines, runtime.NumGoroutine())
	}
	if err = m.shutdownImmediately(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = waitForProfileUnlock(context.Background(), cfg.UserDataDir); err != nil {
		t.Fatal(err)
	}
}

func acceptanceFDCount(t *testing.T, pid int) int {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "lsof", "-p", strconv.Itoa(pid), "-Ff").Output()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		if len(line) > 1 && line[0] == 'f' && line[1] >= '0' && line[1] <= '9' {
			count++
		}
	}
	return count
}

// Exercise the production read tail, not the session-runner seam: extracted
// text must not hide a failed metadata/media step, and cancellation must leave
// the healthy shared browser usable for the next request.
func TestChromeReadTailFailureAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	headless := true
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 30, NavigateTimeoutSec: 5, WaitSelectorTimeoutSec: 3})
	previous := *config.Get()
	config.Get().LocalBrowser = cfg
	config.Get().Gateway.AllowedDomains = nil
	defer func() { *config.Get() = previous }()
	m := &browserManager{cfg: cfg, slots: make(chan struct{}, 1), sessions: make(map[uint64]*browserSession), stopCh: make(chan struct{})}
	defer func() {
		if err := m.shutdownImmediately(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	metadataStarted := make(chan struct{}, 1)
	var disconnectOnce atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/disconnect-metadata" {
			if disconnectOnce.CompareAndSwap(false, true) {
				m.mu.Lock()
				conn := m.active
				m.mu.Unlock()
				safeCloseCDPTransport(conn.transport)
			}
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.URL.Path == "/metadata-started" {
			metadataStarted <- struct{}{}
			_, _ = w.Write([]byte("ok"))
			return
		}
		_, _ = w.Write([]byte("<html><head><title>read tail acceptance</title></head><body><article>confirmed article body</article></body></html>"))
	}))
	defer server.Close()
	read := func(ctx context.Context, req *BrowserBrowseRequest) (*BrowserBrowseResponse, error) {
		req.URL, req.ContentSelector = server.URL, "article"
		return browserBrowseWithSessionRunner(ctx, req, func() *browserManager { return m }, nil)
	}
	if _, err := read(context.Background(), &BrowserBrowseRequest{}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	healthy := m.active
	m.mu.Unlock()
	for _, tc := range []struct {
		name, script, stage, reason string
		media                       bool
	}{
		{name: "metadata_deadline", script: `async () => { await new Promise(r => setTimeout(r, 10000)); return {title:"metadata"}; }`, stage: "metadata", reason: "budget_exhausted"},
		{name: "metadata_cancel", script: `async () => { await fetch('/metadata-started'); return new Promise(() => {}); }`, stage: "metadata", reason: "cancelled"},
		{name: "metadata_invalid_value", script: `() => "invalid metadata fixture EOF timeout"`, stage: "metadata", reason: "unclassified"},
		{name: "metadata_null", script: `() => null`, stage: "metadata", reason: "unclassified"},
		{name: "metadata_script_error", script: `() => {throw new Error("invalid metadata fixture EOF timeout");}`, stage: "metadata", reason: "unclassified"},
		{name: "media_deadline", script: `() => { const p = document.createElement('div'); p.className = 'player'; p.style.cssText = 'width:100px;height:100px'; document.body.appendChild(p); return {}; }`, media: true, stage: "capture_media", reason: "budget_exhausted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit := 10 * time.Second
			if tc.reason == "budget_exhausted" {
				limit = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), limit)
			defer cancel()
			if tc.reason == "cancelled" {
				go func() {
					select {
					case <-metadataStarted:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			resp, err := read(ctx, &BrowserBrowseRequest{MetadataScript: tc.script, CaptureMedia: tc.media})
			var be *BrowseError
			if resp != nil || !errors.As(err, &be) || be.Stage != tc.stage || be.ToolErrorDetails()["reason"] != tc.reason || be.Diagnostics["retry_count"] != 0 || be.Diagnostics["next_actions"] == nil {
				t.Fatalf("resp=%+v err=%v typed=%+v", resp, err, be)
			}
			if strings.Contains(err.Error(), "invalid metadata fixture") {
				t.Fatal("raw metadata value leaked into the tool error")
			}
			m.mu.Lock()
			active, leases, slots, sessions := m.active, healthy.leases, len(m.slots), len(m.sessions)
			m.mu.Unlock()
			if active != healthy || leases != 0 || slots != 0 || sessions != 0 {
				t.Fatalf("healthy browser ownership changed: same=%v leases=%d slots=%d sessions=%d", active == healthy, leases, slots, sessions)
			}
			recovered, err := read(context.Background(), &BrowserBrowseRequest{MetadataScript: `() => ({title:"valid metadata"})`})
			if err != nil || recovered == nil || recovered.Metadata["title"] != "valid metadata" || recovered.Text != "confirmed article body" {
				t.Fatalf("next read failed: resp=%+v err=%v", recovered, err)
			}
			pages, err := healthy.browser.Pages()
			if err != nil || len(pages) != 0 {
				t.Fatalf("read target leaked: pages=%d err=%v", len(pages), err)
			}
			t.Log("typed failure preserved; healthy connection reused; targets/leases/slots/sessions=0")
		})
	}
	t.Run("metadata_transport_disconnect", func(t *testing.T) {
		resp, err := read(context.Background(), &BrowserBrowseRequest{MetadataScript: `async () => { await fetch('/disconnect-metadata'); await new Promise(r => setTimeout(r, 250)); return {title:"recovered metadata"}; }`})
		if err != nil || resp == nil || resp.Metadata["title"] != "recovered metadata" || resp.Diagnostics["retry_count"] != 1 || resp.Diagnostics["attempts"] != 2 || resp.Diagnostics["recovery_status"] != "succeeded" {
			t.Fatalf("metadata disconnect recovery failed: resp=%+v err=%v", resp, err)
		}
		m.mu.Lock()
		current, slots, sessions := m.active, len(m.slots), len(m.sessions)
		leases := current.leases
		m.mu.Unlock()
		if current == healthy || leases != 0 || slots != 0 || sessions != 0 {
			t.Fatalf("invalid replacement ownership: same=%v leases=%d slots=%d sessions=%d", current == healthy, leases, slots, sessions)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := waitForOwnedProcessRelease(ctx, healthy); err != nil {
			t.Fatal(err)
		}
		pages, err := current.browser.Context(ctx).Pages()
		if err != nil || len(pages) != 0 {
			t.Fatalf("replacement leaked read target: pages=%d err=%v", len(pages), err)
		}
		t.Log("metadata CDP failure recovered once; old process released; targets/leases/slots/sessions=0")
	})
}

func TestChromeExternalCDPOwnershipAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	headless, external := true, true
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{Headless: &headless, UserDataDir: t.TempDir(), ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH")})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l := applyBackgroundLauncherPolicy(launcher.New().Context(ctx).Bin(cfg.ChromePath).Headless(true).NoSandbox(true).UserDataDir(cfg.UserDataDir), nil)
	control, err := l.Launch()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Kill()
	u, err := url.Parse(control)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CDPPort, err = strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	cfg.CDPConnectExisting = &external
	m := &browserManager{cfg: cfg, slots: make(chan struct{}, 3), sessions: make(map[uint64]*browserSession), stopCh: make(chan struct{})}
	s, err := m.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.release(s); err != nil {
		t.Fatal(err)
	}
	// Lose the CDP transport with a known live page. The caller must receive
	// cleanup instructions, and a replacement must not discard that target.
	uncertain, err := m.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lostTarget := uncertain.page.TargetID
	safeCloseCDPTransport(uncertain.conn.transport)
	if err = m.release(uncertain); browseCauseReason(err) != "external_target_cleanup_required" {
		t.Fatalf("cleanup falsely succeeded: %v", err)
	}
	<-uncertain.conn.stopped
	if _, err = m.acquireConnection(ctx); browseCauseReason(err) != "external_target_cleanup_required" {
		t.Fatalf("replacement bypassed unresolved target: %v", err)
	}
	if err = m.shutdownImmediately(ctx); browseCauseReason(err) != "external_target_cleanup_required" {
		t.Fatalf("shutdown forgot target: %v", err)
	}
	// A fresh connection must still be able to use the externally owned Chrome.
	b, transport, err := connectCDPBrowser(ctx, control, time.Second)
	if err != nil {
		t.Fatal("shutdown terminated external Chrome:", err)
	}
	defer safeCloseCDPTransport(transport)
	if _, err = b.Context(ctx).Version(); err != nil {
		t.Fatal(err)
	}
	// Simulate the instructed operator cleanup through a fresh connection, and
	// close only the target whose ID was retained by this test.
	if err = closeTargetWithin(b, lostTarget, time.Second); err != nil {
		t.Fatal(err)
	}
	targets, err := (proto.TargetGetTargets{}).Call(b.Context(ctx))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets.TargetInfos {
		if target.TargetID == lostTarget || target.Type == "page" {
			t.Fatalf("external page remains: type=%s id=%s", target.Type, target.TargetID)
		}
	}

	t.Log("external owner preserved; failed target cleanup quarantined replacement; targeted manual cleanup leaves zero targets")
}
