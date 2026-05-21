package toutiao

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/originaleric/diggate/internal/platform"
)

func TestMapAPIResponse(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/api_ok.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	norm := &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/article/123/",
		PlatformID:   "123",
		CanonicalURL: "https://www.toutiao.com/article/123/",
	}
	content, err := MapAPIResponse(platform.ReadInput{IncludeMedia: true}, norm, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if content.Title != "头条测试标题" {
		t.Fatalf("title: %s", content.Title)
	}
	if content.Text != "头条测试正文内容" {
		t.Fatalf("text: %s", content.Text)
	}
}

func TestNormalizeURLWPath(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.toutiao.com/w/99887766/")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "99887766" {
		t.Fatalf("id: %s", n.PlatformID)
	}
}
