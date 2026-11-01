package ocr

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Provider responses can reflect credentials or input URLs. Emit only our own
// bounded classifications, never the original message/code/request body.
type providerError struct {
	status    int
	code      string
	retryable bool
}

func (e *providerError) Error() string {
	return fmt.Sprintf("OCR provider HTTP %d: %s", e.status, e.code)
}
func classifyProviderError(status int, body []byte) error {
	e := &providerError{status: status, code: "provider_rejected"}
	switch {
	case status == 400 || status == 422:
		e.code = "invalid_input"
	case status == 401 || status == 403:
		e.code = "authentication_failed"
	case status == 429:
		e.code = "rate_limited"
		e.retryable = true
	case status >= 500:
		e.code = "provider_unavailable"
		e.retryable = true
	}
	var parsed struct {
		Error struct {
			Code    any    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && (status == 400 || status == 422) {
		msg := strings.ToLower(parsed.Error.Type + " " + fmt.Sprint(parsed.Error.Code) + " " + parsed.Error.Message)
		if strings.Contains(msg, "image") && (strings.Contains(msg, "download") || strings.Contains(msg, "fetch") || strings.Contains(msg, "url")) || strings.Contains(msg, "图片下载") {
			e.code = "image_unavailable"
		}
	}
	return e
}
func safeImageFailure(reason string) string {
	switch reason {
	case "access_denied", "not_loaded", "too_large", "unsupported_format", "fetch_failed", "budget_exhausted":
		return reason
	}
	return "fetch_failed"
}
