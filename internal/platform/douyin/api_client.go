package douyin

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/session"
)

const detailAPIPath = "/aweme/v1/web/aweme/detail/"

// FetchVideoViaAPI 通过浏览器上下文调用抖音 aweme detail API。
func FetchVideoViaAPI(ctx context.Context, norm *platform.NormalizedURL, cookieDomain string) (map[string]any, *platform.APIAttempt, error) {
	if norm == nil || strings.TrimSpace(norm.PlatformID) == "" {
		return nil, &platform.APIAttempt{
			FallbackBrowser: true,
			Message:         "missing aweme id",
		}, platform.NewAPIFallbackError("missing aweme id")
	}

	browseURL := norm.EffectiveBrowseURL()
	if browseURL == "" {
		browseURL = fmt.Sprintf("https://www.douyin.com/video/%s", norm.PlatformID)
	}
	q := url.Values{}
	q.Set("aweme_id", norm.PlatformID)
	fetchURL := "https://www.douyin.com" + detailAPIPath + "?" + q.Encode()

	result, err := session.FetchJSON(ctx, &browser.PageFetchRequest{
		WarmURL:         browseURL,
		FetchURL:        fetchURL,
		Method:          "GET",
		UseCookieDomain: cookieDomain,
		Referer:         browseURL,
	})
	if err != nil {
		if stopErr, stop := platform.ShouldStopStrategy(err); stop {
			return nil, &platform.APIAttempt{Fatal: true, Message: stopErr.Error()}, stopErr
		}
		return nil, &platform.APIAttempt{
			FallbackBrowser: true,
			Message:         err.Error(),
		}, platform.NewAPIFallbackError(err.Error())
	}

	attempt := platform.ClassifyAPIResponse(PlatformName, result.Status, result.ParsedJSON, dyDetailEmpty)
	attempt.Meta["api_fetch_mode"] = "browser_context_fetch"
	attempt.Meta["api_signature_mode"] = "page_runtime"
	if !attempt.OK {
		return nil, attempt, attempt.Error()
	}
	return result.ParsedJSON, attempt, nil
}

func dyDetailEmpty(parsed map[string]any) bool {
	detail, ok := parsed["aweme_detail"].(map[string]any)
	if !ok || len(detail) == 0 {
		return true
	}
	return false
}

// MapDetailResponse 将 aweme detail API JSON 映射为 Content。
func MapDetailResponse(in platform.ReadInput, norm *platform.NormalizedURL, parsed map[string]any) (*platform.Content, error) {
	detail, ok := parsed["aweme_detail"].(map[string]any)
	if !ok || len(detail) == 0 {
		return nil, fmt.Errorf("detail response missing aweme_detail")
	}
	return mapAwemeDetail(in, norm, detail), nil
}
