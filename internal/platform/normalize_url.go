package platform

import (
	"net/url"
	"strings"
)

// NormalizedURL 平台 URL 归一化结果。
type NormalizedURL struct {
	OriginalURL  string
	ResolvedURL  string
	CanonicalURL string
	BrowseURL    string
	PlatformID   string
	ContentKind  string
	CookieDomain string
	QueryParams  map[string]string
	RiskFlags    []string
}

// ExtractQueryParams 从 URL query 中提取指定键（小写键名）。
func ExtractQueryParams(q url.Values, keys ...string) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MergeQueryParams 将 params 追加到 base URL（覆盖同名 query）。
func MergeQueryParams(base string, params map[string]string) string {
	base = strings.TrimSpace(base)
	if base == "" || len(params) == 0 {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// EffectiveBrowseURL 返回浏览器打开用的 URL（优先 BrowseURL，其次带 query 的 ResolvedURL）。
func (n *NormalizedURL) EffectiveBrowseURL() string {
	if n == nil {
		return ""
	}
	if u := strings.TrimSpace(n.BrowseURL); u != "" {
		return u
	}
	if u := strings.TrimSpace(n.ResolvedURL); u != "" {
		return u
	}
	if u := strings.TrimSpace(n.CanonicalURL); u != "" {
		if len(n.QueryParams) > 0 {
			return MergeQueryParams(u, n.QueryParams)
		}
		return u
	}
	return strings.TrimSpace(n.OriginalURL)
}
