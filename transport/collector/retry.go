package collector

import (
	"strings"
	"time"

	"github.com/originaleric/digcrawl/protocol"
)

// shouldRetryToolResult 判断 Collector 是否应对工具失败做有限重试。
func shouldRetryToolResult(result *protocol.ToolResult) bool {
	if result == nil || result.Status == "success" {
		return false
	}
	if result.Error == nil {
		return false
	}
	switch result.Error.Code {
	case "TOOL_EXEC_FAILED":
		msg := strings.ToLower(result.Error.Message)
		for _, p := range []string{
			"timeout", "context deadline", "navigation", "等待选择器",
			"selector_stale", "temporary", "connection reset", "eof",
		} {
			if strings.Contains(msg, p) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func retryDelay(opts Options, attempt int) {
	if attempt <= 0 || opts.RetryDelay <= 0 {
		return
	}
	time.Sleep(opts.RetryDelay)
}
