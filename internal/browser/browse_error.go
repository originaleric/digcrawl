package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"

	"github.com/go-rod/rod"
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
	Code        string
	Stage       string
	Message     string
	Cause       error
	Retryable   bool
	Diagnostics map[string]any
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
	for k, v := range e.Diagnostics {
		details[k] = v
	}
	return details
}

func browseCauseReason(err error) string {
	if err == nil {
		return ""
	}
	var browse *BrowseError
	if errors.As(err, &browse) {
		return browseCauseReason(browse.Cause)
	}
	// Script exception text is page data, not transport evidence. It may quote
	// words such as "EOF"/"timeout" without any browser connection failure.
	var eval *rod.EvalError
	if errors.As(err, &eval) {
		return "unclassified"
	}
	if strings.Contains(err.Error(), "external target cleanup required") {
		return "external_target_cleanup_required"
	}
	var send *cdpSendError
	if errors.As(err, &send) {
		return send.operation
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return "browser_connection_closed"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, ErrProfileInUse) || errors.Is(err, errInteractiveLoginInProgress) {
		return "profile_in_use"
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "external target cleanup required"):
		return "external_target_cleanup_required"
	case isBrowserTimeoutError(lower):
		return "timeout"
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "cannot connect"), strings.Contains(lower, "failed to connect"), strings.Contains(lower, "dial tcp"):
		return "cdp_endpoint_unavailable"
	case strings.Contains(lower, "target closed"), strings.Contains(lower, "page closed"), strings.Contains(lower, "detached"):
		return "page_closed"
	case strings.Contains(lower, "connection reset"), strings.Contains(lower, "broken pipe"), strings.Contains(lower, "use of closed network connection"), strings.Contains(lower, "connection closed"), strings.Contains(lower, "session closed"), strings.Contains(lower, "websocket: close"), strings.Contains(lower, "websocket closed"), strings.Contains(lower, "eof"):
		return "browser_connection_closed"
	default:
		return "unclassified"
	}
}

func browseCauseDescription(err error) string {
	switch browseCauseReason(err) {
	case "external_target_cleanup_required":
		return "外部 CDP 页面清理未确认；请清理或重启外部浏览器，再重启 DigCrawl 后读取"
	case "send_queue":
		return "等待浏览器发送权超时，请等待当前任务结束后再读取"
	case "send_write":
		return "浏览器传输写入失败"
	case "cancelled":
		return "请求已取消"
	case "profile_in_use":
		return "专用浏览器数据目录被占用，请结束对应会话后再读取"
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
	var eval *rod.EvalError
	isScriptError := errors.As(cause, &eval)
	if !isScriptError && isBrowserTimeoutError(lower) {
		code = ErrorBrowserTimeout
		retryable = true
	} else if isPageClosedCause(cause) {
		code = ErrorBrowserPageClosed
		retryable = true
	}
	if errors.Is(cause, context.Canceled) || isLocalBrowserFailure(nil, cause) {
		retryable = false
	}
	operation := stage
	for _, op := range []string{"health_probe", "create_page", "wait_opening", "wait_retirement"} {
		if strings.HasPrefix(cause.Error(), op+":") {
			operation = op
		}
	}
	if strings.Contains(cause.Error(), "启动 Chromium") {
		operation = "launch"
	}
	if strings.Contains(cause.Error(), "连接 Chromium CDP") {
		operation = "connect_cdp"
	}
	if strings.Contains(cause.Error(), "连接已有 CDP 浏览器") {
		operation = "discover_cdp"
	}
	if strings.Contains(cause.Error(), "browser manager stopped") {
		operation = "shutdown"
	}
	if strings.Contains(cause.Error(), "teardown failed") {
		operation = "retirement_check"
	}
	if strings.Contains(cause.Error(), "等待浏览器并发槽位") {
		operation = "wait_slot"
	}
	if errors.Is(cause, ErrProfileInUse) {
		operation = "profile_guard"
	}
	if errors.Is(cause, errInteractiveLoginInProgress) {
		operation = "profile_release"
	}
	e := &BrowseError{Code: code, Stage: stage, Message: message, Cause: cause, Retryable: retryable, Diagnostics: map[string]any{"operation": operation}}
	var send *cdpSendError
	if errors.As(cause, &send) {
		e.Diagnostics = map[string]any{"operation": send.operation}
	}
	if browseCauseReason(cause) == "external_target_cleanup_required" {
		e.Retryable = false
		e.Diagnostics["next_actions"] = []map[string]any{{"type": "cleanup_external_browser", "executor": "user_or_agent", "precondition": "清理或重启外部 CDP 浏览器，再重启 DigCrawl 后读取", "description": "清理或重启外部 CDP 浏览器，再重启 DigCrawl 后读取"}}
	}
	return e
}

func isRetryableBrowserCause(err error) bool {
	if err == nil {
		return false
	}
	var eval *rod.EvalError
	if errors.As(err, &eval) {
		return false
	}
	if isLocalBrowserFailure(nil, err) || isConnectionFailure(err) || errors.Is(err, context.Canceled) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return isBrowserTimeoutError(lower) || isPageClosedCause(err)
}

func isBrowserTimeoutError(message string) bool {
	return strings.Contains(message, "context deadline") || strings.Contains(message, "timeout")
}

func isPageClosedCause(err error) bool {
	var eval *rod.EvalError
	if errors.As(err, &eval) {
		return false
	}
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
