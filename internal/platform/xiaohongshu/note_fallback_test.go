package xiaohongshu

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestScreenshotFallbackOnlyForContentFailures(t *testing.T) {
	cases := []struct {
		name    string
		err     *browser.BrowseError
		text    string
		allowed bool
		success bool
	}{
		{"queue", &browser.BrowseError{Stage: "acquire", Cause: context.DeadlineExceeded, Diagnostics: map[string]any{"operation": "send_queue"}}, "text", false, false},
		{"connection", &browser.BrowseError{Stage: "navigate", Cause: io.EOF}, "text", false, false},
		{"budget", &browser.BrowseError{Stage: "wait_selector", Cause: context.DeadlineExceeded, Diagnostics: map[string]any{"reason": "budget_exhausted"}}, "text", false, false},
		{"selector screenshot only", &browser.BrowseError{Stage: "wait_selector", Cause: context.DeadlineExceeded}, "", true, false},
		{"selector recovered metadata", &browser.BrowseError{Stage: "wait_selector", Cause: context.DeadlineExceeded}, "usable note text", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shots := 0
			content, err := readNoteBrowserWithBrowse(context.Background(), platform.ReadInput{IncludeScreenshot: true, WaitSelector: ".stale"}, &platform.NormalizedURL{ResolvedURL: "https://www.xiaohongshu.com/explore/abc"}, "", nil,
				func(context.Context, string, platform.ReadInput, platform.BrowseSpec, []platform.BrowseAttempt) (*browser.BrowserBrowseResponse, error) {
					return nil, tc.err
				},
				func(_ context.Context, _ string, in platform.ReadInput, spec platform.BrowseSpec) (*browser.BrowserBrowseResponse, error) {
					shots++
					if in.WaitSelector != "" || spec.WaitSelector != "" {
						t.Fatal("screenshot reused failed selector")
					}
					return &browser.BrowserBrowseResponse{ScreenshotBase: "image", Metadata: map[string]any{"text": tc.text}}, nil
				})
			if (shots == 1) != tc.allowed || (err == nil) != tc.success {
				t.Fatalf("shots=%d content=%+v err=%v", shots, content, err)
			}
			if tc.success {
				if platform.MetaErrorCode(content.PlatformMetadata) != platform.ErrPartialContent {
					t.Fatal("partial read presented as complete")
				}
			} else if !errors.Is(err, tc.err) {
				t.Fatalf("lost browser error: %v", err)
			}
		})
	}
}
func TestScreenshotFallbackPropagatesItsInfrastructureFailure(t *testing.T) {
	original := &browser.BrowseError{Stage: "wait_selector", Cause: context.DeadlineExceeded}
	failure := &browser.BrowseError{Stage: "acquire", Cause: io.EOF}
	_, err := readNoteBrowserWithBrowse(context.Background(), platform.ReadInput{IncludeScreenshot: true}, &platform.NormalizedURL{ResolvedURL: "https://www.xiaohongshu.com/explore/abc"}, "", nil,
		func(context.Context, string, platform.ReadInput, platform.BrowseSpec, []platform.BrowseAttempt) (*browser.BrowserBrowseResponse, error) {
			return nil, original
		},
		func(context.Context, string, platform.ReadInput, platform.BrowseSpec) (*browser.BrowserBrowseResponse, error) {
			return nil, failure
		})
	if !errors.Is(err, failure) {
		t.Fatalf("lost screenshot failure: %v", err)
	}
}

func TestScreenshotFallbackCannotHideLoginRequirement(t *testing.T) {
	_, err := readNoteBrowserWithBrowse(context.Background(), platform.ReadInput{IncludeScreenshot: true}, &platform.NormalizedURL{ResolvedURL: "https://www.xiaohongshu.com/explore/abc"}, "", nil,
		func(context.Context, string, platform.ReadInput, platform.BrowseSpec, []platform.BrowseAttempt) (*browser.BrowserBrowseResponse, error) {
			return nil, &browser.BrowseError{Stage: "wait_selector", Cause: context.DeadlineExceeded}
		},
		func(context.Context, string, platform.ReadInput, platform.BrowseSpec) (*browser.BrowserBrowseResponse, error) {
			return &browser.BrowserBrowseResponse{Metadata: map[string]any{"text": "login prompt", "error_code": platform.ErrLoginRequired, "error_message": "login required"}}, nil
		})
	var re *platform.ReadError
	if !errors.As(err, &re) || re.Code != platform.ErrLoginRequired {
		t.Fatalf("login became partial success: %v", err)
	}
}
