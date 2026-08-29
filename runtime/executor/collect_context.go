package executor

import "context"

// collectExecutionContextKey is private so an external ToolCall cannot select
// Collect-only behavior by choosing a particular call ID or payload value.
type collectExecutionContextKey struct{}

// WithCollectExecution marks a context created by the in-process Collect
// runner. It is intentionally transport-independent and must only be called
// by trusted local orchestration code.
func WithCollectExecution(ctx context.Context) context.Context {
	return context.WithValue(ctx, collectExecutionContextKey{}, true)
}

// IsCollectExecution reports whether the handler is running inside the
// trusted Collect workflow rather than an externally supplied tool call.
func IsCollectExecution(ctx context.Context) bool {
	marked, _ := ctx.Value(collectExecutionContextKey{}).(bool)
	return marked
}
