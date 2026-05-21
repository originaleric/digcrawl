package douyin

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

// ReadVideo 读取抖音视频页。
func ReadVideo(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}

	norm, err := NormalizeURL(ctx, url)
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

	base := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    cookieDomain,
		MetadataScript:  MetadataScript(),
	}

	resp, err := platform.BrowseWithFallbacks(ctx, browseURL, in, base, VideoBrowseAttempts)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.URL != "" {
		norm.ResolvedURL = resp.URL
		if id, kind := extractWorkID(resp.URL); id != "" {
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
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	return content, nil
}
