package x

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
)

// ReadPost 读取 X/Twitter 帖子。
func ReadPost(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	cookieDomain := strings.TrimSpace(in.UseCookieDomain)
	if cookieDomain == "" {
		cookieDomain = CookieDomain
	}
	spec := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    cookieDomain,
		MetadataScript:  MetadataScript(),
	}
	resp, err := platform.Browse(ctx, url, in, spec)
	if err != nil {
		return nil, err
	}
	return normalizePost(in, resp), nil
}
