package toutiao

import (
	"errors"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestNormalizeArticleDefaultsExtractionSourceAndAuthorFromText(t *testing.T) {
	content := normalizeArticle(platform.ReadInput{}, &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/group/123/",
		CanonicalURL: "https://www.toutiao.com/group/123/",
		PlatformID:   "123",
	}, &browser.BrowserBrowseResponse{
		URL:      "https://www.toutiao.com/group/123/",
		Title:    "真实标题",
		Text:     "来源：环球网\n\n正文内容",
		Markdown: "来源：环球网\n\n正文内容",
	})

	if got := platform.MetaString(content.PlatformMetadata, "extraction_source"); got != "dom" {
		t.Fatalf("expected dom extraction source, got %q meta=%#v", got, content.PlatformMetadata)
	}
	if content.Author == nil || content.Author.Name != "环球网" {
		t.Fatalf("expected author from source line, got %#v", content.Author)
	}
}

func TestNormalizeArticleKeepsLongerBrowserText(t *testing.T) {
	content := normalizeArticle(platform.ReadInput{}, &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/group/123/",
		CanonicalURL: "https://www.toutiao.com/group/123/",
		PlatformID:   "123",
	}, &browser.BrowserBrowseResponse{
		URL:  "https://www.toutiao.com/group/123/",
		Text: "第一段正文\n\n第二段正文\n\n第三段正文",
		Metadata: map[string]any{
			"title": "真实标题",
			"text":  "来源：环球网",
		},
	})

	if content.Text != "第一段正文\n\n第二段正文\n\n第三段正文" {
		t.Fatalf("expected longer browser text, got %q", content.Text)
	}
}

func TestNormalizeArticleBackfillsTextFromLongMarkdown(t *testing.T) {
	markdown := strings.Join([]string{
		"文章标题",
		"作者 2026-07-03",
		"第一段正文内容足够长，用于模拟浏览器 Markdown 已经拿到完整正文，但 Text 只有标题元信息的情况。",
		"第二段正文继续补充内容，确保纯文本长度明显超过短 Text，从而触发 Markdown 派生文本兜底。",
		"第三段正文包含 **强调文本** 和 [链接文本](https://example.com)，派生后应该保留可读文字。",
		"第四段正文继续增加长度，避免短摘要被误判为完整正文。",
		"第五段正文收尾，验证文章正文不会因为 metadata text 过短而在 JSON text 字段里缺失。",
	}, "\n\n")
	content := normalizeArticle(platform.ReadInput{}, &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/group/123/",
		CanonicalURL: "https://www.toutiao.com/group/123/",
		PlatformID:   "123",
	}, &browser.BrowserBrowseResponse{
		URL:      "https://www.toutiao.com/group/123/",
		Title:    "文章标题",
		Text:     "文章标题\n作者\n打开客户端发表评论",
		Markdown: markdown,
	})

	if len([]rune(content.Text)) <= len([]rune("文章标题\n作者\n打开客户端发表评论"))*2 {
		t.Fatalf("expected text backfilled from markdown, got %q", content.Text)
	}
	if !strings.Contains(content.Text, "链接文本") || strings.Contains(content.Text, "https://example.com") {
		t.Fatalf("expected readable markdown-derived text, got %q", content.Text)
	}
}

func TestValidateArticleContentRejectsVideoLikePage(t *testing.T) {
	content := &platform.Content{
		Platform:     PlatformName,
		ContentType:  ContentType,
		SourceURL:    "https://www.toutiao.com/group/123/",
		CanonicalURL: "https://www.toutiao.com/group/123/",
		Title:        "视频标题",
		Text:         "正文",
		PlatformMetadata: map[string]any{
			"detail_schema": "sslocal://awemevideo?group_id=123",
		},
	}

	err := validateArticleContent(content)
	if err == nil {
		t.Fatal("expected content type mismatch")
	}
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrContentTypeMismatch {
		t.Fatalf("expected content type mismatch read error, got %v", err)
	}
}
