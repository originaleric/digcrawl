package xiaohongshu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

// ReadNote 读取小红书笔记页（API 优先，Browser 兜底）。
func ReadNote(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	ctx = browser.WithBrowseRetryContext(ctx)
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
	if parsed, attempt, apiErr := FetchNoteViaAPI(ctx, norm, cookieDomain); apiErr == nil && parsed != nil {
		apiLatency = time.Since(apiStart).Milliseconds()
		if content, mapErr := MapFeedResponse(in, norm, parsed); mapErr == nil && content != nil {
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
	content, err := readNoteBrowser(ctx, in, norm, cookieDomain, apiAttempt)
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

func readNoteBrowser(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, cookieDomain string, apiAttempt *platform.APIAttempt) (*platform.Content, error) {
	return readNoteBrowserWithBrowse(ctx, in, norm, cookieDomain, apiAttempt, platform.BrowseWithFallbacks, platform.Browse)
}

func readNoteBrowserWithBrowse(ctx context.Context, in platform.ReadInput, norm *platform.NormalizedURL, cookieDomain string, apiAttempt *platform.APIAttempt,
	browse func(context.Context, string, platform.ReadInput, platform.BrowseSpec, []platform.BrowseAttempt) (*browser.BrowserBrowseResponse, error),
	screenshot func(context.Context, string, platform.ReadInput, platform.BrowseSpec) (*browser.BrowserBrowseResponse, error)) (*platform.Content, error) {
	browseURL := norm.EffectiveBrowseURL()
	if browseURL == "" {
		browseURL = norm.ResolvedURL
	}

	base := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    cookieDomain,
		MetadataScript:  MetadataScript(),
		CaptureMedia:    in.IncludeMedia,
	}

	var resp *browser.BrowserBrowseResponse
	err := platform.WithBrowseRetry(ctx, platform.BrowseRetryPolicy(), func(ctx context.Context) error {
		r, browseErr := browse(ctx, browseURL, in, base, NoteBrowseAttempts)
		if browseErr != nil {
			if in.IncludeScreenshot && ctx.Err() == nil && browser.CanBrowseFallback(browseErr) {
				base.Action = "screenshot"
				base.WaitSelector = ""
				screenshotInput := in
				screenshotInput.WaitSelector = ""
				if r2, err2 := screenshot(ctx, browseURL, screenshotInput, base); err2 == nil && r2 != nil {
					if code := platform.MetaErrorCode(r2.Metadata); platform.IsFatalErrorCode(code) {
						return platform.NewReadError(code, platform.MetaString(r2.Metadata, "error_message"), PlatformName, r2.Metadata)
					}
					// A screenshot alone does not satisfy a note read. Accept this fallback
					// only when metadata recovered usable text, and explicitly mark it partial.
					if strings.TrimSpace(r2.Text) == "" && strings.TrimSpace(platform.MetaString(r2.Metadata, "text")) == "" {
						return browseErr
					}
					resp = r2
					resp.Metadata = platform.AppendWarning(resp.Metadata, "browse selector failed, screenshot fallback")
					resp.Metadata = platform.SetMetaError(resp.Metadata, platform.ErrPartialContent, "正文选择器失败，返回截图及可用的部分内容")
					return nil
				} else if err2 != nil {
					return err2
				}
			}
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
		if id := extractNoteID(strings.TrimSpace(resp.URL)); id != "" {
			norm.PlatformID = id
			norm.CanonicalURL = fmt.Sprintf("https://www.xiaohongshu.com/explore/%s", id)
		}
	}

	code := platform.MetaErrorCode(resp.Metadata)
	if platform.IsFatalErrorCode(code) {
		return nil, platform.NewReadError(
			code,
			platform.MetaString(resp.Metadata, "error_message"),
			PlatformName,
			resp.Metadata,
		)
	}

	content := normalizeNote(in, norm, resp)
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	if src := platform.MetaString(content.PlatformMetadata, "extraction_source"); src == "" {
		content.PlatformMetadata["extraction_source"] = platform.MetaString(resp.Metadata, "extraction_source")
	}
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	return content, nil
}
