package kuaishou

import (
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestValidateVideoContentRejectsEmptyShell(t *testing.T) {
	t.Parallel()

	err := validateVideoContent(&platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		PlatformMetadata: map[string]any{"extraction_source": "browser"},
	})
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrPartialContent {
		t.Fatalf("expected PARTIAL_CONTENT, got %+v", re)
	}
}

func TestClassifyBrowseErrorConvertsTimeout(t *testing.T) {
	t.Parallel()

	err := classifyBrowseError(contextDeadlineExceededForTest{})
	var re *platform.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("expected ReadError, got %v", err)
	}
	if re.Code != platform.ErrPartialContent {
		t.Fatalf("expected PARTIAL_CONTENT, got %+v", re)
	}
}

type contextDeadlineExceededForTest struct{}

func (contextDeadlineExceededForTest) Error() string { return "context deadline exceeded" }
