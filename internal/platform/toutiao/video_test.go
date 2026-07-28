package toutiao

import (
	"errors"
	"testing"

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
