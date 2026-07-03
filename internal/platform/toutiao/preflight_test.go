package toutiao

import (
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
)

func TestClassifyArticlePreflightRejectsIxiguaFinalURL(t *testing.T) {
	err := classifyArticlePreflight("https://www.ixigua.com/6980221806720483877", httpStatusOK, "text/html", "")
	if err == nil {
		t.Fatal("expected content type mismatch")
	}
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrContentTypeMismatch {
		t.Fatalf("expected content type mismatch read error, got %v", err)
	}
	if got := platform.MetaString(readErr.Meta, "preflight_final_url"); got != "https://www.ixigua.com/6980221806720483877" {
		t.Fatalf("expected final url metadata, got %q", got)
	}
}

func TestClassifyArticlePreflightAllowsToutiaoArticleURL(t *testing.T) {
	if err := classifyArticlePreflight("https://www.toutiao.com/group/123/", httpStatusOK, "text/html", "<html></html>"); err != nil {
		t.Fatalf("expected no preflight error, got %v", err)
	}
}

const httpStatusOK = 200
