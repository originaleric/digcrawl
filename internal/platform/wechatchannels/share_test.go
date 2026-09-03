package wechatchannels

import (
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestValidateShareURL(t *testing.T) {
	if err := validateShareURL("https://weixin.qq.com/sph/AKFbOmNOLl"); err != nil {
		t.Fatalf("validateShareURL() error = %v", err)
	}
	err := validateShareURL("https://weixin.qq.com/s/AKFbOmNOLl")
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrContentTypeMismatch {
		t.Fatalf("invalid share URL error = %v", err)
	}
	if err := validateShareURL("https://example.com/sph/AKFbOmNOLl"); err == nil {
		t.Fatal("non-WeChat share URL was accepted")
	}
}

func TestContentFromShareResponse(t *testing.T) {
	content, err := contentFromShareResponse(platform.ReadInput{IncludeMedia: true}, "https://weixin.qq.com/sph/AKFbOmNOLl", &browser.BrowserBrowseResponse{
		URL: "https://channels.weixin.qq.com/finder-preview/pages/sph?id=AKFbOmNOLl",
		Metadata: map[string]any{
			"title":        "公开视频标题",
			"description":  "公开视频简介",
			"author_name":  "发布者",
			"published_at": "2026年8月21日",
			"cover_url":    "https://example.com/cover.jpg",
			"video_urls":   []any{"https://example.com/video.mp4"},
		},
		MediaURLs: []string{"https://example.com/video.mp4", "https://example.com/video-2.mp4"},
	})
	if err != nil {
		t.Fatalf("contentFromShareResponse() error = %v", err)
	}
	if content.Platform != PlatformName || content.ContentType != ContentType || content.Title != "公开视频标题" || content.Author == nil || content.Author.Name != "发布者" {
		t.Fatalf("content = %#v", content)
	}
	if len(content.Media) != 3 {
		t.Fatalf("media = %#v", content.Media)
	}
}

func TestContentFromShareResponseRejectsUnexpectedFinalURL(t *testing.T) {
	_, err := contentFromShareResponse(platform.ReadInput{}, "https://weixin.qq.com/sph/AKFbOmNOLl", &browser.BrowserBrowseResponse{URL: "https://example.com/"})
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrScopeViolation {
		t.Fatalf("error = %v", err)
	}
}

func TestContentFromShareResponseReportsQRRestrictedPlayback(t *testing.T) {
	_, err := contentFromShareResponse(platform.ReadInput{IncludeMedia: true}, "https://weixin.qq.com/sph/AKFbOmNOLl", &browser.BrowserBrowseResponse{
		URL: "https://channels.weixin.qq.com/finder-preview/pages/sph?id=AKFbOmNOLl",
		Metadata: map[string]any{
			"title":               "公开视频标题",
			"playback_restricted": true,
		},
	})
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrContentPrivate {
		t.Fatalf("error = %v", err)
	}
}
