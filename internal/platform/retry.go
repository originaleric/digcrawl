package platform

import (
	"context"
	"errors"
	"github.com/originaleric/digcrawl/internal/browser"
	"strings"
	"time"
)

// RetryPolicy 平台读取重试策略。
type RetryPolicy struct {
	MaxAttempts int
	Delay       time.Duration
}

// DefaultBrowseRetry 保留有限内容验证 fallback；浏览器故障由 BrowserBrowse 处理。
var DefaultBrowseRetry = RetryPolicy{MaxAttempts: 2, Delay: 800 * time.Millisecond}

// WithBrowseRetry 只对平台内容验证失败做有限 fallback，共享浏览器故障预算。
func WithBrowseRetry(ctx context.Context, policy RetryPolicy, fn func(context.Context) error) error {
	// Keep only the existing finite content-validation fallback. Transport
	// and page failures have already been handled by BrowserBrowse.
	ctx = browser.WithBrowseRetryContext(ctx)
	attempts := policy.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 2 {
		attempts = 2
	}
	for attempt := 0; attempt < attempts; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		var re *ReadError
		if !errors.As(err, &re) || (re.Code != ErrSelectorStale && re.Code != ErrPartialContent) || attempt == attempts-1 {
			return err
		}
		timer := time.NewTimer(policy.Delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func isRetryableBrowseError(err error) bool {
	if err == nil {
		return false
	}
	var retryable interface {
		ToolErrorRetryable() bool
	}
	if errors.As(err, &retryable) {
		return retryable.ToolErrorRetryable()
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
