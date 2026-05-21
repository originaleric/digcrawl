package platform

import (
	"fmt"
	"strings"
)

// MetaErrorCode 从 metadata 读取 error_code。
func MetaErrorCode(meta map[string]any) string {
	return MetaString(meta, "error_code")
}

// MetaWarnings 从 metadata 读取 warnings 列表。
func MetaWarnings(meta map[string]any) []string {
	return MetaStringSlice(meta, "warnings")
}

// EnsureMeta 保证 map 非 nil。
func EnsureMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return map[string]any{}
	}
	return meta
}

// SetMetaError 向 metadata 写入 error_code 与 message。
func SetMetaError(meta map[string]any, code, message string) map[string]any {
	meta = EnsureMeta(meta)
	meta["error_code"] = code
	if message != "" {
		meta["error_message"] = message
	}
	return meta
}

// AppendWarning 追加 warning 字符串。
func AppendWarning(meta map[string]any, warning string) map[string]any {
	meta = EnsureMeta(meta)
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return meta
	}
	var warnings []string
	if existing, ok := meta["warnings"].([]string); ok {
		warnings = existing
	} else if existing, ok := meta["warnings"].([]any); ok {
		for _, item := range existing {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				warnings = append(warnings, s)
			}
		}
	}
	warnings = append(warnings, warning)
	meta["warnings"] = warnings
	return meta
}
