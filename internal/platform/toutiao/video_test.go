package toutiao

import (
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestValidateVideoContentRequiresVideoMediaWhenRequested(t *testing.T) {
	t.Parallel()
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      VideoContentType,
		Title:            "video title",
		Text:             "video description",
		Media:            []platform.MediaItem{{Type: "image", URL: "https://example.com/cover.jpg"}},
		PlatformMetadata: map[string]any{},
	}
	err := validateVideoContent(content, true)
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrPartialContent {
		t.Fatalf("expected partial content when video media is missing, got %v", err)
	}
}

func TestValidateVideoContentAllowsMetadataOnlyRead(t *testing.T) {
	t.Parallel()
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      VideoContentType,
		Title:            "video title",
		Text:             "video description",
		PlatformMetadata: map[string]any{},
	}
	if err := validateVideoContent(content, false); err != nil {
		t.Fatalf("metadata-only video read should be allowed: %v", err)
	}
}

func TestNormalizeVideoPrefersBrowserMediaResources(t *testing.T) {
	t.Parallel()
	content := normalizeVideo(platform.ReadInput{IncludeMedia: true}, &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/video/123/",
		CanonicalURL: "https://www.toutiao.com/video/123/",
	}, &browser.BrowserBrowseResponse{
		Metadata:  map[string]any{"video_urls": []string{"https://old.example/video.mp4"}},
		MediaURLs: []string{"https://cdn.example/video.mp4"},
	})
	if !hasVideoMedia(content.Media) || content.Media[len(content.Media)-1].URL != "https://cdn.example/video.mp4" {
		t.Fatalf("expected browser media resource, got %#v", content.Media)
	}
	if got := platform.MetaString(content.PlatformMetadata, "media_extraction_source"); got != "browser_resource" {
		t.Fatalf("media extraction source = %q", got)
	}
}

func TestCleanVideoPublishedAtRejectsPlayerClock(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"00:04 / 01:27 直播", "00:04 / 01:27", "01:27", "直播"} {
		if got := cleanVideoPublishedAt(raw); got != "" {
			t.Fatalf("player clock %q should not be accepted as published_at, got %q", raw, got)
		}
	}
	if got := cleanVideoPublishedAt("2026-07-30T09:00:00+08:00"); got == "" {
		t.Fatal("real published_at should be preserved")
	}
}
