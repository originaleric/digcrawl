package x

import (
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestMapSyndicationResponseSelectsBestMP4AndKeepsImages(t *testing.T) {
	content, err := MapSyndicationResponse(platform.ReadInput{URL: "https://x.com/u/status/42", IncludeMedia: true}, &platform.NormalizedURL{PlatformID: "42", CanonicalURL: "https://x.com/u/status/42"}, &syndicationTweetResponse{
		IDStr: "42", Text: "hello", User: syndicationUser{Name: "User", ScreenName: "u"},
		MediaDetails: []syndicationMedia{
			{Type: "video", MediaURLHTTPS: "https://img.example/cover.jpg", VideoInfo: syndicationVideoInfo{Variants: []syndicationVideoVariant{
				{URL: "https://video.example/hls.m3u8", ContentType: "application/x-mpegURL"},
				{URL: "https://video.example/low.mp4", ContentType: "video/mp4", Bitrate: 100},
				{URL: "https://video.example/high.mp4", ContentType: "video/mp4", Bitrate: 200},
			}}},
			{Type: "photo", MediaURLHTTPS: "https://img.example/photo.jpg"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(content.Media) != 3 || content.Media[0].Type != "video" || content.Media[0].URL != "https://video.example/high.mp4" {
		t.Fatalf("media=%+v", content.Media)
	}
	if content.Media[1].Type != "image" || content.Media[2].Type != "image" {
		t.Fatalf("images=%+v", content.Media)
	}
	if content.PlatformMetadata["extraction_source"] != "syndication" {
		t.Fatalf("metadata=%+v", content.PlatformMetadata)
	}
}

func TestMapSyndicationResponseWithHLSOnlyKeepsPost(t *testing.T) {
	content, err := MapSyndicationResponse(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{PlatformID: "42"}, &syndicationTweetResponse{IDStr: "42", Text: "hello", MediaDetails: []syndicationMedia{{Type: "video", VideoInfo: syndicationVideoInfo{Variants: []syndicationVideoVariant{{URL: "https://video.example/a.m3u8", ContentType: "application/x-mpegURL"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(content.Media) != 0 || content.PlatformMetadata["video_variant_unavailable"] != 1 {
		t.Fatalf("content=%+v", content)
	}
}

func TestMapSyndicationResponseKeepsMediaOnlyPost(t *testing.T) {
	content, err := MapSyndicationResponse(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{PlatformID: "42"}, &syndicationTweetResponse{
		IDStr: "42",
		MediaDetails: []syndicationMedia{{
			Type:      "video",
			VideoInfo: syndicationVideoInfo{Variants: []syndicationVideoVariant{{URL: "https://video.example/video.mp4", ContentType: "video/mp4"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.Text != "" || len(content.Media) != 1 || content.Media[0].URL != "https://video.example/video.mp4" {
		t.Fatalf("content=%+v", content)
	}
}

func TestMapSyndicationResponseRejectsEmptyPayload(t *testing.T) {
	if _, err := MapSyndicationResponse(platform.ReadInput{}, &platform.NormalizedURL{PlatformID: "42"}, &syndicationTweetResponse{IDStr: "42"}); err == nil {
		t.Fatal("expected an empty syndication payload to be rejected")
	}
}

func TestMapSyndicationResponseRejectsUnusableMediaOnlyPayload(t *testing.T) {
	if _, err := MapSyndicationResponse(platform.ReadInput{}, &platform.NormalizedURL{PlatformID: "42"}, &syndicationTweetResponse{IDStr: "42", MediaDetails: []syndicationMedia{{Type: "unknown"}}}); err == nil {
		t.Fatal("expected media-only payload without a usable media mapping to be rejected")
	}
}

func TestMapSyndicationResponseRejectsMismatchedPostID(t *testing.T) {
	_, err := MapSyndicationResponse(platform.ReadInput{}, &platform.NormalizedURL{PlatformID: "42"}, &syndicationTweetResponse{IDStr: "43", Text: "wrong post"})
	if err == nil {
		t.Fatal("expected mismatched syndication post ID to be rejected")
	}
}
