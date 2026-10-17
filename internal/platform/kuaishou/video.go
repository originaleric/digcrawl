package kuaishou

import (
	"context"
	"errors"
	"fmt"
	"github.com/originaleric/digcrawl/internal/browser"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

func ReadVideo(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	ctx = browser.WithBrowseRetryContext(ctx)
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	norm, err := NormalizeURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind == "user" {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "当前 URL 不是快手视频页", PlatformName, map[string]any{"page_kind": norm.ContentKind})
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}
	var apiAttempt *platform.APIAttempt
	var apiLatency int64
	if content, attempt, latency, apiErr := FetchVideoViaAPI(ctx, norm, in); apiErr == nil && content != nil {
		content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
		platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "api", ExtractionSource: "api", APILatencyMs: latency})
		return content, nil
	} else {
		if re, stop := platform.ShouldStopStrategy(apiErr); stop {
			return nil, re
		}
		if attempt != nil {
			apiAttempt = attempt
			apiLatency = latency
		}
	}
	browseCtx, cancel := context.WithTimeout(ctx, kuaishouBrowseTimeout())
	defer cancel()
	start := time.Now()
	resp, err := platform.Browse(browseCtx, norm.EffectiveBrowseURL(), in, platform.BrowseSpec{WaitSelector: DefaultWaitSelector, ContentSelector: DefaultContentSelector, CookieDomain: cookieDomain, MetadataScript: MetadataScript(), CaptureMedia: in.IncludeMedia})
	if err != nil {
		return nil, classifyBrowseError(err)
	}
	if code := platform.MetaErrorCode(resp.Metadata); platform.IsFatalErrorCode(code) {
		return nil, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}
	content := normalizeVideo(in, norm, resp)
	if err := validateVideoContent(content); err != nil {
		return nil, err
	}
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "browser", ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"), APILatencyMs: apiLatency, BrowserLatencyMs: time.Since(start).Milliseconds()})
	return content, nil
}

func kuaishouBrowseTimeout() time.Duration {
	return 35 * time.Second
}

func classifyBrowseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return platform.NewReadError(platform.ErrPartialContent, "快手页面浏览器采集超时", PlatformName, map[string]any{
			"error_source": "browser",
			"raw_error":    err.Error(),
		})
	}
	return err
}
