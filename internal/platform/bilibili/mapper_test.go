package bilibili

import (
	"strings"
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
	if content.Title != "B站测试视频" || !strings.Contains(content.Text, "简介: 视频简介") || content.Author == nil || content.Author.Name != "UP主" {
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

func TestAPIContentNeedsBrowserVideoCaptureWhenMediaRequested(t *testing.T) {
	content, err := mapAPIResponse(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL:  "https://www.bilibili.com/video/BV1xx411c7mD",
		PlatformID:   "BV1xx411c7mD",
		ContentKind:  "bv",
		CanonicalURL: "https://www.bilibili.com/video/BV1xx411c7mD",
	}, &viewResponse{Data: viewData{BVID: "BV1xx411c7mD", Title: "B站测试视频", Pic: "https://i0.hdslb.com/cover.jpg"}})
	if err != nil {
		t.Fatal(err)
	}
	if !needsBrowserVideoCapture(platform.ReadInput{IncludeMedia: true}, content) {
		t.Fatalf("cover-only API media must request browser video capture: %+v", content.Media)
	}
	content.Media = append(content.Media, platform.MediaItem{Type: "video", URL: "https://cdn.example/video.mp4"})
	if needsBrowserVideoCapture(platform.ReadInput{IncludeMedia: true}, content) {
		t.Fatal("a resolved video reference must not force browser capture")
	}
	if needsBrowserVideoCapture(platform.ReadInput{}, content) {
		t.Fatal("a caller that did not request media must keep API-first behavior")
	}
}

func TestBrowserVideoCaptureKeepsAPIContentAsSourceOfTruth(t *testing.T) {
	apiContent := &platform.Content{
		Title:    "API title",
		Text:     "API body",
		Markdown: "API markdown",
		Media:    []platform.MediaItem{{Type: "image", URL: "https://i0.hdslb.com/cover.jpg"}},
	}
	loginShell := &platform.Content{
		Title:    "登录 - 哔哩哔哩",
		Text:     "请登录",
		Markdown: "请登录",
		Media:    []platform.MediaItem{{Type: "image", URL: "https://i0.hdslb.com/login.jpg"}},
	}
	appendBrowserVideoMedia(apiContent, loginShell)
	if apiContent.Title != "API title" || apiContent.Text != "API body" || apiContent.Markdown != "API markdown" || len(apiContent.Media) != 1 {
		t.Fatalf("browser shell must not replace API content: %+v", apiContent)
	}

	videoCapture := &platform.Content{
		Title: "browser title must not replace API",
		Media: []platform.MediaItem{
			{Type: "image", URL: "https://i0.hdslb.com/browser.jpg"},
			{Type: "video", URL: "https://media.example/video.mp4"},
		},
	}
	appendBrowserVideoMedia(apiContent, videoCapture)
	if apiContent.Title != "API title" || apiContent.Text != "API body" || len(apiContent.Media) != 2 || apiContent.Media[1].URL != "https://media.example/video.mp4" {
		t.Fatalf("browser capture must only enrich API content with video: %+v", apiContent)
	}
}

func TestBuildSummaryInputKeepsTitleWhenDescriptionIsThin(t *testing.T) {
	t.Parallel()

	text := buildSummaryInput(viewData{
		Title: "字幕君交流场所",
		Desc:  "www",
		Owner: viewOwner{Name: "UP主"},
		Stat:  viewStat{View: 100, Like: 10},
	})
	if !strings.Contains(text, "标题: 字幕君交流场所") || !strings.Contains(text, "UP主: UP主") {
		t.Fatalf("summary input lost useful context: %q", text)
	}
}
