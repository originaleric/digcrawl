package browser

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"
	"github.com/originaleric/digcrawl/config"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Test-only interception preserves the actual Chrome page/manager lifecycle.
type readBoundaryTestCDP struct {
	call func(context.Context, string, string, any) ([]byte, error)
}

func (c *readBoundaryTestCDP) Event() <-chan *cdp.Event { return nil }
func (c *readBoundaryTestCDP) Call(ctx context.Context, sid, method string, params any) ([]byte, error) {
	return c.call(ctx, sid, method, params)
}

func TestChromeReadBoundaryAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	headless := true
	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 30, NavigateTimeoutSec: 5, WaitSelectorTimeoutSec: 1})
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
			http.Redirect(w, r, "http://"+strings.Replace(r.Host, "localhost", "127.0.0.1", 1)+"/foreign", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<html><head><title>read boundary</title></head><body><article>confirmed body</article><p>confirmed summary</p></body></html>`)
	}))
	defer server.Close()
	read := func(req *BrowserBrowseRequest) (*BrowserBrowseResponse, error) {
		return browserBrowseWithSessionRunner(context.Background(), req, func() *browserManager { return m }, nil)
	}
	if _, err := read(&BrowserBrowseRequest{URL: server.URL, ContentSelector: "body"}); err != nil {
		t.Fatal(err)
	}
	healthy := m.active
	original := healthy.browser.Context(context.Background())
	restore := func() { healthy.browser.Client(&readBoundaryTestCDP{call: original.Call}) }
	for _, tc := range []struct{ name, mode, selector, stage string }{
		{name: "load_queue", selector: "body", stage: "wait_load"},
		{name: "inspect_queue", selector: "body", stage: "read_page"},
		{name: "final_inspect_queue", selector: "body", stage: "read_page"},
		{name: "document_queue", selector: "body", stage: "read_page"},
		{name: "final_document_queue", selector: "body", stage: "read_page"},
		{name: "selector_queue", stage: "extract_content"},
		{name: "html_queue", selector: "body", stage: "extract_content"},
		{name: "text_queue", selector: "body", stage: "extract_content"},
		{name: "summary_queue", mode: "summary", stage: "summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			injected, navigated := false, false
			infos, frames := 0, 0
			cause := &cdpSendError{operation: "send_queue", cause: errors.New("sender busy")}
			healthy.browser.Client(&readBoundaryTestCDP{call: func(ctx context.Context, sid, method string, params any) ([]byte, error) {
				if method == "Page.navigate" {
					navigated = true
				}
				if navigated && method == "Target.getTargetInfo" {
					infos++
				}
				if navigated && method == "Page.getFrameTree" {
					frames++
				}
				trigger := tc.name == "load_queue" && method == "Runtime.evaluate" || tc.name == "inspect_queue" && method == "Target.getTargetInfo" || tc.name == "final_inspect_queue" && method == "Target.getTargetInfo" && infos == 2 || tc.name == "html_queue" && method == "DOM.getOuterHTML"
				trigger = trigger || tc.name == "document_queue" && method == "Page.getFrameTree" || tc.name == "final_document_queue" && method == "Page.getFrameTree" && frames == 2
				if arg, ok := params.(proto.RuntimeCallFunctionOn); ok {
					if tc.name == "text_queue" && strings.Contains(arg.FunctionDeclaration, "functions.text") {
						trigger = true
					}
					for _, v := range arg.Arguments {
						if tc.name == "selector_queue" && v.Value.Str() == "article" || tc.name == "summary_queue" && v.Value.Str() == "p" {
							trigger = true
						}
					}
				}
				if navigated && !injected && trigger {
					injected = true
					return nil, cause
				}
				return original.Call(ctx, sid, method, params)
			}})
			defer restore()
			resp, err := read(&BrowserBrowseRequest{URL: server.URL, Mode: tc.mode, ContentSelector: tc.selector})
			var be *BrowseError
			if !injected || resp != nil || !errors.As(err, &be) || !errors.Is(err, cause) || be.Stage != tc.stage || be.ToolErrorDetails()["reason"] != "send_queue" || be.Diagnostics["retry_count"] != 0 || CanBrowseFallback(err) {
				t.Fatalf("resp=%+v err=%v be=%+v injected=%v", resp, err, be, injected)
			}
			actions, _ := be.Diagnostics["next_actions"].([]map[string]any)
			if len(actions) != 1 || actions[0]["type"] != "wait" {
				t.Fatalf("actions=%v", actions)
			}
			restore()
			if m.active != healthy || healthy.leases != 0 || len(m.slots) != 0 || len(m.sessions) != 0 {
				t.Fatal("queue failure changed ownership or leaked lease")
			}
			pages, err := healthy.browser.Pages()
			if err != nil || len(pages) != 0 {
				t.Fatalf("targets=%d err=%v", len(pages), err)
			}
			if _, err = read(&BrowserBrowseRequest{URL: server.URL, ContentSelector: "body"}); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("document_interruption", func(t *testing.T) {
		injected, navigated := false, false
		healthy.browser.Client(&readBoundaryTestCDP{call: func(ctx context.Context, sid, method string, params any) ([]byte, error) {
			if method == "Page.navigate" {
				navigated = true
			}
			if arg, ok := params.(proto.RuntimeCallFunctionOn); ok && navigated && !injected && method == "Runtime.callFunctionOn" && arg.AwaitPromise {
				result, err := original.Call(ctx, sid, method, params)
				if err != nil {
					return result, err
				}
				injected = true
				return nil, errors.New("document replaced")
			}
			return original.Call(ctx, sid, method, params)
		}})
		defer restore()
		resp, err := read(&BrowserBrowseRequest{URL: server.URL, ContentSelector: "body"})
		if err != nil || resp == nil || (!strings.Contains(resp.Text, "confirmed body") || !strings.Contains(resp.Text, "confirmed summary")) || resp.Diagnostics["retry_count"] != 0 {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
	})
	restore()
	initialURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/redirect"
	t.Run("redirect_allowed", func(t *testing.T) {
		resp, err := read(&BrowserBrowseRequest{URL: initialURL, ContentSelector: "body"})
		if err != nil || resp == nil || resp.URL != server.URL+"/foreign" {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
	})
	t.Run("redirect_blocked", func(t *testing.T) {
		config.Get().Gateway.AllowedDomains = []string{"localhost"}
		defer func() { config.Get().Gateway.AllowedDomains = nil }()
		resp, err := read(&BrowserBrowseRequest{URL: initialURL, ContentSelector: "body"})
		if resp != nil || err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") {
			t.Fatalf("resp=%+v err=%v", resp, err)
		}
	})
	t.Run("late_redirect_blocked", func(t *testing.T) {
		config.Get().Gateway.AllowedDomains = []string{"127.0.0.1"}
		defer func() { config.Get().Gateway.AllowedDomains = nil }()
		infos := 0
		healthy.browser.Client(&readBoundaryTestCDP{call: func(ctx context.Context, sid, method string, params any) ([]byte, error) {
			if method == "Target.getTargetInfo" {
				infos++
				if infos == 2 {
					return []byte(`{"targetInfo":{"url":"https://blocked.example/","title":"late redirect"}}`), nil
				}
			}
			return original.Call(ctx, sid, method, params)
		}})
		defer restore()
		resp, err := read(&BrowserBrowseRequest{URL: server.URL, ContentSelector: "body"})
		if resp != nil || err == nil || !strings.Contains(err.Error(), "DOMAIN_NOT_ALLOWED") || infos != 2 {
			t.Fatalf("resp=%+v err=%v infos=%d", resp, err, infos)
		}
	})
}
