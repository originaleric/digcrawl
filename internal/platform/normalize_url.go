package platform

// NormalizedURL 平台 URL 归一化结果。
type NormalizedURL struct {
	OriginalURL  string
	ResolvedURL  string
	CanonicalURL string
	PlatformID   string
	ContentKind  string
	CookieDomain string
	RiskFlags    []string
}
