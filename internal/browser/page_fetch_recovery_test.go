package browser

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func fetchTestManager() *browserManager {
	return &browserManager{acquireSessionFn: func(context.Context) (*browserSession, error) { return &browserSession{}, nil }, releaseSessionFn: func(*browserSession) error { return nil }}
}
func TestPageFetchAndDOMReadShareRetryAndDeadline(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	m := fetchTestManager()
	var deadline time.Time
	calls := 0
	result, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api"}, func() *browserManager { return m }, func(ctx context.Context, _ *browserSession) (*PageFetchResult, error) {
		calls++
		d, _ := ctx.Deadline()
		if deadline.IsZero() {
			deadline = d
		} else if d != deadline {
			t.Fatal("fetch retry extended deadline")
		}
		if calls == 1 {
			return nil, newBrowseError(ErrorBrowserPageClosed, "page_fetch", "fetch lost connection", io.EOF, true)
		}
		return &PageFetchResult{Status: 200}, nil
	})
	if err != nil || calls != 2 || result.Diagnostics["retry_count"] != 1 {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
	reads := 0
	_, err = browserBrowseWithSessionRunner(ctx, &BrowserBrowseRequest{URL: "https://www.douyin.com"}, func() *browserManager { return m }, func(ctx context.Context, _ *BrowserBrowseRequest, _ *browserSession) (*BrowserBrowseResponse, error) {
		reads++
		d, _ := ctx.Deadline()
		if d != deadline {
			t.Fatal("DOM fallback restarted deadline")
		}
		return nil, newBrowseError(ErrorBrowserNavigationFailed, "navigate", "disconnected", io.EOF, true)
	})
	var be *BrowseError
	if reads != 1 || !errors.As(err, &be) || be.Diagnostics["attempts"] != 3 || be.Diagnostics["retry_count"] != 1 || be.Diagnostics["fallback_count"] != 1 {
		t.Fatalf("reads=%d err=%+v", reads, err)
	}
}
func TestPageFetchQueueFailureDoesNotRetry(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	m := fetchTestManager()
	calls := 0
	m.acquireSessionFn = func(context.Context) (*browserSession, error) {
		calls++
		return nil, &cdpSendError{operation: "send_queue", cause: context.DeadlineExceeded}
	}
	_, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api"}, func() *browserManager { return m }, nil)
	var be *BrowseError
	if calls != 1 || !errors.As(err, &be) || be.Diagnostics["retry_count"] != 0 || be.ToolErrorDetails()["reason"] != "send_queue" || be.Diagnostics["next_actions"] == nil {
		t.Fatalf("calls=%d error=%+v", calls, err)
	}
}
func TestPageFetchPostCannotReplayAfterFetch(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	m := fetchTestManager()
	calls := 0
	_, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api", Method: "POST"}, func() *browserManager { return m }, func(context.Context, *browserSession) (*PageFetchResult, error) {
		calls++
		return nil, newBrowseError(ErrorBrowserPageClosed, "page_fetch", "unknown POST outcome", io.EOF, true)
	})
	if err == nil || calls != 1 || BrowseDiagnostics(ctx)["retry_count"] != 0 {
		t.Fatalf("POST replayed calls=%d error=%v", calls, err)
	}
}
func TestPageFetchBusinessFailureRetainsFallbackAndCounts(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	m := fetchTestManager()
	result, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api"}, func() *browserManager { return m }, func(context.Context, *browserSession) (*PageFetchResult, error) {
		return &PageFetchResult{Status: 0}, errors.New("fetch business failure")
	})
	var be *BrowseError
	if err == nil || result == nil || errors.As(err, &be) || result.Diagnostics["attempts"] != 1 || result.Diagnostics["retry_count"] != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestSuccessfulPostIsNotReplayedAfterCleanupFailure(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	m := fetchTestManager()
	calls := 0
	m.releaseSessionFn = func(*browserSession) error { return io.EOF }
	_, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api", Method: "POST"}, func() *browserManager { return m }, func(context.Context, *browserSession) (*PageFetchResult, error) {
		calls++
		return &PageFetchResult{Status: 200}, nil
	})
	if err == nil || calls != 1 || BrowseDiagnostics(ctx)["retry_count"] != 0 {
		t.Fatalf("successful POST repeated after cleanup calls=%d err=%v", calls, err)
	}
}

func TestPageFetchDoesNotPublishSuccessAfterCleanupCancellation(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithCancel(WithBrowseRetryContext(context.Background()))
			defer cancel()
			m := fetchTestManager()
			calls, releases := 0, 0
			m.releaseSessionFn = func(*browserSession) error {
				releases++
				cancel()
				return nil
			}
			result, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api", Method: method}, func() *browserManager { return m }, func(context.Context, *browserSession) (*PageFetchResult, error) {
				calls++
				return &PageFetchResult{Status: 200}, nil
			})
			var be *BrowseError
			if result != nil || !errors.As(err, &be) || !errors.Is(err, context.Canceled) || calls != 1 || releases != 1 {
				t.Fatalf("result=%+v err=%v calls=%d releases=%d", result, err, calls, releases)
			}
			if be.Diagnostics["retry_count"] != 0 || be.Diagnostics["reason"] != "cancelled" || be.Diagnostics["replay_safe"] != (method == "GET") || be.Retryable {
				t.Fatalf("invalid cancellation/replay contract: %+v", be)
			}
		})
	}
}

func TestPostMayRecoverBeforeFetchIsSent(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	m := fetchTestManager()
	acquires, sends := 0, 0
	m.acquireSessionFn = func(context.Context) (*browserSession, error) {
		acquires++
		if acquires == 1 {
			return nil, context.DeadlineExceeded
		}
		return &browserSession{}, nil
	}
	_, err := pageFetchWithSessionRunner(ctx, &PageFetchRequest{WarmURL: "https://www.douyin.com", FetchURL: "https://www.douyin.com/api", Method: "POST"}, func() *browserManager { return m }, func(context.Context, *browserSession) (*PageFetchResult, error) {
		sends++
		return &PageFetchResult{Status: 200}, nil
	})
	if err != nil || acquires != 2 || sends != 1 || BrowseDiagnostics(ctx)["retry_count"] != 1 {
		t.Fatalf("acquires=%d sends=%d error=%v", acquires, sends, err)
	}
}
