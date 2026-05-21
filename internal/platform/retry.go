package platform

import (
	"context"
	"errors"
	"strings"
	"time"
)

// RetryPolicy 平台读取重试策略。
type RetryPolicy struct {
	MaxAttempts int
	Delay       time.Duration
}

// DefaultBrowseRetry 浏览器兜底默认重试（仅 transient 错误）。
var DefaultBrowseRetry = RetryPolicy{MaxAttempts: 2, Delay: 800 * time.Millisecond}

// WithBrowseRetry 对 browser 读取函数做有限重试。
func WithBrowseRetry(ctx context.Context, policy RetryPolicy, fn func(context.Context) error) error {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(policy.Delay):
			}
		}
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		var re *ReadError
		if errors.As(lastErr, &re) && IsFatalErrorCode(re.Code) {
			return lastErr
		}
		if !isRetryableBrowseError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func isRetryableBrowseError(err error) bool {
	if err == nil {
		return false
	}
	var re *ReadError
	if errors.As(err, &re) {
		switch re.Code {
		case ErrSelectorStale, ErrPartialContent:
			return true
		default:
			return false
		}
	}
	msg := strings.ToLower(err.Error())
	for _, p := range []string{"timeout", "context deadline", "navigation", "等待选择器"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
