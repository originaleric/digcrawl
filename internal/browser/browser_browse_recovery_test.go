package browser

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
)

func TestBrowserBrowseRebuildsBridgeOwnedConnectionOnceAfterCDPDisconnect(t *testing.T) {
	firstStopped := make(chan struct{})
	first := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		close(firstStopped)
		return nil
	})
	second := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	first.leases = 1

	var opens atomic.Int32
	var acquires atomic.Int32
	manager := &browserManager{
		cfg:    config.LocalBrowserConfig{TotalTimeoutSec: 1},
		active: first,
		openFn: func(context.Context) (*browserConnection, error) {
			opens.Add(1)
			return second, nil
		},
	}
	manager.acquireSessionFn = func(ctx context.Context) (*browserSession, error) {
		if acquires.Add(1) == 1 {
			return &browserSession{conn: first}, nil
		}
		conn, err := manager.acquireConnection(ctx)
		if err != nil {
			return nil, err
		}
		return &browserSession{conn: conn}, nil
	}
	manager.releaseSessionFn = manager.release

	var attempts atomic.Int32
	runner := func(_ context.Context, _ *BrowserBrowseRequest, session *browserSession) (*BrowserBrowseResponse, error) {
		if attempts.Add(1) == 1 {
			if session.conn != first {
				t.Fatalf("first attempt used unexpected connection")
			}
			return nil, newBrowseError(ErrorBrowserNavigationFailed, "navigate", "页面导航失败", errors.New("websocket closed"), true)
		}
		if session.conn != second {
			t.Fatalf("retry reused failed connection")
		}
		select {
		case <-firstStopped:
		default:
			t.Fatal("replacement started before the failed browser owner stopped")
		}
		return &BrowserBrowseResponse{URL: "https://example.com/article", Markdown: "recovered body"}, nil
	}

	response, err := browserBrowseWithSessionRunner(context.Background(), &BrowserBrowseRequest{URL: "https://example.com/article"}, func() *browserManager { return manager }, runner)
	if err != nil {
		t.Fatalf("browser read after one recoverable disconnect: %v", err)
	}
	if response == nil || response.Markdown != "recovered body" {
		t.Fatalf("response=%+v", response)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts=%d, want one replay", got)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("replacement launches=%d, want 1", got)
	}
	if !first.retired {
		t.Fatal("failed connection was not retired")
	}
}

func TestBrowserBrowseDoesNotReplayPageSpecificFailure(t *testing.T) {
	conn := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	conn.leases = 1
	manager := &browserManager{cfg: config.LocalBrowserConfig{TotalTimeoutSec: 1}, active: conn}
	manager.acquireSessionFn = func(context.Context) (*browserSession, error) {
		return &browserSession{conn: conn}, nil
	}
	manager.releaseSessionFn = manager.release

	var attempts atomic.Int32
	runner := func(_ context.Context, _ *BrowserBrowseRequest, _ *browserSession) (*BrowserBrowseResponse, error) {
		attempts.Add(1)
		return nil, newBrowseError(ErrorBrowserContentNotFound, "extract_content", "提取页面正文失败", errors.New("target closed"), false)
	}

	_, err := browserBrowseWithSessionRunner(context.Background(), &BrowserBrowseRequest{URL: "https://example.com/article"}, func() *browserManager { return manager }, runner)
	if err == nil {
		t.Fatal("expected page-specific failure")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, want no replay", got)
	}
	if conn.retired {
		t.Fatal("page-specific failure must not tear down the shared browser")
	}
}

func TestShouldRecoverBrowserConnectionKeepsOriginalDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	err := newBrowseError(ErrorBrowserNavigationFailed, "navigate", "页面导航失败", errors.New("websocket closed"), true)
	if shouldRecoverBrowserConnection(ctx, err) {
		t.Fatal("expired request must not start a replacement replay")
	}
}
