package douyin

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/originaleric/diggate/internal/platform"
)

func TestMapDetailResponse(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/detail_ok.json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	norm := &platform.NormalizedURL{
		OriginalURL:  "https://www.douyin.com/video/123",
		PlatformID:   "123",
		ContentKind:  "video",
		CanonicalURL: "https://www.douyin.com/video/123",
	}
	content, err := MapDetailResponse(platform.ReadInput{IncludeMedia: true}, norm, parsed)
	if err != nil {
		t.Fatal(err)
	}
	if content.Title != "测试视频标题" {
		t.Fatalf("title: %s", content.Title)
	}
	if content.Text != "测试视频描述" {
		t.Fatalf("text: %s", content.Text)
	}
	if platform.MetaString(content.PlatformMetadata, "extraction_source") != "api" {
		t.Fatalf("source: %v", content.PlatformMetadata)
	}
}
