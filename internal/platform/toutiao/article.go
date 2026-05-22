package toutiao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

// ReadArticle 读取今日头条文章（API 优先，Browser 兜底）。
func ReadArticle(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
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
	if parsed, attempt, latency, apiErr := FetchArticleViaAPI(ctx, norm, cookieDomain); apiErr == nil && parsed != nil {
		if content, mapErr := MapAPIResponse(in, norm, parsed); mapErr == nil && content != nil {
			metrics := platform.ReadMetrics{
				Platform:         PlatformName,
				Channel:          "api",
				ExtractionSource: "api",
				APILatencyMs:     latency,
			}
			platform.MergeContentMetrics(content, metrics)
			return content, nil
		}
	} else if attempt != nil {
		apiAttempt = attempt
		apiLatency = latency
		if re, stop := platform.ShouldStopStrategy(apiErr); stop {
			return nil, re
		}
	}

	content, browserLatency, retryCount, err := readArticleBrowser(ctx, in, norm, cookieDomain, apiAttempt)
	if err != nil {
		return nil, err
	}
	metrics := platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		APILatencyMs:     apiLatency,
		BrowserLatencyMs: browserLatency,
		RetryCount:       retryCount,
	}
	platform.MergeContentMetrics(content, metrics)
	return content, nil
}

func readArticleBrowser(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, cookieDomain string, apiAttempt *platform.APIAttempt) (*platform.Content, int64, int, error) {
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
	start := time.Now()
	err := platform.WithBrowseRetry(ctx, platform.BrowseRetryPolicy(), func(ctx context.Context) error {
		r, browseErr := platform.BrowseWithFallbacks(ctx, browseURL, in, base, ArticleBrowseAttempts)
		if browseErr != nil {
			return browseErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, time.Since(start).Milliseconds(), 0, err
	}

	if resp != nil && resp.URL != "" {
		norm.ResolvedURL = resp.URL
	}

	code := platform.MetaErrorCode(resp.Metadata)
	if platform.IsFatalErrorCode(code) {
		return nil, time.Since(start).Milliseconds(), 0, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}

	content := normalizeArticle(in, norm, resp)
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	if src := platform.MetaString(content.PlatformMetadata, "extraction_source"); src == "" {
		content.PlatformMetadata["extraction_source"] = platform.MetaString(resp.Metadata, "extraction_source")
	}
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	return content, time.Since(start).Milliseconds(), 0, nil
}
