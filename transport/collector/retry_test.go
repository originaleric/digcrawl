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
	if !shouldRetryToolResult(fail) {
		t.Fatal("expected retry for timeout")
	}
	rate := &protocol.ToolResult{
		Status: "error",
		Error:  &protocol.ToolError{Code: "RATE_LIMITED", Message: "busy"},
	}
	if shouldRetryToolResult(rate) {
		t.Fatal("rate limited should not retry")
	}
}
