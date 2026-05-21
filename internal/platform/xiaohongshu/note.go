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
	spec := platform.BrowseSpec{
		WaitSelector:    DefaultWaitSelector,
		ContentSelector: DefaultContentSelector,
		CookieDomain:    CookieDomain,
		MetadataScript:  MetadataScript(),
	}
	resp, err := platform.Browse(ctx, url, in, spec)
	if err != nil {
		return nil, err
	}
	return normalizeNote(in, resp), nil
}
