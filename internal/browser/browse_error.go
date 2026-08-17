package browser

import (
	"fmt"
	"strings"
)

const (
	ErrorBrowserUnavailable       = "BROWSER_UNAVAILABLE"
	ErrorBrowserNavigationFailed  = "BROWSER_NAVIGATION_FAILED"
	ErrorBrowserPageLoadFailed    = "BROWSER_PAGE_LOAD_FAILED"
	ErrorBrowserSelectorTimeout   = "BROWSER_SELECTOR_TIMEOUT"
	ErrorBrowserPageClosed        = "BROWSER_PAGE_CLOSED"
	ErrorBrowserContentNotFound   = "BROWSER_CONTENT_NOT_FOUND"
	ErrorBrowserContentConversion = "BROWSER_CONTENT_CONVERSION_FAILED"
	ErrorBrowserCookieFailed      = "BROWSER_COOKIE_FAILED"
	ErrorBrowserScreenshotFailed  = "BROWSER_SCREENSHOT_FAILED"
	ErrorBrowserTimeout           = "BROWSER_TIMEOUT"
)

// BrowseError identifies the browser stage that failed. It is deliberately
// transport-neutral: Runtime exposes its code and details through the normal
// ToolError envelope for MCP, HTTP, and Collector clients.
type BrowseError struct {
	Code      string
	Stage     string
	Message   string
	Cause     error
	Retryable bool
}

func (e *BrowseError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = e.Code
	}
	if e.Cause == nil {
		return message
	}
	return fmt.Sprintf("%s: %s", message, browseCauseDescription(e.Cause))
}

func (e *BrowseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *BrowseError) ToolErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func (e *BrowseError) ToolErrorMessage() string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// ToolErrorDetails is detected by Runtime without importing this internal
// package, keeping the protocol extensible for other providers as well.
func (e *BrowseError) ToolErrorDetails() map[string]any {
	if e == nil {
		return nil
	}
	details := map[string]any{
		"component": "local_browser",
		"stage":     e.Stage,
		"retryable": e.Retryable,
	}
	if e.Cause != nil {
		// Never send the raw lower-level error across the tool boundary. CDP,
		// HTTP, and browser-launch errors can contain cookies, authorization
		// headers, signed URLs, or filesystem paths. A stable classification is
		// sufficient for a caller's retry decision and safe to expose.
		details["reason"] = browseCauseReason(e.Cause)
	}
	return details
}

func browseCauseReason(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	switch {
	case isBrowserTimeoutError(lower):
		return "timeout"
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "cannot connect"), strings.Contains(lower, "failed to connect"), strings.Contains(lower, "dial tcp"):
		return "cdp_endpoint_unavailable"
	case strings.Contains(lower, "target closed"), strings.Contains(lower, "page closed"), strings.Contains(lower, "detached"):
		return "page_closed"
	case strings.Contains(lower, "connection closed"), strings.Contains(lower, "session closed"), strings.Contains(lower, "websocket: close"), strings.Contains(lower, "websocket closed"), strings.Contains(lower, "eof"):
		return "browser_connection_closed"
	default:
		return "unclassified"
	}
}

func browseCauseDescription(err error) string {
	switch browseCauseReason(err) {
	case "timeout":
		return "浏览器操作超时"
	case "cdp_endpoint_unavailable":
		return "本地 CDP 浏览器不可用"
	case "page_closed":
		return "页面已关闭或被替换"
	case "browser_connection_closed":
		return "浏览器连接已关闭"
	default:
		return "浏览器返回未分类错误"
	}
}

// ToolErrorRetryable lets platform retry policy use the structured signal
// instead of parsing a human-readable error message.
func (e *BrowseError) ToolErrorRetryable() bool {
	return e != nil && e.Retryable
}

func newBrowseError(code, stage, message string, cause error, retryable bool) error {
	if cause == nil {
		return &BrowseError{Code: code, Stage: stage, Message: message, Retryable: retryable}
	}
	lower := strings.ToLower(cause.Error())
	if isBrowserTimeoutError(lower) {
		code = ErrorBrowserTimeout
		retryable = true
	} else if isPageClosedCause(cause) {
		code = ErrorBrowserPageClosed
		retryable = true
	}
	return &BrowseError{Code: code, Stage: stage, Message: message, Cause: cause, Retryable: retryable}
}

func isRetryableBrowserCause(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return isBrowserTimeoutError(lower) || isPageClosedCause(err)
}

func isBrowserTimeoutError(message string) bool {
	return strings.Contains(message, "context deadline") || strings.Contains(message, "timeout")
}

func isPageClosedCause(err error) bool {
	return err != nil && isPageClosedError(strings.ToLower(err.Error()))
}

func isPageClosedError(message string) bool {
	for _, marker := range []string{
		"target closed", "page closed", "session closed", "connection closed",
		"websocket: close", "websocket closed", "detached", "eof",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
