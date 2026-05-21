package xiaohongshu

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/originaleric/diggate/internal/platform"
)

func TestMapFeedResponse(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/feed_ok.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	norm := &platform.NormalizedURL{
		OriginalURL:  "https://www.xiaohongshu.com/explore/abc",
		PlatformID:   "abc",
		CanonicalURL: "https://www.xiaohongshu.com/explore/abc",
	}
	content, err := MapFeedResponse(platform.ReadInput{IncludeMedia: true}, norm, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if content.Title != "测试笔记标题" {
		t.Fatalf("title: %s", content.Title)
	}
	if content.Text != "测试笔记正文" {
		t.Fatalf("text: %s", content.Text)
	}
	if platform.MetaString(content.PlatformMetadata, "extraction_source") != "api" {
		t.Fatalf("source: %v", content.PlatformMetadata)
	}
	if content.Author == nil || content.Author.Name != "测试作者" {
		t.Fatalf("author: %v", content.Author)
	}
}

func TestClassifyFeedEmpty(t *testing.T) {
	t.Parallel()
	attempt := platform.ClassifyAPIResponse(PlatformName, 200, map[string]any{"items": []any{}}, xhsFeedEmpty)
	if attempt.OK || !attempt.FallbackBrowser {
		t.Fatalf("expected fallback: %+v", attempt)
	}
}
