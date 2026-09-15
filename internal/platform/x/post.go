package x

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

// ReadPost 读取 X/Twitter 帖子（仅首条 status，不展开 thread）。
func ReadPost(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	return ReadPostWithConfig(ctx, in, config.Get().Platform.X)
}

// ReadPostWithConfig keeps every X read strategy bound to the configuration
// of the Runtime that registered the provider. ReadPost remains the direct
// compatibility entry point for callers that use the default profile.
func ReadPostWithConfig(ctx context.Context, in platform.ReadInput, cfg config.XAPIConfig) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	norm, err := NormalizeURL(url)
	if err != nil {
		return nil, err
	}

	var syndicationAttempt *platform.APIAttempt
	var syndicationLatency int64
	if parsed, attempt, latency, syndicationErr := FetchPostViaSyndication(ctx, norm, cfg); syndicationErr == nil && parsed != nil {
		if content, mapErr := MapSyndicationResponse(in, norm, parsed); mapErr == nil && content != nil {
			content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
			platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "api", ExtractionSource: "syndication", APILatencyMs: latency})
			return content, nil
		} else {
			syndicationAttempt, syndicationLatency = syndicationMapFailureAttempt(attempt), latency
		}
	} else if attempt != nil {
		// Syndication is an opportunistic public source, never a content-access
		// authority. Even 4xx responses must continue through the old chain.
		syndicationAttempt, syndicationLatency = attempt, latency
	}
	var apiAttempt *platform.APIAttempt
	var apiLatency int64
	if parsed, attempt, latency, apiErr := FetchPostViaAPI(ctx, norm, cfg); apiErr == nil && parsed != nil {
		if content, mapErr := MapAPIResponse(in, norm, parsed); mapErr == nil && content != nil {
			content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
			content.PlatformMetadata = annotateSyndicationAttempt(content.PlatformMetadata, syndicationAttempt)
			platform.MergeContentMetrics(content, platform.ReadMetrics{
				Platform:         PlatformName,
				Channel:          "api",
				ExtractionSource: "api",
				APILatencyMs:     syndicationLatency + latency,
			})
			return content, nil
		}
	} else if attempt != nil {
		apiAttempt = attempt
		apiLatency = latency
		if re, stop := platform.ShouldStopStrategy(apiErr); stop {
			return nil, re
		}
	}

	if parsed, attempt, latency, embedErr := FetchPostViaOEmbed(ctx, norm, cfg); embedErr == nil && parsed != nil {
		if content, mapErr := MapOEmbedResponse(in, norm, parsed); mapErr == nil && content != nil {
			content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
			content.PlatformMetadata = annotatePrimaryAPIAttempt(content.PlatformMetadata, apiAttempt)
			content.PlatformMetadata = annotateSyndicationAttempt(content.PlatformMetadata, syndicationAttempt)
			platform.MergeContentMetrics(content, platform.ReadMetrics{
				Platform:         PlatformName,
				Channel:          "api",
				ExtractionSource: "oembed",
				APILatencyMs:     syndicationLatency + apiLatency + latency,
			})
			return content, nil
		}
	} else if attempt != nil {
		apiLatency += latency
		if apiAttempt == nil {
			apiAttempt = attempt
		}
		if re, stop := platform.ShouldStopStrategy(embedErr); stop {
			return nil, re
		}
	}

	content, browserLatency, retryCount, err := readPostBrowser(ctx, in, norm, apiAttempt)
	if err != nil {
		return nil, err
	}
	content.PlatformMetadata = annotateSyndicationAttempt(content.PlatformMetadata, syndicationAttempt)
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		APILatencyMs:     syndicationLatency + apiLatency,
		BrowserLatencyMs: browserLatency,
		RetryCount:       retryCount,
	})
	return content, nil
}

func syndicationMapFailureAttempt(attempt *platform.APIAttempt) *platform.APIAttempt {
	endpoint := ""
	status := 0
	if attempt != nil {
		endpoint, status = platform.MetaString(attempt.Meta, "api_endpoint"), attempt.HTTPStatus
	}
	return syndicationAttempt(status, endpoint, "map_failed", "syndication response could not be mapped")
}

func annotateSyndicationAttempt(meta map[string]any, attempt *platform.APIAttempt) map[string]any {
	meta = platform.EnsureMeta(meta)
	if attempt == nil {
		return meta
	}
	if attempt.HTTPStatus > 0 {
		meta["syndication_http_status"] = attempt.HTTPStatus
	}
	if attempt.ErrorCode != "" {
		meta["syndication_error_code"] = attempt.ErrorCode
	}
	if attempt.Message != "" {
		meta["syndication_error_message"] = attempt.Message
	}
	if endpoint := platform.MetaString(attempt.Meta, "api_endpoint"); endpoint != "" {
		meta["syndication_endpoint"] = endpoint
	}
	return meta
}

func readPostBrowser(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, apiAttempt *platform.APIAttempt) (*platform.Content, int64, int, error) {
	browseURL := norm.CanonicalURL
	if browseURL == "" {
		browseURL = norm.ResolvedURL
	}

	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}
	// twitter.com cookie 统一到 x.com 存储域
	if strings.Contains(strings.ToLower(cookieDomain), "twitter.com") {
		cookieDomain = CookieDomain
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
		r, browseErr := platform.BrowseWithFallbacks(ctx, browseURL, in, base, PostBrowseAttempts)
		if browseErr != nil {
			return browseErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, time.Since(start).Milliseconds(), 0, err
	}

	code := platform.MetaErrorCode(resp.Metadata)
	if platform.IsFatalErrorCode(code) {
		return nil, time.Since(start).Milliseconds(), 0, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}

	content := normalizePost(in, norm, resp)
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	if err := validatePostContent(content); err != nil {
		return nil, time.Since(start).Milliseconds(), 0, err
	}
	return content, time.Since(start).Milliseconds(), 0, nil
}

func annotatePrimaryAPIAttempt(meta map[string]any, attempt *platform.APIAttempt) map[string]any {
	meta = platform.EnsureMeta(meta)
	if attempt == nil {
		return meta
	}
	if attempt.HTTPStatus > 0 {
		meta["tweet_lookup_http_status"] = attempt.HTTPStatus
	}
	if attempt.ErrorCode != "" {
		meta["tweet_lookup_error_code"] = attempt.ErrorCode
	}
	if attempt.Message != "" {
		meta["tweet_lookup_error_message"] = attempt.Message
	}
	if endpoint := platform.MetaString(attempt.Meta, "api_endpoint"); endpoint != "" {
		meta["tweet_lookup_endpoint"] = endpoint
	}
	return meta
}

func validatePostContent(content *platform.Content) error {
	if content == nil {
		return platform.NewReadError(platform.ErrPartialContent, "X 帖子结果为空", PlatformName, nil)
	}
	meta := platform.EnsureMeta(content.PlatformMetadata)
	if code := platform.MetaErrorCode(meta); platform.IsFatalErrorCode(code) {
		return platform.NewReadError(code, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	if isDeletedOrUnavailableShell(content) {
		meta = platform.SetMetaError(meta, platform.ErrContentNotFound, "X 帖子不存在、已删除或不可见")
		return platform.NewReadError(platform.ErrContentNotFound, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	if isLoginShell(content) {
		meta = platform.SetMetaError(meta, platform.ErrLoginRequired, "需要登录 X 后查看帖子")
		return platform.NewReadError(platform.ErrLoginRequired, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	if strings.TrimSpace(content.Text) == "" {
		meta = platform.SetMetaError(meta, platform.ErrPartialContent, "未提取到真实 X 帖子正文")
		return platform.NewReadError(platform.ErrPartialContent, platform.MetaString(meta, "error_message"), PlatformName, meta)
	}
	content.PlatformMetadata = meta
	return nil
}

func isDeletedOrUnavailableShell(content *platform.Content) bool {
	haystack := strings.ToLower(strings.Join([]string{content.Title, content.Text, content.Markdown}, "\n"))
	return strings.Contains(haystack, "doesn’t exist") ||
		strings.Contains(haystack, "doesn't exist") ||
		strings.Contains(haystack, "unable to show this content") ||
		strings.Contains(haystack, "private, deleted or only available on the app") ||
		strings.Contains(haystack, "this post is from an account that no longer exists")
}

func isLoginShell(content *platform.Content) bool {
	haystack := strings.ToLower(strings.Join([]string{content.Title, content.Text, content.Markdown}, "\n"))
	return strings.Contains(haystack, "log in") &&
		strings.Contains(haystack, "sign up") &&
		!strings.Contains(haystack, "data-testid")
}
