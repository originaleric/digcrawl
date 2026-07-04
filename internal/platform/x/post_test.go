package x

import (
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestValidatePostContentRejectsDeletedShell(t *testing.T) {
	content := &platform.Content{
		Platform:    PlatformName,
		ContentType: ContentType,
		Title:       "Post / X",
		Text:        "Post\nHmm...this page doesn’t exist. Try searching for something else.\nSearch",
		Markdown:    "We're unable to show this content\nThe content may be private, deleted or only available on the app.",
	}
	err := validatePostContent(content)
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if readErr.Code != platform.ErrContentNotFound {
		t.Fatalf("code=%s", readErr.Code)
	}
}
