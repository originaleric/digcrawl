package weibo

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestMapAPIResponse(t *testing.T) {
	t.Parallel()
	content, err := mapAPIResponse(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL:  "https://m.weibo.cn/detail/502233445566",
		PlatformID:   "502233445566",
		ContentKind:  "post",
		CanonicalURL: "https://m.weibo.cn/detail/502233445566",
	}, &statusResponse{
		OK: 1,
		statusPayload: statusPayload{
			ID:            "502233445566",
			MID:           "NabcDEF",
			Text:          "<p>微博正文&nbsp;内容</p>",
			CreatedAt:     "Wed Jul 29 09:00:00 +0800 2026",
			User:          statusUser{ID: 123456, ScreenName: "博主", ProfileURL: "/u/123456", ProfileImageURL: "https://wx1.sinaimg.cn/avatar.jpg"},
			Pics:          []statusPic{{URL: "https://wx1.sinaimg.cn/pic.jpg"}},
			PageInfo:      statusPageInfo{MediaURL: "https://video.weibo.com/show?fid=abc", PagePic: "https://wx1.sinaimg.cn/cover.jpg"},
			RepostsCount:  1,
			CommentsCount: 2,
			Attitudes:     3,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.Text != "微博正文 内容" || content.Author == nil || content.Author.Name != "博主" {
		t.Fatalf("unexpected content: %+v", content)
	}
	if content.Engagement == nil || content.Engagement.Likes != 3 || content.Engagement.Comments != 2 || content.Engagement.Reposts != 1 {
		t.Fatalf("unexpected engagement: %+v", content.Engagement)
	}
	if len(content.Media) != 3 {
		t.Fatalf("expected image, video and cover media, got %+v", content.Media)
	}
	if got := platform.MetaString(content.PlatformMetadata, "extraction_source"); got != "api" {
		t.Fatalf("expected api extraction source, got %q", got)
	}
}
