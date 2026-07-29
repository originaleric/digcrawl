package zhihu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

func ReadContent(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	norm, err := NormalizeURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if norm.ContentKind == "user" {
		return nil, platform.NewReadError(platform.ErrContentTypeMismatch, "当前 URL 不是知乎内容详情页", PlatformName, map[string]any{"page_kind": norm.ContentKind})
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = norm.CookieDomain
	}
	start := time.Now()
	resp, err := platform.Browse(ctx, norm.EffectiveBrowseURL(), in, platform.BrowseSpec{WaitSelector: DefaultWaitSelector, ContentSelector: DefaultContentSelector, CookieDomain: cookieDomain, MetadataScript: MetadataScript()})
	if err != nil {
		return nil, err
	}
	content := normalizeContent(in, norm, resp)
	platform.MergeContentMetrics(content, platform.ReadMetrics{Platform: PlatformName, Channel: "browser", ExtractionSource: platform.MetaString(content.PlatformMetadata, "extraction_source"), BrowserLatencyMs: time.Since(start).Milliseconds()})
	return content, nil
}
