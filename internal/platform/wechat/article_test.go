package wechat

import (
	"context"
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestNormalizeArticleIncludeMedia(t *testing.T) {
	t.Parallel()
	in := platform.ReadInput{IncludeMedia: true}
	resp := &browser.BrowserBrowseResponse{
		URL:      "https://mp.weixin.qq.com/s/abc",
		Title:    "Weixin Official Accounts Platform",
		Markdown: "![](https://mmbiz.qpic.cn/img.jpg)",
		Metadata: map[string]any{
			"title":        "真实文章标题",
			"text":         "正文内容",
			"author_name":  "Author",
			"published_at": "2026-01-01",
			"image_urls":   []any{"https://mmbiz.qpic.cn/a.jpg", "https://mmbiz.qpic.cn/b.jpg"},
			"final_url":    "https://mp.weixin.qq.com/s/abc?real=1",
		},
	}
	c := normalizeArticle(in, resp)
	if c.Title != "真实文章标题" {
		t.Fatalf("expected metadata title override, got %q", c.Title)
	}
	if c.Text != "正文内容" {
		t.Fatalf("expected metadata text fallback, got %q", c.Text)
	}
	if c.SourceURL != "https://mp.weixin.qq.com/s/abc?real=1" {
		t.Fatalf("expected final url, got %q", c.SourceURL)
	}
	if len(c.Media) != 2 {
		t.Fatalf("expected 2 media items, got %d", len(c.Media))
	}
	if c.Media[0].Type != "image" || c.Media[0].URL == "" {
		t.Fatalf("unexpected media: %+v", c.Media[0])
	}
}

func TestNormalizeArticleSkipMediaWhenDisabled(t *testing.T) {
	t.Parallel()
	in := platform.ReadInput{IncludeMedia: false}
	resp := &browser.BrowserBrowseResponse{
		Metadata: map[string]any{"image_urls": []any{"https://mmbiz.qpic.cn/a.jpg"}},
	}
	c := normalizeArticle(in, resp)
	if len(c.Media) != 0 {
		t.Fatalf("expected no media when IncludeMedia=false, got %d", len(c.Media))
	}
}

func TestValidateArticleContentRejectsPlaceholderTitle(t *testing.T) {
	t.Parallel()
	content := &platform.Content{
		Title: "Weixin Official Accounts Platform",
		Text:  "正文内容",
		PlatformMetadata: map[string]any{
			"page_title": "Weixin Official Accounts Platform",
		},
	}
	err := validateArticleContent(content, "#js_content")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrSelectorStale {
		t.Fatalf("expected %s, got %s", platform.ErrSelectorStale, re.Code)
	}
	if platform.MetaString(re.Meta, "matched_selector") != "#js_content" {
		t.Fatalf("expected matched_selector in metadata, got %#v", re.Meta)
	}
}

func TestValidateArticleContentRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	content := &platform.Content{
		Title: "真实文章标题",
		Text:  "",
		PlatformMetadata: map[string]any{
			"page_title":       "真实文章标题",
			"body_text_length": 0,
		},
	}
	err := validateArticleContent(content, "#js_content")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrPartialContent {
		t.Fatalf("expected %s, got %s", platform.ErrPartialContent, re.Code)
	}
}

func TestValidateArticleContentRejectsStructuredMetaError(t *testing.T) {
	t.Parallel()
	content := &platform.Content{
		Title: "真实文章标题",
		Text:  "正文内容",
		PlatformMetadata: map[string]any{
			"error_code":    platform.ErrRateLimited,
			"error_message": "公众号访问过于频繁",
			"warnings":      []string{"page title is platform shell"},
		},
	}
	err := validateArticleContent(content, "#js_article")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrRateLimited {
		t.Fatalf("expected %s, got %s", platform.ErrRateLimited, re.Code)
	}
	if platform.MetaString(re.Meta, "error_message") != "公众号访问过于频繁" {
		t.Fatalf("unexpected error message metadata: %#v", re.Meta)
	}
}

func TestValidateArticleContentRejectsOffDomainFinalURL(t *testing.T) {
	t.Parallel()
	content := &platform.Content{
		SourceURL: "https://evil.example/landing",
		Title:     "真实文章标题",
		Text:      "正文内容",
		PlatformMetadata: map[string]any{
			"final_url": "https://evil.example/landing",
		},
	}
	err := validateArticleContent(content, "#js_content")
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrScopeViolation {
		t.Fatalf("expected %s, got %s", platform.ErrScopeViolation, re.Code)
	}
	if platform.MetaString(re.Meta, "scope_error") == "" {
		t.Fatalf("expected scope_error metadata, got %#v", re.Meta)
	}
}

func TestWithBrowseDiagnosticsPreservesContext(t *testing.T) {
	t.Parallel()
	resp := &browser.BrowserBrowseResponse{
		URL:   "https://mp.weixin.qq.com/s/test",
		Title: "Weixin Official Accounts Platform",
		Text:  "",
	}
	out := withBrowseDiagnostics(resp, "#js_article")
	if platform.MetaString(out.Metadata, "final_url") != "https://mp.weixin.qq.com/s/test" {
		t.Fatalf("missing final_url: %#v", out.Metadata)
	}
	if platform.MetaString(out.Metadata, "page_title") != "Weixin Official Accounts Platform" {
		t.Fatalf("missing page_title: %#v", out.Metadata)
	}
	if platform.MetaString(out.Metadata, "matched_selector") != "#js_article" {
		t.Fatalf("missing matched_selector: %#v", out.Metadata)
	}
	if platform.MetaString(out.Metadata, "body_text_length") != "0" {
		t.Fatalf("expected body_text_length=0, got %#v", out.Metadata)
	}
}

func TestReadArticleBrowserRetriesOnSelectorStaleAndSucceeds(t *testing.T) {
	t.Parallel()

	attempts := 0
	content, retryCount, err := readArticleBrowserWithOptions(context.Background(), platform.ReadInput{
		URL: "https://mp.weixin.qq.com/s/retry-title",
	}, "https://mp.weixin.qq.com/s/retry-title", func(ctx context.Context, url string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
		attempts++
		if attempts == 1 {
			return &browser.BrowserBrowseResponse{
				URL:   url,
				Title: "Weixin Official Accounts Platform",
				Text:  "正文内容",
			}, "#js_article", nil
		}
		return &browser.BrowserBrowseResponse{
			URL:   url,
			Title: "真实文章标题",
			Text:  "恢复后的正文",
		}, "#js_content", nil
	}, platform.RetryPolicy{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("expected retry to recover, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 browse attempts, got %d", attempts)
	}
	if retryCount != 1 {
		t.Fatalf("expected retryCount=1, got %d", retryCount)
	}
	if content.Title != "真实文章标题" || content.Text != "恢复后的正文" {
		t.Fatalf("unexpected content after retry: %+v", content)
	}
	if platform.MetaString(content.PlatformMetadata, "matched_selector") != "#js_content" {
		t.Fatalf("expected matched_selector from successful retry, got %#v", content.PlatformMetadata)
	}
}

func TestReadArticleBrowserRetriesOnPartialContentAndSucceeds(t *testing.T) {
	t.Parallel()

	attempts := 0
	content, retryCount, err := readArticleBrowserWithOptions(context.Background(), platform.ReadInput{
		URL: "https://mp.weixin.qq.com/s/retry-body",
	}, "https://mp.weixin.qq.com/s/retry-body", func(ctx context.Context, url string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
		attempts++
		if attempts == 1 {
			return &browser.BrowserBrowseResponse{
				URL:   url,
				Title: "真实文章标题",
				Text:  "",
			}, "#js_content", nil
		}
		return &browser.BrowserBrowseResponse{
			URL:   url,
			Title: "真实文章标题",
			Text:  "补齐后的正文",
		}, "#js_content", nil
	}, platform.RetryPolicy{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("expected retry to recover, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 browse attempts, got %d", attempts)
	}
	if retryCount != 1 {
		t.Fatalf("expected retryCount=1, got %d", retryCount)
	}
	if content.Text != "补齐后的正文" {
		t.Fatalf("unexpected content after retry: %+v", content)
	}
}

func TestReadArticleBrowserRejectsOffDomainFinalURL(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, retryCount, err := readArticleBrowserWithOptions(context.Background(), platform.ReadInput{
		URL: "https://mp.weixin.qq.com/s/redirect",
	}, "https://mp.weixin.qq.com/s/redirect", func(ctx context.Context, url string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
		attempts++
		return &browser.BrowserBrowseResponse{
			URL:   "https://evil.example/landing",
			Title: "真实文章标题",
			Text:  "正文内容",
			Metadata: map[string]any{
				"final_url": "https://evil.example/landing",
			},
		}, "#js_content", nil
	}, platform.RetryPolicy{MaxAttempts: 3})
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrScopeViolation {
		t.Fatalf("expected %s, got %s", platform.ErrScopeViolation, re.Code)
	}
	if attempts != 1 {
		t.Fatalf("scope violation should be fatal without retry, got %d attempts", attempts)
	}
	if retryCount != 0 {
		t.Fatalf("expected retryCount=0, got %d", retryCount)
	}
}

func TestReadArticleBrowserRejectsOffDomainResponseURLWithoutMetadata(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, _, err := readArticleBrowserWithOptions(context.Background(), platform.ReadInput{
		URL: "https://mp.weixin.qq.com/s/redirect",
	}, "https://mp.weixin.qq.com/s/redirect", func(ctx context.Context, url string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, string, error) {
		attempts++
		return &browser.BrowserBrowseResponse{
			URL:   "https://evil.example/landing",
			Title: "真实文章标题",
			Text:  "正文内容",
		}, "#js_content", nil
	}, platform.RetryPolicy{MaxAttempts: 3})
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrScopeViolation {
		t.Fatalf("expected %s, got %s", platform.ErrScopeViolation, re.Code)
	}
	if attempts != 1 {
		t.Fatalf("scope violation should be fatal without retry, got %d attempts", attempts)
	}
}
