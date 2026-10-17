package browser

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"
)

func TestMediaResourceProbeScriptUsesOnlyPlayableHTTPResources(t *testing.T) {
	t.Parallel()
	for _, required := range []string{
		"const primaryPlayer",
		"player.click()",
		"await video.play()",
		"getBoundingClientRect()",
		"A URL sourced directly from a <video> element",
	} {
		if !strings.Contains(mediaResourceProbeScript, required) {
			t.Fatalf("media probe missing %q", required)
		}
	}
	if strings.Contains(mediaResourceProbeScript, "blob:") {
		t.Fatal("media probe must not return browser-local blob URLs")
	}
}

func TestIsVideoMediaResponse(t *testing.T) {
	t.Parallel()
	for _, event := range []*proto.NetworkResponseReceived{
		{Type: proto.NetworkResourceTypeMedia, Response: &proto.NetworkResponse{URL: "https://cdn.example/video"}},
		{Response: &proto.NetworkResponse{URL: "https://cdn.example/master", MIMEType: "application/vnd.apple.mpegurl"}},
		{Response: &proto.NetworkResponse{URL: "https://cdn.example/video", MIMEType: "video/mp4"}},
	} {
		if !isVideoMediaResponse(event) {
			t.Fatalf("expected media response: %#v", event)
		}
	}
	if isVideoMediaResponse(&proto.NetworkResponseReceived{Response: &proto.NetworkResponse{URL: "https://cdn.example/image", MIMEType: "image/jpeg"}}) {
		t.Fatal("image response must not be treated as video media")
	}
}

func TestReadTailHelpersPreserveInfrastructureFailure(t *testing.T) {
	for _, failure := range []error{io.EOF, context.Canceled, &cdpSendError{operation: "send_queue", cause: context.DeadlineExceeded}, &cdpSendError{operation: "send_write", cause: context.DeadlineExceeded}} {
		t.Run(browseCauseReason(failure), func(t *testing.T) {
			client := &scriptedCDP{events: make(chan *cdp.Event), call: func(_ context.Context, method string, _ any) ([]byte, error) {
				if method == "Runtime.evaluate" || method == "Network.enable" {
					return nil, failure
				}
				return []byte(`{}`), nil
			}}
			defer close(client.events)
			b := rod.New().ControlURL("").NoDefaultDevice().Client(client)
			if err := b.Connect(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			page, err := b.Context(ctx).PageFromTarget("read-tail-target")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := evalPageMetadata(page, `() => ({})`); !errors.Is(err, failure) {
				t.Fatalf("metadata lost cause: %v", err)
			}
			if urls, err := captureMediaURLs(page); urls != nil || !errors.Is(err, failure) {
				t.Fatalf("media probe hid failure: urls=%v err=%v", urls, err)
			}
			if finish, err := startMediaResponseCapture(ctx, page); finish != nil || !errors.Is(err, failure) {
				t.Fatalf("media listener hid failure: finish=%v err=%v", finish != nil, err)
			}
		})
	}
}

func TestDOMHelpersStopOnInfrastructureFailure(t *testing.T) {
	for _, cause := range []error{io.EOF, context.Canceled, &cdpSendError{operation: "send_queue", cause: context.DeadlineExceeded}, &cdpSendError{operation: "send_write", cause: context.DeadlineExceeded}} {
		for _, helper := range []string{"main", "summary"} {
			t.Run(helper+"/"+browseCauseReason(cause), func(t *testing.T) {
				calls := 0
				client := &scriptedCDP{events: make(chan *cdp.Event), call: func(ctx context.Context, method string, _ any) ([]byte, error) {
					if method == "Runtime.evaluate" {
						calls++
						return nil, cause
					}
					return []byte(`{}`), nil
				}}
				defer close(client.events)
				b := rod.New().ControlURL("").NoDefaultDevice().Client(client)
				if err := b.Connect(); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				page, err := b.Context(ctx).PageFromTarget("dom-infrastructure")
				if err != nil {
					t.Fatal(err)
				}
				if helper == "main" {
					_, _, err = extractMainContent(page, 1)
				} else {
					_, err = extractSummary(page, 1)
				}
				if !errors.Is(err, cause) || calls != 1 {
					t.Fatalf("cause lost or continued after failure: err=%v calls=%d", err, calls)
				}
			})
		}
	}
}
func TestCookieCollectionUsesCurrentPageContext(t *testing.T) {
	seen := false
	client := &scriptedCDP{events: make(chan *cdp.Event), call: func(ctx context.Context, method string, _ any) ([]byte, error) {
		if method == "Storage.getCookies" {
			seen = true
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return []byte(`{"cookies":[]}`), nil
		}
		return []byte(`{}`), nil
	}}
	defer close(client.events)
	b := rod.New().ControlURL("").NoDefaultDevice().Client(client)
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	setup, cancelSetup := context.WithCancel(context.Background())
	page, err := b.Context(setup).PageFromTarget("login-cookie-context")
	if err != nil {
		t.Fatal(err)
	}
	cancelSetup()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	page = page.Context(ctx)
	if _, err = collectSessionCookies(page, "example.com", nil); err != nil || !seen {
		t.Fatalf("current login context not used: err=%v seen=%v", err, seen)
	}
}

func TestDiagnosticDoesNotMarkRejectedReadAsSuccess(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	recordBrowseDiagnostic(ctx, errors.New("DOMAIN_NOT_ALLOWED"))
	id := BrowseDiagnostics(ctx)["diagnostic_id"].(string)
	records, err := DiagnosticSummary(id)
	if err != nil || len(records) != 1 || records[0]["status"] != "error" {
		t.Fatalf("rejected read diagnostic: records=%v err=%v", records, err)
	}
}

func TestPageChangeGuidanceAndDiagnosticPreserveReason(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	e := newBrowseError(ErrorBrowserContentNotFound, "read_page", "页面已切换", nil, false).(*BrowseError)
	e.Diagnostics = map[string]any{"reason": "page_changed"}
	err := finishBrowseError(ctx, e)
	recordBrowseDiagnostic(ctx, err)
	id := BrowseDiagnostics(ctx)["diagnostic_id"].(string)
	records, readErr := DiagnosticSummary(id)
	if readErr != nil || len(records) != 1 || records[0]["status"] != "error" || records[0]["reason"] != "page_changed" {
		t.Fatalf("page change diagnostic: records=%v err=%v", records, readErr)
	}
	if canRetryBrowse(ctx, err) || CanBrowseFallback(err) {
		t.Fatal("page change unexpectedly allowed automatic replay or selector fallback")
	}
	actions := e.Diagnostics["next_actions"].([]map[string]any)
	if actions[0]["type"] != "retry_read" {
		t.Fatalf("actions=%v", actions)
	}
}
