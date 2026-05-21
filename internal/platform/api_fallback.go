package platform

import "fmt"

// NewAPIFallbackError 构造 API fallback 错误。
func NewAPIFallbackError(message string) error {
	return &APIFallbackError{
		Attempt: &APIAttempt{
			FallbackBrowser: true,
			Message:         message,
		},
	}
}

// NewAPIFallbackErrorf 格式化构造 API fallback 错误。
func NewAPIFallbackErrorf(format string, args ...any) error {
	return NewAPIFallbackError(fmt.Sprintf(format, args...))
}
