package xiaohongshu

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestNormalizeNoteWithMetadata(t *testing.T) {
	t.Parallel()
	norm := &platform.NormalizedURL{
		OriginalURL:  "https://www.xiaohongshu.com/explore/abc",
		CanonicalURL: "https://www.xiaohongshu.com/explore/abc",
		PlatformID:   "abc",
	}
	resp := &browser.BrowserBrowseResponse{
		URL:   norm.CanonicalURL,
		Title: "page title",
		Text:  "body",
		Metadata: map[string]any{
			"title":        "meta title",
			"text":         "meta desc",
			"author_name":  "alice",
			"likes":        float64(12000),
			"comments":     88,
			"bookmarks":    42,
			"image_urls":   []any{"https://img.example/a.jpg"},
			"video_url":    "https://video.example/v.mp4",
			"content_kind": "video",
			"tags":         []any{"tag1"},
		},
	}
	c := normalizeNote(platform.ReadInput{IncludeMedia: true}, norm, resp)
	if c.Title != "meta title" {
		t.Fatalf("title: %s", c.Title)
	}
	if c.Text != "meta desc" {
		t.Fatalf("text: %s", c.Text)
	}
	if c.Author == nil || c.Author.Name != "alice" {
		t.Fatalf("author: %+v", c.Author)
	}
	if c.Engagement == nil || c.Engagement.Likes != 12000 {
		t.Fatalf("engagement: %+v", c.Engagement)
	}
	if c.ContentType != "video" {
		t.Fatalf("content type: %s", c.ContentType)
	}
	if len(c.Media) < 2 {
		t.Fatalf("media: %+v", c.Media)
	}
}
