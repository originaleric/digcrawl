package bilibili

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

func ReadVideo(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	norm, err := NormalizeURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind == "user" {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "当前 URL 不是 B站视频页", PlatformName, map[string]any{"page_kind": norm.ContentKind})
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}
	var apiAttempt *platform.APIAttempt
	var apiLatency int64
	var apiContent *platform.Content
	if parsed, attempt, latency, apiErr := FetchVideoViaAPI(ctx, norm); apiErr == nil && parsed != nil {
		if content, mapErr := mapAPIResponse(in, norm, parsed); mapErr == nil && content != nil {
			// The view API supplies page metadata and the cover, but not a
			// playable video reference. When the caller requested media (including
			// explicit provider retention), continue through the existing browser
			// capture path instead of reporting this video as unavailable.
			if !needsBrowserVideoCapture(in, content) {
				content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, attempt)
				platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "api", ExtractionSource: "api", APILatencyMs: latency})
				return content, nil
			}
			apiContent = content
			apiAttempt = attempt
			apiLatency = latency
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
		CaptureMedia:    in.IncludeMedia,
	})
	if err != nil {
		// Video capture is an optional enhancement of an otherwise complete API
		// read. Keep the established API-first content result available when a
		// local browser is unavailable, logged out, or times out.
		if apiContent != nil {
			apiContent.PlatformMetadata = platform.MergeAPIMeta(apiContent.PlatformMetadata, apiAttempt)
			platform.MergeContentMetrics(apiContent, platform.ReadMetrics{Platform: PlatformName, Channel: "api", ExtractionSource: "api", APILatencyMs: apiLatency})
			return apiContent, nil
		}
		return nil, err
	}
	content := normalizeBrowserContent(in, norm, resp)
	if apiContent != nil {
		// The API response remains the authoritative page result. Browser work
		// is needed solely because that API does not expose a playable stream;
		// never let a login shell or otherwise incomplete browser page replace
		// already-successful title, text, author, and metadata.
		appendBrowserVideoMedia(apiContent, content)
		apiContent.PlatformMetadata = platform.MergeAPIMeta(apiContent.PlatformMetadata, apiAttempt)
		platform.MergeContentMetrics(apiContent, platform.ReadMetrics{
			Platform:         PlatformName,
			Channel:          "api",
			ExtractionSource: "api",
			APILatencyMs:     apiLatency,
			BrowserLatencyMs: time.Since(start).Milliseconds(),
		})
		return apiContent, nil
	}
	content.PlatformMetadata = platform.MergeAPIMeta(content.PlatformMetadata, apiAttempt)
	platform.MergeContentMetrics(content, platform.ReadMetrics{
		Platform:         PlatformName,
		Channel:          "browser",
		ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"),
		APILatencyMs:     apiLatency,
		BrowserLatencyMs: time.Since(start).Milliseconds(),
	})
	return content, nil
}

// needsBrowserVideoCapture preserves API-first reads unless the caller asked
// for media and the API result cannot provide a playable video reference.
// Bilibili's view API normally contains only the cover image, while the
// established browser capture path can observe the page's resolved media.
func needsBrowserVideoCapture(in platform.ReadInput, content *platform.Content) bool {
	if !in.IncludeMedia || content == nil {
		return false
	}
	return !hasVideoMedia(content.Media)
}

func hasVideoMedia(items []platform.MediaItem) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Type), "video") && (strings.TrimSpace(item.URL) != "" || strings.TrimSpace(item.ArtifactID) != "") {
			return true
		}
	}
	return false
}

// appendBrowserVideoMedia keeps API content as the source of truth while
// adding only browser-confirmed video references. The generic retention layer
// will perform the bounded download and final file validation afterwards.
func appendBrowserVideoMedia(apiContent, browserContent *platform.Content) {
	if apiContent == nil || browserContent == nil {
		return
	}
	for _, item := range browserContent.Media {
		if strings.EqualFold(strings.TrimSpace(item.Type), "video") && (strings.TrimSpace(item.URL) != "" || strings.TrimSpace(item.ArtifactID) != "") {
			apiContent.Media = append(apiContent.Media, item)
		}
	}
}
