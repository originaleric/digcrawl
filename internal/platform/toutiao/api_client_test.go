package toutiao

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchArticleViaAPIClassifiesNonJSONAsFallback(t *testing.T) {
	oldFetch := articleFetchJSON
	articleFetchJSON = func(ctx context.Context, req *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
		return &browser.PageFetchResult{
			Status:      200,
			ContentType: "text/html; charset=utf-8",
			Body:        "<html><body>main app shell</body></html>",
		}, nil
	}
	defer func() { articleFetchJSON = oldFetch }()

	norm := &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/group/123/",
		ResolvedURL:  "https://www.toutiao.com/group/123/",
		PlatformID:   "123",
		CanonicalURL: "https://www.toutiao.com/group/123/",
		CookieDomain: CookieDomain,
	}
	parsed, attempt, _, err := FetchArticleViaAPI(context.Background(), norm, CookieDomain)
	if parsed != nil {
		t.Fatalf("expected no parsed payload, got %#v", parsed)
	}
	var fallback *platform.APIFallbackError
	if !errors.As(err, &fallback) {
		t.Fatalf("expected API fallback error, got %v", err)
	}
	if attempt == nil || !attempt.FallbackBrowser {
		t.Fatalf("expected fallback attempt, got %#v", attempt)
	}
	if got := platform.MetaString(attempt.Meta, "api_response_type"); got != "html" {
		t.Fatalf("expected html response type, got %q meta=%#v", got, attempt.Meta)
	}
	if got := platform.MetaString(attempt.Meta, "api_body_prefix"); got == "" {
		t.Fatalf("expected body prefix in metadata: %#v", attempt.Meta)
	}
}

func TestFetchArticleStopsAfterBrowserInfrastructureFailure(t *testing.T) {
	oldFetch := articleFetchJSON
	defer func() { articleFetchJSON = oldFetch }()
	failure := &browser.BrowseError{Code: browser.ErrorBrowserTimeout, Stage: "acquire", Cause: context.DeadlineExceeded, Diagnostics: map[string]any{"operation": "send_queue", "next_actions": []string{"wait"}}}
	calls := 0
	articleFetchJSON = func(context.Context, *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
		calls++
		return nil, failure
	}
	_, attempt, _, err := FetchArticleViaAPI(context.Background(), &platform.NormalizedURL{PlatformID: "123456", ResolvedURL: "https://www.toutiao.com/article/123456/"}, "")
	if calls != 1 || err != failure || attempt == nil || !attempt.Fatal {
		t.Fatalf("calls=%d attempt=%+v err=%v", calls, attempt, err)
	}
}

func TestArticleAPIStopsAfterFatalResponse(t *testing.T) {
	oldFetch := articleFetchJSON
	defer func() { articleFetchJSON = oldFetch }()
	calls := 0
	articleFetchJSON = func(context.Context, *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
		calls++
		return &browser.PageFetchResult{Status: 403, ParsedJSON: map[string]any{"message": "login required"}}, nil
	}
	_, attempt, _, err := FetchArticleViaAPI(context.Background(), &platform.NormalizedURL{PlatformID: "123456", ResolvedURL: "https://www.toutiao.com/article/123456/"}, "")
	var re *platform.ReadError
	if calls != 1 || attempt == nil || !attempt.Fatal || !errors.As(err, &re) {
		t.Fatalf("fatal API response continued calls=%d attempt=%+v error=%v", calls, attempt, err)
	}
}

func TestArticleAPIWarmupAndEndpointsShareCallerBudget(t *testing.T) {
	oldFetch := articleFetchJSON
	defer func() { articleFetchJSON = oldFetch }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	calls := 0
	var diagnosticID any
	articleFetchJSON = func(fetchCtx context.Context, _ *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
		calls++
		if got, _ := fetchCtx.Deadline(); !got.Equal(deadline) {
			t.Fatalf("API shortened caller deadline: got=%v want=%v", got, deadline)
		}
		d := browser.BrowseDiagnostics(fetchCtx)
		if d == nil {
			t.Fatal("API endpoints have no shared invocation mark")
		}
		if calls == 1 {
			diagnosticID = d["diagnostic_id"]
			// A healthy cold browser/warm page can legitimately need over four
			// seconds. The first endpoint's HTML response permits the next one.
			timer := time.NewTimer(4100 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-fetchCtx.Done():
				return nil, fetchCtx.Err()
			}
			return &browser.PageFetchResult{Status: 200, ContentType: "text/html", Body: "<html>app shell</html>"}, nil
		}
		if d["diagnostic_id"] != diagnosticID {
			t.Fatal("second API endpoint reset the invocation mark")
		}
		return &browser.PageFetchResult{Status: 200, ParsedJSON: map[string]any{"title": "valid article"}}, nil
	}
	parsed, attempt, _, err := FetchArticleViaAPI(ctx, &platform.NormalizedURL{PlatformID: "123456"}, "")
	if err != nil || parsed == nil || attempt == nil || !attempt.OK || calls != 2 {
		t.Fatalf("cold read failed: calls=%d parsed=%v attempt=%+v err=%v", calls, parsed, attempt, err)
	}
}

func TestArticleAPICancellationStopsEndpointChain(t *testing.T) {
	oldFetch := articleFetchJSON
	defer func() { articleFetchJSON = oldFetch }()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	articleFetchJSON = func(fetchCtx context.Context, _ *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
		calls++
		cancel()
		return nil, fetchCtx.Err()
	}
	defer cancel()
	_, attempt, _, err := FetchArticleViaAPI(ctx, &platform.NormalizedURL{PlatformID: "123456"}, "")
	if !errors.Is(err, context.Canceled) || attempt == nil || !attempt.Fatal || calls != 1 {
		t.Fatalf("cancellation did not stop chain: calls=%d attempt=%+v err=%v", calls, attempt, err)
	}
}

func TestArticleAPIFatalStatusIndependentOfResponseBody(t *testing.T) {
	oldFetch := articleFetchJSON
	defer func() { articleFetchJSON = oldFetch }()
	for _, status := range []int{401, 403, 429, 461, 471} {
		for _, kind := range []string{"html", "non_json", "empty", "json"} {
			t.Run(fmt.Sprintf("%d/%s", status, kind), func(t *testing.T) {
				result := &browser.PageFetchResult{Status: status}
				switch kind {
				case "html":
					result.ContentType, result.Body = "text/html", "<html>access denied</html>"
				case "non_json":
					result.ContentType, result.Body = "text/plain", "access denied"
				case "json":
					result.ParsedJSON = map[string]any{"message": "access denied"}
				}
				calls := 0
				articleFetchJSON = func(context.Context, *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
					calls++
					return result, nil
				}
				_, attempt, _, err := FetchArticleViaAPI(context.Background(), &platform.NormalizedURL{PlatformID: "123456"}, "")
				var re *platform.ReadError
				wantCode := platform.ErrLoginRequired
				if status == 429 {
					wantCode = platform.ErrRateLimited
				} else if status == 461 || status == 471 {
					wantCode = platform.ErrCaptchaRequired
				}
				if calls != 1 || attempt == nil || !attempt.Fatal || !errors.As(err, &re) || re.Code != wantCode {
					t.Fatalf("fatal status continued: calls=%d attempt=%+v err=%v", calls, attempt, err)
				}
				if got := platform.MetaString(attempt.Meta, "api_response_type"); got != kind {
					t.Fatalf("response type=%s want=%s", got, kind)
				}
			})
		}
	}
}
