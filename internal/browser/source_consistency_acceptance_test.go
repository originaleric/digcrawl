package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
)

func TestChromeSourceConsistencyAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	headless := true
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 20, NavigateTimeoutSec: 5, WaitSelectorTimeoutSec: 1})
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/a", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<html><head><title>Article A</title></head><body><article>Body A</article><p>Summary A</p><button>Button A</button><script>
window.addEventListener('review-next', () => { history.replaceState({}, '', window.nextURL); document.title='Article B'; document.querySelector('article').textContent='Body B'; });
</script></body></html>`)
	}))
	defer server.Close()
	read := func(req *BrowserBrowseRequest) (*BrowserBrowseResponse, error) {
		return browserBrowseWithSessionRunner(context.Background(), req, func() *browserManager { return m }, nil)
	}
	if _, err := read(&BrowserBrowseRequest{URL: server.URL + "/a", ContentSelector: "article"}); err != nil {
		t.Fatal(err)
	}
	healthy := m.active
	original := healthy.browser.Context(context.Background())
	restore := func() { healthy.browser.Client(&readBoundaryTestCDP{call: original.Call}) }
	assertRejected := func(t *testing.T, req *BrowserBrowseRequest) {
		t.Helper()
		resp, err := read(req)
		var be *BrowseError
		if resp != nil || !errors.As(err, &be) || be.Stage != "read_page" || be.ToolErrorDetails()["reason"] != "page_changed" || be.Diagnostics["retry_count"] != 0 || be.Retryable || CanBrowseFallback(err) {
			t.Fatalf("resp=%+v err=%v be=%+v", resp, err, be)
		}
		actions, _ := be.Diagnostics["next_actions"].([]map[string]any)
		if len(actions) != 1 || actions[0]["type"] != "retry_read" || !strings.Contains(actions[0]["description"].(string), "页面稳定") {
			t.Fatalf("missing page-change guidance: %v", actions)
		}
		restore()
		if m.active != healthy || healthy.leases != 0 || len(m.slots) != 0 || len(m.sessions) != 0 {
			t.Fatal("page change retired a healthy connection or leaked resources")
		}
		pages, err := healthy.browser.Pages()
		if err != nil || len(pages) != 0 {
			t.Fatalf("targets=%d err=%v", len(pages), err)
		}
		if resp, err := read(&BrowserBrowseRequest{URL: server.URL + "/a", ContentSelector: "article"}); err != nil || resp.Text != "Body A" {
			t.Fatalf("next read resp=%+v err=%v", resp, err)
		}
	}
	for _, tc := range []struct{ name, mode, action, nextURL string }{
		{name: "full_path", nextURL: "/b"},
		{name: "full_query", nextURL: "/a?article=b"},
		{name: "full_fragment", nextURL: "/a#article-b"},
		{name: "summary", mode: "summary", nextURL: "/b"},
		{name: "snapshot", mode: "snapshot", nextURL: "/b"},
		{name: "screenshot", action: "screenshot", nextURL: "/b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := fmt.Sprintf(`async () => { window.nextURL=%q; setTimeout(() => window.dispatchEvent(new Event('review-next')), 0); await new Promise(r=>setTimeout(r,100)); return {text:document.querySelector('article').textContent}; }`, tc.nextURL)
			assertRejected(t, &BrowserBrowseRequest{URL: server.URL + "/a", ContentSelector: "article", Mode: tc.mode, Action: tc.action, CaptureScreenshot: tc.action == "", MetadataScript: script})
		})
	}
	t.Run("same_url_document_reload", func(t *testing.T) {
		var loader proto.NetworkLoaderID
		frames := 0
		reloaded := false
		healthy.browser.Client(&readBoundaryTestCDP{call: func(ctx context.Context, sid, method string, params any) ([]byte, error) {
			if method != "Page.getFrameTree" {
				return original.Call(ctx, sid, method, params)
			}
			frames++
			if frames == 2 {
				if _, err := original.Call(ctx, sid, "Page.reload", proto.PageReload{}); err != nil {
					return nil, err
				}
				reloaded = true
			}
			for {
				data, err := original.Call(ctx, sid, method, params)
				if err != nil {
					return data, err
				}
				var result proto.PageGetFrameTreeResult
				if err := json.Unmarshal(data, &result); err != nil {
					return nil, err
				}
				if frames == 1 {
					loader = result.FrameTree.Frame.LoaderID
					return data, nil
				}
				if result.FrameTree.Frame.LoaderID != loader {
					return data, nil
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
			}
		}})
		defer restore()
		assertRejected(t, &BrowserBrowseRequest{URL: server.URL + "/a", ContentSelector: "article"})
		if !reloaded {
			t.Fatal("reload fixture was not exercised")
		}
	})
	t.Run("stable_redirect_and_title", func(t *testing.T) {
		resp, err := read(&BrowserBrowseRequest{URL: server.URL + "/redirect", ContentSelector: "article", MetadataScript: `() => {document.title='Completed title'; return {text:document.querySelector('article').textContent};}`})
		if err != nil || resp.URL != server.URL+"/a" || resp.Text != "Body A" || resp.Title != "Completed title" {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
	})
	t.Run("stable_snapshot", func(t *testing.T) {
		resp, err := read(&BrowserBrowseRequest{URL: server.URL + "/a", Mode: "snapshot"})
		if err != nil || resp == nil {
			t.Fatalf("snapshot resp=%+v err=%v", resp, err)
		}
		for _, element := range resp.Elements {
			if element.Role == "button" && element.Name == "Button A" {
				return
			}
		}
		t.Fatalf("snapshot omitted fixture button: %v", resp.Elements)
	})
}

func TestChromeDedicatedSnapshotAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	previous := *config.Get()
	headless := true
	config.Get().LocalBrowser = config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), CookieStoreDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 15, NavigateTimeoutSec: 5}
	config.Get().Gateway.AllowedDomains = nil
	m := getBrowserManager()
	defer func() {
		if err := m.shutdownImmediately(context.Background()); err != nil {
			t.Error(err)
		}
		*config.Get() = previous
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://"+strings.Replace(r.Host, "localhost", "127.0.0.1", 1)+"/a", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<html><head><title>Snapshot A</title></head><body><article>Body A</article><button>Button A</button></body></html>`)
	}))
	defer server.Close()
	read := func(ctx context.Context, filter string, depth int) (*BrowserSnapshotResponse, error) {
		return BrowserSnapshot(ctx, &BrowserSnapshotRequest{URL: server.URL + "/a", Filter: filter, MaxDepth: depth})
	}
	all, err := read(context.Background(), FilterAll, -1)
	if err != nil {
		t.Fatal(err)
	}
	if all.URL != server.URL+"/a" || len(all.Elements) <= 1 || len(all.Refs) == 0 {
		t.Fatalf("snapshot=%+v", all)
	}
	for _, node := range all.Elements {
		if node.NodeID != 0 && all.Refs[node.Ref] != node.NodeID {
			t.Fatalf("refs=%v node=%v", all.Refs, node)
		}
	}
	limited, err := read(context.Background(), FilterAll, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Elements) >= len(all.Elements) {
		t.Fatal("depth was not applied")
	}
	for _, node := range limited.Elements {
		if node.Depth > 1 {
			t.Fatal(node)
		}
	}
	healthy := m.active
	original := healthy.browser.Context(context.Background())
	for _, fault := range []string{"page_changed", "send_queue", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			injected := false
			healthy.browser.Client(&readBoundaryTestCDP{call: func(ctx context.Context, sid, method string, params any) ([]byte, error) {
				if method != "Accessibility.getFullAXTree" || injected {
					return original.Call(ctx, sid, method, params)
				}
				injected = true
				switch fault {
				case "send_queue":
					return nil, &cdpSendError{operation: "send_queue", cause: errors.New("fixture queue busy")}
				case "cancelled":
					cancel()
					return nil, context.Canceled
				default:
					data, err := original.Call(ctx, sid, method, params)
					if err != nil {
						return nil, err
					}
					_, err = original.Call(ctx, sid, "Runtime.evaluate", proto.RuntimeEvaluate{Expression: `history.replaceState({},'', '/changed'); document.title='Snapshot B';`})
					return data, err
				}
			}})
			resp, err := read(ctx, FilterInteractive, -1)
			healthy.browser.Client(&readBoundaryTestCDP{call: original.Call})
			var be *BrowseError
			if !injected || resp != nil || !errors.As(err, &be) || be.ToolErrorDetails()["reason"] != fault || be.Diagnostics["retry_count"] != 0 || CanBrowseFallback(err) {
				t.Fatalf("resp=%v err=%v details=%+v", resp, err, be)
			}
			if m.active != healthy || healthy.leases != 0 || len(m.slots) != 0 || len(m.sessions) != 0 {
				t.Fatal("snapshot leaked or retired a healthy connection")
			}
			pages, err := healthy.browser.Pages()
			if err != nil || len(pages) != 0 {
				t.Fatalf("targets=%d err=%v", len(pages), err)
			}
			if resp, err := read(context.Background(), FilterInteractive, -1); err != nil || len(resp.Elements) != 1 || resp.Elements[0].Name != "Button A" {
				t.Fatalf("next read=%+v err=%v", resp, err)
			}
		})
	}
	t.Run("local_domain_restriction", func(t *testing.T) {
		config.Get().LocalBrowser.AllowedDomains = []string{"localhost"}
		defer func() { config.Get().LocalBrowser.AllowedDomains = nil }()
		input := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/redirect"
		if resp, err := BrowserSnapshot(context.Background(), &BrowserSnapshotRequest{URL: input}); resp != nil || err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") {
			t.Fatalf("snapshot=%v err=%v", resp, err)
		}
	})
}
