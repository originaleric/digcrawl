package wechat

import (
	"context"
	"fmt"
	"strings"

	"github.com/originaleric/diggate/internal/platform"
	"github.com/originaleric/diggate/internal/browser"
)

const (
	PlatformName = "wechat"
	ContentType  = "article"
	CookieDomain = "mp.weixin.qq.com"
)

var articleSelectors = []struct {
	wait    string
	content string
}{
	{wait: "#js_article", content: "#js_article"},
	{wait: "#js_content", content: "#js_content"},
}

// ReadArticle 读取微信公众号文章。
func ReadArticle(ctx context.Context, in platform.ReadInput) (*platform.Content, error) {
	url := strings.TrimSpace(in.URL)
	if url == "" {
		return nil, fmt.Errorf("url 不能为空")
	}
	var lastErr error
	var resp *browser.BrowserBrowseResponse
	for _, sel := range articleSelectors {
		spec := platform.BrowseSpec{
			WaitSelector:    sel.wait,
			ContentSelector: sel.content,
			CookieDomain:    CookieDomain,
			MetadataScript:  metadataScript(),
		}
		r, err := platform.Browse(ctx, url, in, spec)
		if err != nil {
			lastErr = err
			continue
		}
		resp = r
		break
	}
	if resp == nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("未能读取公众号文章")
	}
	return normalizeArticle(in, resp), nil
}

func normalizeArticle(in platform.ReadInput, resp *browser.BrowserBrowseResponse) *platform.Content {
	c := &platform.Content{
		Platform:     PlatformName,
		ContentType:  ContentType,
		SourceURL:    resp.URL,
		CanonicalURL: resp.URL,
		Title:        resp.Title,
		Text:         resp.Text,
		Markdown:     resp.Markdown,
		CapturedAt:   platform.NowRFC3339(),
	}
	if resp.Metadata != nil {
		c.Author = platform.AuthorFromMeta(resp.Metadata)
		if pub := platform.MetaString(resp.Metadata, "published_at"); pub != "" {
			c.PublishedAt = pub
		}
		c.PlatformMetadata = resp.Metadata
	}
	if in.IncludeScreenshot {
		c.ScreenshotBase64 = resp.ScreenshotBase
	}
	return c
}

func metadataScript() string {
	return `(() => {
  const author = document.querySelector('#js_name')?.innerText
    || document.querySelector('.profile_nickname')?.innerText
    || document.querySelector('meta[property="og:article:author"]')?.content
    || '';
  const published = document.querySelector('#publish_time')?.innerText
    || document.querySelector('meta[property="og:article:published_time"]')?.content
    || '';
  return { author_name: (author || '').trim(), published_at: (published || '').trim() };
})()`
}
