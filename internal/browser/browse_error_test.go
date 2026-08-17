package browser

import (
	"errors"
	"strings"
	"testing"
)

func TestNewBrowseErrorReportsPageClosedWithDiagnostics(t *testing.T) {
	t.Parallel()
	err := newBrowseError(ErrorBrowserNavigationFailed, "navigate", "页面导航失败", errors.New("target closed"), true)
	var browseErr *BrowseError
	if !errors.As(err, &browseErr) {
		t.Fatalf("expected BrowseError, got %T", err)
	}
	if browseErr.Code != ErrorBrowserPageClosed || !browseErr.ToolErrorRetryable() {
		t.Fatalf("unexpected browser error: %+v", browseErr)
	}
	details := browseErr.ToolErrorDetails()
	if details["component"] != "local_browser" || details["stage"] != "navigate" || details["reason"] != "page_closed" {
		t.Fatalf("unexpected diagnostics: %#v", details)
	}
}

func TestNewBrowseErrorReportsTimeout(t *testing.T) {
	t.Parallel()
	err := newBrowseError(ErrorBrowserContentNotFound, "extract_content", "提取页面正文失败", errors.New("context deadline exceeded"), false)
	var browseErr *BrowseError
	if !errors.As(err, &browseErr) {
		t.Fatalf("expected BrowseError, got %T", err)
	}
	if browseErr.Code != ErrorBrowserTimeout || !browseErr.ToolErrorRetryable() {
		t.Fatalf("unexpected browser error: %+v", browseErr)
	}
}

func TestBrowseErrorClassifiesUnavailableCDPEndpoint(t *testing.T) {
	t.Parallel()
	err := newBrowseError(ErrorBrowserUnavailable, "acquire", "无法启动或连接本地浏览器", errors.New("dial tcp 127.0.0.1:9323: connect: connection refused"), false)
	var browseErr *BrowseError
	if !errors.As(err, &browseErr) {
		t.Fatalf("expected BrowseError, got %T", err)
	}
	if got := browseErr.ToolErrorDetails()["reason"]; got != "cdp_endpoint_unavailable" {
		t.Fatalf("unexpected safe reason: %#v", got)
	}
	if got := browseErr.ToolErrorMessage(); !strings.Contains(got, "本地 CDP 浏览器不可用") {
		t.Fatalf("unexpected client message: %q", got)
	}
}

func TestNewBrowseErrorKeepsPermanentContentFailureNonRetryable(t *testing.T) {
	t.Parallel()
	err := newBrowseError(ErrorBrowserContentNotFound, "extract_content", "提取页面正文失败", errors.New("empty page body"), false)
	var browseErr *BrowseError
	if !errors.As(err, &browseErr) {
		t.Fatalf("expected BrowseError, got %T", err)
	}
	if browseErr.Code != ErrorBrowserContentNotFound || browseErr.ToolErrorRetryable() {
		t.Fatalf("unexpected browser error: %+v", browseErr)
	}
}

func TestBrowseErrorDoesNotExposeRawCauseToClients(t *testing.T) {
	err := newBrowseError(ErrorBrowserNavigationFailed, "navigate", "页面导航失败", errors.New("request https://video.example/path?x-signature=secret&token=also-secret failed; Authorization: Bearer header-secret; Cookie: session=another-secret"), true)
	var browseErr *BrowseError
	if !errors.As(err, &browseErr) {
		t.Fatalf("expected BrowseError, got %T", err)
	}
	for _, secret := range []string{"secret", "?", "Authorization", "Cookie"} {
		if got := browseErr.Error(); strings.Contains(got, secret) {
			t.Fatalf("client error leaked raw cause: %q", got)
		}
	}
	details := browseErr.ToolErrorDetails()
	if _, ok := details["cause"]; ok {
		t.Fatalf("raw cause must not be exported: %#v", details)
	}
	if details["reason"] != "unclassified" {
		t.Fatalf("unexpected safe reason: %#v", details)
	}
}

func TestIsPageClosedCauseDoesNotTreatSelectorTimeoutAsClosed(t *testing.T) {
	t.Parallel()
	if !isPageClosedCause(errors.New("target closed")) {
		t.Fatal("target closed should be retained as a terminal browser failure")
	}
	if isPageClosedCause(errors.New("context deadline exceeded")) {
		t.Fatal("a candidate selector timeout must not be reported as page closed")
	}
}
