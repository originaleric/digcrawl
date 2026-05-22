package x

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/digcrawl/internal/platform"
)

// ReadPost 读取 X/Twitter 帖子（仅首条 status，不展开 thread）。
func ReadPost(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	norm, err := NormalizeURL(url)
	if err != nil {
		return nil, err
	}
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

	resp, err := platform.BrowseWithFallbacks(ctx, browseURL, in, base, PostBrowseAttempts)
	if err != nil {
		return nil, err
	}

	code := platform.MetaErrorCode(resp.Metadata)
	if platform.IsFatalErrorCode(code) {
		return nil, platform.NewReadError(code, platform.MetaString(resp.Metadata, "error_message"), PlatformName, resp.Metadata)
	}

	content := normalizePost(in, norm, resp)
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	return content, nil
}
