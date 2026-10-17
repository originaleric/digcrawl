package youtube

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestReadVideoSelectsSavedYouTubeSession(t *testing.T) {
	t.Parallel()
	for _, override := range []string{"", "youtube.com"} {
		t.Run("cookie_domain="+override, func(t *testing.T) {
			calls := 0
			content, err := readVideoWithBrowse(context.Background(), platform.ReadInput{
				URL: "https://youtu.be/dQw4w9WgXcQ", UseCookieDomain: override,
			}, func(_ context.Context, rawURL string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, error) {
				calls++
				if rawURL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" || spec.CookieDomain != "www.youtube.com" {
					t.Fatalf("reader did not select the saved YouTube session: url=%s domain=%s", rawURL, spec.CookieDomain)
				}
				if in.UseCookieDomain != override {
					t.Fatalf("explicit cookie-domain override was lost: %q", in.UseCookieDomain)
				}
				return &browser.BrowserBrowseResponse{URL: rawURL, Metadata: map[string]any{
					"video_id": "dQw4w9WgXcQ", "title": "Authenticated video", "description": "Retrieved description",
				}}, nil
			})
			if err != nil || calls != 1 || content == nil || content.Text != "Retrieved description" {
				t.Fatalf("read result: calls=%d content=%+v err=%v", calls, content, err)
			}
		})
	}
}

func TestReadVideoPreservesLoginRequiredWithoutRetrying(t *testing.T) {
	t.Parallel()
	calls := 0
	content, err := readVideoWithBrowse(context.Background(), platform.ReadInput{URL: "https://youtu.be/dQw4w9WgXcQ"},
		func(context.Context, string, platform.ReadInput, platform.BrowseSpec) (*browser.BrowserBrowseResponse, error) {
			calls++
			return &browser.BrowserBrowseResponse{Metadata: map[string]any{
				"error_code": platform.ErrLoginRequired, "error_message": "Sign in to confirm you’re not a bot",
			}}, nil
		})
	var readErr *platform.ReadError
	if content != nil || !errors.As(err, &readErr) || readErr.Code != platform.ErrLoginRequired || calls != 1 {
		t.Fatalf("login failure must remain explicit without replay: calls=%d content=%+v err=%v", calls, content, err)
	}
}

func TestNormalizeVideoUsesDescriptionAndCoverWithoutPlaybackURL(t *testing.T) {
	t.Parallel()
	content := normalizeVideo(context.Background(), platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL: "https://youtu.be/dQw4w9WgXcQ", CanonicalURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", PlatformID: "dQw4w9WgXcQ",
	}, &browser.BrowserBrowseResponse{Metadata: map[string]any{
		"title": "Video", "description": "Description", "author_name": "Channel", "cover_url": "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg?sqp=forced-avif",
	}})
	if content.Title != "Video" || content.Text != "Description" || content.Author == nil || content.Author.Name != "Channel" {
		t.Fatalf("unexpected content: %+v", content)
	}
	if len(content.Media) != 1 || content.Media[0].Type != "image" {
		t.Fatalf("media=%+v", content.Media)
	}
	if got, want := content.Media[0].URL, "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg"; got != want {
		t.Fatalf("thumbnail URL=%q want stable endpoint %q", got, want)
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
