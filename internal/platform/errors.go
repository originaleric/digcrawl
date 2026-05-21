package platform

// 平台读取结构化错误码（写入 platform_metadata.error_code）。
const (
	ErrLoginRequired    = "LOGIN_REQUIRED"
	ErrCaptchaRequired  = "CAPTCHA_REQUIRED"
	ErrContentNotFound  = "CONTENT_NOT_FOUND"
	ErrContentPrivate   = "CONTENT_PRIVATE"
	ErrContentDeleted   = "CONTENT_DELETED"
	ErrRateLimited      = "RATE_LIMITED"
	ErrRegionRestricted = "REGION_RESTRICTED"
	ErrSelectorStale    = "SELECTOR_STALE"
	ErrPartialContent   = "PARTIAL_CONTENT"
)

// fatalErrorCodes 表示应中止读取并向上返回诊断结果的错误码。
var fatalErrorCodes = map[string]bool{
	ErrLoginRequired:    true,
	ErrCaptchaRequired:  true,
	ErrContentNotFound:  true,
	ErrContentPrivate:   true,
	ErrContentDeleted:   true,
	ErrRateLimited:      true,
	ErrRegionRestricted: true,
}

// IsFatalErrorCode 判断 metadata 中的 error_code 是否应作为读取失败返回。
func IsFatalErrorCode(code string) bool {
	return fatalErrorCodes[code]
}
