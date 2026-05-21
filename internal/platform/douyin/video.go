package douyin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/diggate/internal/browser"
	"github.com/originaleric/diggate/internal/platform"
)

// ReadVideo 读取抖音视频页（API 优先，Browser 兜底）。
func ReadVideo(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	norm, err := NormalizeURL(ctx, url)
	if err != nil {
		return nil, err
	}

	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}

	var apiAttempt *platform.APIAttempt
	var apiLatency int64
	apiStart := time.Now()
	if parsed, attempt, apiErr := FetchVideoViaAPI(ctx, norm, cookieDomain); apiErr == nil && parsed != nil {
		apiLatency = time.Since(apiStart).Milliseconds()
		if content, mapErr := MapDetailResponse(in, norm, parsed); mapErr == nil && content != nil {
			platform.MergeContentMetrics(content, platform.ReadMetrics{
				Platform:         PlatformName,
				Channel:          "api",
				ExtractionSource: "api",
				APILatencyMs:     apiLatency,
			})
			return content, nil
		}
	} else if attempt != nil {
		apiLatency = time.Since(apiStart).Milliseconds()
		apiAttempt = attempt
		if re, stop := platform.ShouldStopStrategy(apiErr); stop {
			return nil, re
		}
	}

	browserStart := time.Now()
	content, err := readVideoBrowser(ctx, in, norm, cookieDomain, apiAttempt)
	if err != nil {
		return nil, err
	}
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		APILatencyMs:     apiLatency,
		BrowserLatencyMs: time.Since(browserStart).Milliseconds(),
	})
	return content, nil
}

func readVideoBrowser(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, cookieDomain string, apiAttempt *platform.APIAttempt) (*platform.Content, error) {
	browseURL := norm.EffectiveBrowseURL()
	if browseURL == "" {
		browseURL = norm.ResolvedURL
	}

	base := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    cookieDomain,
		MetadataScript:  MetadataScript(),
	}

	var resp *browser.BrowserBrowseResponse
	err := platform.WithBrowseRetry(ctx, platform.BrowseRetryPolicy(), func(ctx context.Context) error {
		r, browseErr := platform.BrowseWithFallbacks(ctx, browseURL, in, base, VideoBrowseAttempts)
		if browseErr != nil {
			return browseErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.URL != "" {
		norm.ResolvedURL = resp.URL
		if id, kind := extractWorkIDFromURL(resp.URL); id != "" {
			norm.PlatformID = id
			norm.ContentKind = kind
			norm.CanonicalURL = fmt.Sprintf("https://www.douyin.com/%s/%s", kind, id)
		}
	}

	code := platform.MetaErrorCode(resp.Metadata)
	if platform.IsFatalErrorCode(code) {
		return nil, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}

	content := normalizeVideo(in, norm, resp)
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	if src := platform.MetaString(content.PlatformMetadata, "extraction_source"); src == "" {
		content.PlatformMetadata["extraction_source"] = platform.MetaString(resp.Metadata, "extraction_source")
	}
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	return content, nil
}
