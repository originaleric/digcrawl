package toutiao

import (
	"context"
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform"
)

func TestFetchArticleViaAPIClassifiesNonJSONAsFallback(t *testing.T) {
	oldFetch := articleFetchJSON
	articleFetchJSON = func(ctx context.Context, req *browser.PageFetchRequest) (*browser.PageFetchResult, error) {
		return &browser.PageFetchResult{
			Status:      200,
			ContentType: "text/html; charset=utf-8",
			Body:        "<html><body>main app shell</body></html>",
		}, nil
	}
	defer func() { articleFetchJSON = oldFetch }()

	norm := &platform.NormalizedURL{
		OriginalURL:  "https://www.toutiao.com/group/123/",
		ResolvedURL:  "https://www.toutiao.com/group/123/",
		PlatformID:   "123",
		CanonicalURL: "https://www.toutiao.com/group/123/",
		CookieDomain: CookieDomain,
	}
	parsed, attempt, _, err := FetchArticleViaAPI(context.Background(), norm, CookieDomain)
	if parsed != nil {
		t.Fatalf("expected no parsed payload, got %#v", parsed)
	}
	var fallback *platform.APIFallbackError
	if !errors.As(err, &fallback) {
		t.Fatalf("expected API fallback error, got %v", err)
	}
	if attempt == nil || !attempt.FallbackBrowser {
		t.Fatalf("expected fallback attempt, got %#v", attempt)
	}
	if got := platform.MetaString(attempt.Meta, "api_response_type"); got != "html" {
		t.Fatalf("expected html response type, got %q meta=%#v", got, attempt.Meta)
	}
	if got := platform.MetaString(attempt.Meta, "api_body_prefix"); got == "" {
		t.Fatalf("expected body prefix in metadata: %#v", attempt.Meta)
	}
}
