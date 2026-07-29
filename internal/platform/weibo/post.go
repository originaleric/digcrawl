package weibo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

func ReadPost(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	norm, err := NormalizeURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind == "user" {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "当前 URL 不是微博详情页", PlatformName, map[string]any{"page_kind": norm.ContentKind})
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}
	var apiAttempt *platform.APIAttempt
	var apiLatency int64
	if parsed, attempt, latency, apiErr := FetchPostViaAPI(ctx, norm); apiErr == nil && parsed != nil {
		if content, mapErr := mapAPIResponse(in, norm, parsed); mapErr == nil && content != nil {
			content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
			platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "api", ExtractionSource: "api", APILatencyMs: latency})
			return content, nil
		}
	} else if attempt != nil {
		apiAttempt = attempt
		apiLatency = latency
		if re, stop := platform.ShouldStopStrategy(apiErr); stop {
			return nil, re
		}
	}
	start := time.Now()
	resp, err := platform.Browse(ctx, norm.EffectiveBrowseURL(), in, platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    cookieDomain,
		MetadataScript:  MetadataScript(),
	})
	if err != nil {
		return nil, err
	}
	content := normalizePost(in, norm, resp)
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "browser", ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"), APILatencyMs: apiLatency, BrowserLatencyMs: time.Since(start).Milliseconds()})
	return content, nil
}
