package youtube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestNormalizeVideoUsesDescriptionAndCoverWithoutPlaybackURL(t *testing.T) {
	t.Parallel()
	content := normalizeVideo(context.Background(), platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL: "https://youtu.be/dQw4w9WgXcQ", CanonicalURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", PlatformID: "dQw4w9WgXcQ",
	}, &browser.BrowserBrowseResponse{Metadata: map[string]any{
		"title": "Video", "description": "Description", "author_name": "Channel", "cover_url": "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg",
	}})
	if content.Title != "Video" || content.Text != "Description" || content.Author == nil || content.Author.Name != "Channel" {
		t.Fatalf("unexpected content: %+v", content)
	}
	if len(content.Media) != 1 || content.Media[0].Type != "image" {
		t.Fatalf("media=%+v", content.Media)
	}
	if _, ok := content.PlatformMetadata["video_urls"]; ok {
		t.Fatalf("provider must not expose playback URLs: %+v", content.PlatformMetadata)
	}
}

func TestNormalizeVideoDoesNotUseUnboundedPageCaptionResult(t *testing.T) {
	t.Parallel()
	content := normalizeVideo(context.Background(), platform.ReadInput{}, &platform.NormalizedURL{
		OriginalURL: "https://youtu.be/dQw4w9WgXcQ", CanonicalURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", PlatformID: "dQw4w9WgXcQ",
	}, &browser.BrowserBrowseResponse{Metadata: map[string]any{
		"title": "Video", "description": "Description", "caption_text": "Caption one\nCaption two", "caption_source": "youtube_page_fetch",
	}})
	if content.Text != "Description" {
		t.Fatalf("unexpected caption result: %+v", content)
	}
	if _, ok := content.PlatformMetadata["caption_text"]; ok {
		t.Fatalf("caption text must not be duplicated in metadata: %+v", content.PlatformMetadata)
	}
	if _, ok := content.PlatformMetadata["caption_source"]; ok {
		t.Fatalf("page caption source must not survive without a bounded caption read: %+v", content.PlatformMetadata)
	}
}

func TestValidateVideoResponseRequiresMatchingPlayerID(t *testing.T) {
	t.Parallel()
	norm := &platform.NormalizedURL{PlatformID: "dQw4w9WgXcQ"}
	tests := []struct {
		name string
		meta map[string]any
		code string
	}{
		{name: "missing player response", meta: map[string]any{"title": "loading"}, code: platform.ErrPartialContent},
		{name: "different video", meta: map[string]any{"video_id": "aaaaaaaaaaa"}, code: platform.ErrContentNotFound},
		{name: "login required", meta: map[string]any{"error_code": platform.ErrLoginRequired}, code: platform.ErrLoginRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVideoResponse(norm, &browser.BrowserBrowseResponse{Metadata: tt.meta})
			var readErr *platform.ReadError
			if !errors.As(err, &readErr) || readErr.Code != tt.code {
				t.Fatalf("error=%v, want code %s", err, tt.code)
			}
		})
	}
	if err := validateVideoResponse(norm, &browser.BrowserBrowseResponse{Metadata: map[string]any{"video_id": norm.PlatformID}}); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataScriptDoesNotFetchCaptionsOrAcceptBodyAsReady(t *testing.T) {
	t.Parallel()
	if strings.Contains(DefaultWaitSelector, "body") {
		t.Fatalf("wait selector must not treat document body as a ready video page: %q", DefaultWaitSelector)
	}
	script := MetadataScript()
	if strings.Contains(script, "fetch(track.base_url") || strings.Contains(script, "AbortController") {
		t.Fatal("caption fetch must be owned by the Go-side shared time budget")
	}
}

func TestNormalizeVideoDoesNotExportSignedCaptionURLs(t *testing.T) {
	t.Parallel()
	content := normalizeVideo(context.Background(), platform.ReadInput{}, &platform.NormalizedURL{
		OriginalURL: "https://youtu.be/dQw4w9WgXcQ", CanonicalURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", PlatformID: "dQw4w9WgXcQ",
	}, &browser.BrowserBrowseResponse{Metadata: map[string]any{
		"title": "Video", "caption_text": "Caption", "caption_tracks": []any{map[string]any{
			"base_url": "https://www.youtube.com/api/timedtext?v=dQw4w9WgXcQ&signature=private", "language_code": "en",
		}},
	}})
	if _, ok := content.PlatformMetadata["caption_tracks"]; ok {
		t.Fatalf("caption track URLs must not be exported: %+v", content.PlatformMetadata)
	}
	if available, ok := content.PlatformMetadata["captions_available"].(bool); !ok || !available {
		t.Fatalf("safe caption availability marker missing: %+v", content.PlatformMetadata)
	}
}
