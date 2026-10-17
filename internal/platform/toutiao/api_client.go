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

var articleFetchJSON = session.FetchJSON

// FetchArticleViaAPI 尝试通过浏览器上下文调用头条文章 detail API。
func FetchArticleViaAPI(ctx context.Context, norm *platform.NormalizedURL, cookieDomain string) (map[string]any, *platform.APIAttempt, int64, error) {
	// Both API endpoints and the reader's DOM fallback use the existing browser
	// invocation budget. A probe deadline must not cut off Chrome setup/warming.
	ctx = browser.WithBrowseRetryContext(ctx)
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
		if ctx.Err() != nil {
			return nil, &platform.APIAttempt{Fatal: true, Message: "请求已取消或时间预算耗尽"}, time.Since(start).Milliseconds(), ctx.Err()
		}
		fetchURL := "https://www.toutiao.com" + fmt.Sprintf(pathTpl, norm.PlatformID)
		result, err := articleFetchJSON(ctx, &browser.PageFetchRequest{
			WarmURL:         browseURL,
			FetchURL:        fetchURL,
			Method:          "GET",
			UseCookieDomain: cookieDomain,
			Referer:         browseURL,
		})
		latency := time.Since(start).Milliseconds()
		if err != nil {
			if stopErr, stop := platform.ShouldStopStrategy(err); stop {
				return nil, &platform.APIAttempt{Fatal: true, Message: stopErr.Error()}, latency, stopErr
			}
			lastErr = err
			lastAttempt = toutiaoAPIFallbackAttempt(0, fetchURL, "", "", "", err.Error())
			continue
		}
		attempt := classifyToutiaoAPIResult(fetchURL, result)
		attempt.Meta["api_fetch_mode"] = "browser_context_fetch"
		attempt.Meta["api_signature_mode"] = "page_runtime"
		if attempt.OK {
			return result.ParsedJSON, attempt, latency, nil
		}
		if stopErr, stop := platform.ShouldStopStrategy(attempt.Error()); stop {
			return nil, attempt, latency, stopErr
		}
		lastAttempt = attempt
		lastErr = attempt.Error()
	}
	if lastAttempt == nil {
		lastAttempt = &platform.APIAttempt{FallbackBrowser: true, Message: "all api endpoints failed"}
	}
	return nil, lastAttempt, time.Since(start).Milliseconds(), lastErr
}

func classifyToutiaoAPIResult(fetchURL string, result *browser.PageFetchResult) *platform.APIAttempt {
	if result == nil {
		return toutiaoAPIFallbackAttempt(0, fetchURL, "", "", "", "API 返回空结果")
	}
	body := strings.TrimSpace(result.Body)
	contentType := strings.TrimSpace(result.ContentType)
	attempt := platform.ClassifyAPIResponse(PlatformName, result.Status, result.ParsedJSON, toutiaoAPIEmpty)
	responseType := "json"
	if result.ParsedJSON == nil {
		responseType = classifyAPIResponseType(contentType, body)
		if !attempt.Fatal {
			msg := fmt.Sprintf("API 返回非 JSON 响应: %s", responseType)
			return toutiaoAPIFallbackAttempt(result.Status, fetchURL, contentType, responseType, bodyPrefix(body), msg)
		}
	}
	attempt.Meta["api_endpoint"] = fetchURL
	attempt.Meta["api_content_type"] = contentType
	attempt.Meta["api_response_type"] = responseType
	attempt.Meta["api_body_prefix"] = bodyPrefix(body)
	return attempt
}

func toutiaoAPIFallbackAttempt(status int, endpoint string, contentType string, responseType string, prefix string, message string) *platform.APIAttempt {
	if responseType == "" {
		responseType = "error"
	}
	meta := map[string]any{
		"platform":          PlatformName,
		"api_endpoint":      endpoint,
		"api_content_type":  contentType,
		"api_response_type": responseType,
	}
	if status > 0 {
		meta["api_http_status"] = status
	}
	if prefix != "" {
		meta["api_body_prefix"] = prefix
	}
	return &platform.APIAttempt{
		FallbackBrowser: true,
		Message:         message,
		HTTPStatus:      status,
		Meta:            meta,
	}
}

func classifyAPIResponseType(contentType, body string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "empty"
	}
	if strings.Contains(ct, "html") || strings.HasPrefix(strings.ToLower(trimmed), "<!doctype") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return "html"
	}
	if strings.Contains(strings.ToLower(trimmed), "登录") || strings.Contains(strings.ToLower(trimmed), "验证") {
		return "blocked_or_login"
	}
	return "non_json"
}

func bodyPrefix(body string) string {
	body = strings.TrimSpace(body)
	if len(body) > 160 {
		body = body[:160]
	}
	return body
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
