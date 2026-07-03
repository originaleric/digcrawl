package toutiao

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	articlePreflightTimeout = 5 * time.Second
	articlePreflightBodyMax = 16 * 1024
)

var articlePreflightHTTPClient = &http.Client{
	Timeout: articlePreflightTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

func checkArticlePreflight(ctx context.Context, norm *platform.NormalizedURL) error {
	if norm == nil {
		return nil
	}
	browseURL := strings.TrimSpace(norm.EffectiveBrowseURL())
	if browseURL == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	preflightCtx, cancel := context.WithTimeout(ctx, articlePreflightTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(preflightCtx, http.MethodGet, browseURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Range", "bytes=0-16383")

	resp, err := articlePreflightHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var body []byte
	if resp.Body != nil {
		body, _ = io.ReadAll(io.LimitReader(resp.Body, articlePreflightBodyMax))
	}
	finalURL := browseURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if re := classifyArticlePreflight(finalURL, resp.StatusCode, resp.Header.Get("Content-Type"), string(body)); re != nil {
		return re
	}
	if finalHostMatches(finalURL, toutiaoHosts) {
		norm.ResolvedURL = finalURL
	}
	return nil
}

func classifyArticlePreflight(finalURL string, status int, contentType, body string) *platform.ReadError {
	finalURL = strings.TrimSpace(finalURL)
	meta := platform.EnsureMeta(nil)
	if finalURL != "" {
		meta["preflight_final_url"] = finalURL
	}
	if status > 0 {
		meta["preflight_http_status"] = status
	}
	if ct := strings.TrimSpace(contentType); ct != "" {
		meta["preflight_content_type"] = ct
	}

	lowerURL := strings.ToLower(finalURL)
	lowerBody := strings.ToLower(body)
	if isKnownNonArticleURL(lowerURL) {
		meta["page_kind"] = "non_article"
		meta = platform.SetMetaError(meta, platform.ErrContentTypeMismatch, "当前 URL 不是头条文章页")
		return platform.NewReadError(platform.ErrContentTypeMismatch, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	if strings.Contains(lowerBody, "sslocal://awemevideo") || strings.Contains(lowerBody, "video_oracle") || strings.Contains(lowerBody, "hotsoon_video") {
		meta["page_kind"] = "video"
		meta = platform.SetMetaError(meta, platform.ErrContentTypeMismatch, "当前 URL 不是头条文章页")
		return platform.NewReadError(platform.ErrContentTypeMismatch, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	return nil
}

func isKnownNonArticleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.EscapedPath())
	if hostMatches(host, []string{"ixigua.com", "douyin.com", "iesdouyin.com", "hotsoon.com"}) {
		return true
	}
	for _, marker := range []string{"/video/", "/aweme/", "awemevideo", "shortvideo", "hotsoon_video", "video_oracle"} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

func finalHostMatches(rawURL string, allowed []string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return hostMatches(u.Hostname(), allowed)
}
