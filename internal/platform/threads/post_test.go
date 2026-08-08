package threads

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestNormalizeContentPrefersBrowserMediaResources(t *testing.T) {
	t.Parallel()
	content := normalizeContent(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL:  "https://www.threads.com/@user/post/abc",
		CanonicalURL: "https://www.threads.com/@user/post/abc",
	}, &browser.BrowserBrowseResponse{
		Metadata:  map[string]any{"title": "post", "description": "text", "video_urls": []string{"https://old.example/video.mp4"}},
		MediaURLs: []string{"https://cdn.example/video.mp4"},
	})
	if len(content.Media) != 1 || content.Media[0].URL != "https://cdn.example/video.mp4" {
		t.Fatalf("media = %#v", content.Media)
	}
}
