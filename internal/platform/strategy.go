package platform

import (
	"errors"
	"fmt"
	"strings"
)

// APIAttempt 记录 API 通道尝试结果，供 strategy chain 决策。
type APIAttempt struct {
	OK              bool
	FallbackBrowser bool
	Fatal           bool
	ErrorCode       string
	Message         string
	HTTPStatus      int
	Meta            map[string]any
}

func (a *APIAttempt) Error() error {
	if a == nil || a.OK {
		return nil
	}
	if a.Fatal {
		return NewReadError(a.ErrorCode, a.Message, MetaString(a.Meta, "platform"), a.Meta)
	}
	if a.FallbackBrowser {
		return &APIFallbackError{Attempt: a}
	}
	return fmt.Errorf("api attempt failed: %s", a.Message)
}

// APIFallbackError 表示应回落 Browser 通道的 API 失败。
type APIFallbackError struct {
	Attempt *APIAttempt
}

func (e *APIFallbackError) Error() string {
	if e == nil || e.Attempt == nil {
		return "api fallback"
	}
	return e.Attempt.Message
}

// ClassifyAPIResponse 根据 HTTP 状态与 JSON 判断 API 结果。
func ClassifyAPIResponse(platformName string, status int, parsed map[string]any, emptyCheck func(map[string]any) bool) *APIAttempt {
	meta := map[string]any{
		"platform":        platformName,
		"api_http_status": status,
	}
	if status == 401 || status == 403 {
		return &APIAttempt{
			Fatal:      true,
			ErrorCode:  ErrLoginRequired,
			Message:    "API 需要登录或授权",
			HTTPStatus: status,
			Meta:       meta,
		}
	}
	if status == 461 || status == 471 {
		meta["api_error_code"] = status
		return &APIAttempt{
			Fatal:      true,
			ErrorCode:  ErrCaptchaRequired,
			Message:    "API 触发安全验证",
			HTTPStatus: status,
			Meta:       meta,
		}
	}
	if status == 429 {
		meta["api_error_code"] = status
		return &APIAttempt{
			Fatal:      true,
			ErrorCode:  ErrRateLimited,
			Message:    "API 限流",
			HTTPStatus: status,
			Meta:       meta,
		}
	}
	if status >= 500 {
		meta["api_error_code"] = status
		return &APIAttempt{
			FallbackBrowser: true,
			Message:         fmt.Sprintf("API 临时错误 HTTP %d", status),
			HTTPStatus:      status,
			Meta:            meta,
		}
	}
	if status < 200 || status >= 300 {
		meta["api_error_code"] = status
		return &APIAttempt{
			FallbackBrowser: true,
			Message:         fmt.Sprintf("API HTTP %d", status),
			HTTPStatus:      status,
			Meta:            meta,
		}
	}
	if parsed == nil || (emptyCheck != nil && emptyCheck(parsed)) {
		meta["api_error_code"] = "empty_payload"
		return &APIAttempt{
			FallbackBrowser: true,
			Message:         "API 返回空结构",
			HTTPStatus:      status,
			Meta:            meta,
		}
	}
	if code := apiBusinessErrorCode(parsed); code != "" {
		if IsFatalErrorCode(code) {
			return &APIAttempt{
				Fatal:      true,
				ErrorCode:  code,
				Message:    apiBusinessMessage(parsed, code),
				HTTPStatus: status,
				Meta:       meta,
			}
		}
		meta["api_error_code"] = code
		return &APIAttempt{
			FallbackBrowser: true,
			ErrorCode:       code,
			Message:         apiBusinessMessage(parsed, code),
			HTTPStatus:      status,
			Meta:            meta,
		}
	}
	return &APIAttempt{OK: true, HTTPStatus: status, Meta: meta}
}

func apiBusinessErrorCode(parsed map[string]any) string {
	for _, key := range []string{"code", "status_code", "statusCode", "error_code"} {
		if v := MetaString(parsed, key); v != "" && v != "0" && v != "success" {
			return v
		}
	}
	if success, ok := parsed["success"].(bool); ok && !success {
		return "api_success_false"
	}
	return ""
}

func apiBusinessMessage(parsed map[string]any, code string) string {
	for _, key := range []string{"msg", "message", "error_msg", "error_message"} {
		if m := MetaString(parsed, key); m != "" {
			return m
		}
	}
	return "API business error: " + code
}

// ShouldStopStrategy 判断是否应终止 strategy chain（fatal，不 fallback）。
func ShouldStopStrategy(err error) (*ReadError, bool) {
	var re *ReadError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}

// IsAPIFallback 判断错误是否应继续 Browser fallback。
func IsAPIFallback(err error) (*APIAttempt, bool) {
	var fe *APIFallbackError
	if errors.As(err, &fe) {
		return fe.Attempt, true
	}
	return nil, false
}

// MergeAPIMeta 将 API 尝试信息写入 platform_metadata。
func MergeAPIMeta(meta map[string]any, attempt *APIAttempt) map[string]any {
	meta = EnsureMeta(meta)
	if attempt == nil {
		return meta
	}
	for k, v := range attempt.Meta {
		if k == "" || v == nil {
			continue
		}
		if _, exists := meta[k]; !exists {
			meta[k] = v
		}
	}
	if attempt.HTTPStatus > 0 {
		meta["api_http_status"] = attempt.HTTPStatus
	}
	if attempt.ErrorCode != "" {
		meta["api_error_code"] = attempt.ErrorCode
	}
	if attempt.Message != "" {
		msg := strings.TrimSpace(MetaString(meta, "api_error_message"))
		if msg == "" {
			meta["api_error_message"] = attempt.Message
		}
	}
	return meta
}
