package platform

import "fmt"

// ReadError 平台读取结构化错误，供 gateway 写入 platform_metadata。
type ReadError struct {
	Code     string
	Message  string
	Platform string
	Meta     map[string]any
}

func (e *ReadError) Error() string {
	if e == nil {
		return ""
	}
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	return fmt.Sprintf("platform read %s: %s", e.Code, msg)
}

func (e *ReadError) ToolErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func (e *ReadError) ToolErrorMessage() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// NewReadError 构造平台读取错误。
func NewReadError(code, message, platform string, meta map[string]any) *ReadError {
	return &ReadError{
		Code:     code,
		Message:  message,
		Platform: platform,
		Meta:     meta,
	}
}

// ErrorOutput 将 ReadError 转为 gateway 输出 map（含 platform_metadata.error_code）。
func ErrorOutput(e *ReadError) map[string]any {
	if e == nil {
		return nil
	}
	meta := map[string]any{
		"error_code":    e.Code,
		"error_message": e.Message,
	}
	for k, v := range e.Meta {
		meta[k] = v
	}
	out := map[string]any{
		"platform_metadata": meta,
	}
	if e.Platform != "" {
		out["platform"] = e.Platform
	}
	return out
}
