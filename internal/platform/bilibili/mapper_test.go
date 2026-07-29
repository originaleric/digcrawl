package bilibili

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestMapAPIResponse(t *testing.T) {
	t.Parallel()
	content, err := mapAPIResponse(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL:  "https://www.bilibili.com/video/BV1xx411c7mD",
		PlatformID:   "BV1xx411c7mD",
		ContentKind:  "bv",
		CanonicalURL: "https://www.bilibili.com/video/BV1xx411c7mD",
	}, &viewResponse{
		Code: 0,
		Data: viewData{
			BVID:    "BV1xx411c7mD",
			AID:     170001,
			CID:     880001,
			Title:   "B站测试视频",
			Desc:    "视频简介",
			Pic:     "https://i0.hdslb.com/cover.jpg",
			PubDate: 1785231600,
			Owner:   viewOwner{MID: 123456, Name: "UP主", Face: "https://i0.hdslb.com/avatar.jpg"},
			Stat:    viewStat{Like: 10, Reply: 2, Share: 3, Favorite: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.Title != "B站测试视频" || content.Text != "视频简介" || content.Author == nil || content.Author.Name != "UP主" {
		t.Fatalf("unexpected content: %+v", content)
	}
	if content.Engagement == nil || content.Engagement.Likes != 10 || content.Engagement.Bookmarks != 4 {
		t.Fatalf("unexpected engagement: %+v", content.Engagement)
	}
	if len(content.Media) != 1 || content.Media[0].URL == "" {
		t.Fatalf("expected cover media, got %+v", content.Media)
	}
	if got := platform.MetaString(content.PlatformMetadata, "extraction_source"); got != "api" {
		t.Fatalf("expected api extraction source, got %q", got)
	}
}
