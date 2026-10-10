package collector

import (
	"testing"

	"github.com/originaleric/digcrawl/protocol"
)

func TestShouldRetryToolResult(t *testing.T) {
	t.Parallel()
	ok := &protocol.ToolResult{Status: "success"}
	if shouldRetryToolResult(ok) {
		t.Fatal("success should not retry")
	}
	fail := &protocol.ToolResult{
		Status: "error",
		Error:  &protocol.ToolError{Code: "TOOL_EXEC_FAILED", Message: "navigation timeout"},
	}
	if shouldRetryToolResult(fail) {
		t.Fatal("unknown tool timeout text must not authorize replay")
	}
	rate := &protocol.ToolResult{
		Status: "error",
		Error:  &protocol.ToolError{Code: "RATE_LIMITED", Message: "busy"},
	}
	if shouldRetryToolResult(rate) {
		t.Fatal("rate limited should not retry")
	}
}

func TestShouldRetryToolResultUsesStructuredRetryableDetail(t *testing.T) {
	t.Parallel()
	result := &protocol.ToolResult{
		Status: "error",
		Error: &protocol.ToolError{
			Code:    "BROWSER_CONTENT_NOT_FOUND",
			Message: "提取页面正文失败",
			Details: map[string]any{"retryable": true},
		},
	}
	if !shouldRetryToolResult(result) {
		t.Fatal("structured retryable browser error should retry")
	}
}

func TestShouldRetryToolResultDoesNotRetryPermanentContentFailure(t *testing.T) {
	t.Parallel()
	result := &protocol.ToolResult{
		Status: "error",
		Error: &protocol.ToolError{
			Code:    "BROWSER_CONTENT_NOT_FOUND",
			Message: "提取页面正文失败",
			Details: map[string]any{"retryable": false},
		},
	}
	if shouldRetryToolResult(result) {
		t.Fatal("permanent content failure should not retry")
	}
}
