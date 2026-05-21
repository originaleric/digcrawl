package xiaohongshu

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

// ReadNote 读取小红书笔记页。
func ReadNote(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
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

	resp, err := platform.BrowseWithFallbacks(ctx, browseURL, in, base, NoteBrowseAttempts)
	if err != nil {
		// selector 等待失败时尝试仅截图/元数据兜底
		if in.IncludeScreenshot {
			base.Action = "screenshot"
			base.WaitSelector = ""
			if r2, err2 := platform.Browse(ctx, browseURL, in, base); err2 == nil {
				resp = r2
				resp.Metadata = platform.AppendWarning(resp.Metadata, "browse selector failed, screenshot fallback")
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
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
	if code == platform.ErrPartialContent || code == platform.ErrSelectorStale {
		content.PlatformMetadata = platform.SetMetaError(content.PlatformMetadata, code, platform.MetaString(resp.Metadata, "error_message"))
	}
	return content, nil
}
