package audit

import (
	"log"
	"strings"

	"github.com/originaleric/digcrawl/protocol"
)

// Logger writes redacted audit lines for gateway tool calls.
type Logger struct {
	logger *log.Logger
}

func NewLogger() *Logger {
	return &Logger{logger: log.Default()}
}

// LogCall records a tool invocation without sensitive payloads.
func (l *Logger) LogCall(call *protocol.ToolCall, result *protocol.ToolResult) {
	if call == nil {
		return
	}
	status := "unknown"
	errCode := ""
	if result != nil {
		status = result.Status
		if result.Error != nil {
			errCode = result.Error.Code
		}
	}
	l.logger.Printf(
		"[gateway-audit] tool=%s call_id=%s trace_id=%s host=%s status=%s err_code=%s duration_ms=%d",
		sanitize(call.Tool),
		sanitize(call.ID),
		sanitize(call.Context.TraceID),
		sanitize(call.Context.Host),
		status,
		errCode,
		resultUsageMs(result),
	)
}

func resultUsageMs(r *protocol.ToolResult) int64 {
	if r == nil {
		return 0
	}
	return r.Usage.DurationMs
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	if len(s) > 128 {
		return s[:128] + "..."
	}
	return s
}
