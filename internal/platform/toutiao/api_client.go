package toutiao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/session"
)

var articleAPIPaths = []string{
	"/article/v2/index/?group_id=%s",
	"/group/article_info/?group_id=%s",
}

// FetchArticleViaAPI 尝试通过浏览器上下文调用头条文章 detail API。
func FetchArticleViaAPI(ctx context.Context, norm *platform.NormalizedURL, cookieDomain string) (map[string]any, *platform.APIAttempt, int64, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		return nil, &platform.APIAttempt{FallbackBrowser: true, Message: "missing article id"},
			0, platform.NewAPIFallbackError("missing article id")
	}

	browseURL := norm.EffectiveBrowseURL()
	if browseURL == "" {
		browseURL = fmt.Sprintf("https://www.toutiao.com/article/%s/", norm.PlatformID)
	}

	var lastAttempt *platform.APIAttempt
	var lastErr error
	start := time.Now()
	for _, pathTpl := range articleAPIPaths {
		fetchURL := "https://www.toutiao.com" + fmt.Sprintf(pathTpl, norm.PlatformID)
		result, err := session.FetchJSON(ctx, &browser.PageFetchRequest{
			WarmURL:         browseURL,
			FetchURL:        fetchURL,
			Method:          "GET",
			UseCookieDomain: cookieDomain,
			Referer:         browseURL,
		})
		latency := time.Since(start).Milliseconds()
		if err != nil {
			lastErr = err
			lastAttempt = &platform.APIAttempt{FallbackBrowser: true, Message: err.Error()}
			continue
		}
		attempt := platform.ClassifyAPIResponse(PlatformName, result.Status, result.ParsedJSON, toutiaoAPIEmpty)
		attempt.Meta["api_fetch_mode"] = "browser_context_fetch"
		attempt.Meta["api_signature_mode"] = "page_runtime"
		if attempt.OK {
			return result.ParsedJSON, attempt, latency, nil
		}
		lastAttempt = attempt
		lastErr = attempt.Error()
	}
	if lastAttempt == nil {
		lastAttempt = &platform.APIAttempt{FallbackBrowser: true, Message: "all api endpoints failed"}
	}
	return nil, lastAttempt, time.Since(start).Milliseconds(), lastErr
}

func toutiaoAPIEmpty(parsed map[string]any) bool {
	if parsed == nil {
		return true
	}
	if data, ok := parsed["data"].(map[string]any); ok && len(data) > 0 {
		if platformMetaString(data, "title", "content", "abstract") != "" {
			return false
		}
	}
	if info, ok := parsed["article_info"].(map[string]any); ok && len(info) > 0 {
		return false
	}
	if title := platformMetaString(parsed, "title"); title != "" {
		return false
	}
	return true
}
