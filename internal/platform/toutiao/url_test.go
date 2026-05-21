package toutiao

import (
	"context"
	"testing"
)

func TestNormalizeURLArticle(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.toutiao.com/article/7123456789012345678/")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7123456789012345678" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.CanonicalURL != "https://www.toutiao.com/article/7123456789012345678/" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
}

func TestNormalizeURLGroup(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.toutiao.com/group/1234567890/")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "1234567890" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.ContentKind != "group" {
		t.Fatalf("kind: %s", n.ContentKind)
	}
}

func TestNormalizeURLRejectForeign(t *testing.T) {
	t.Parallel()
	_, err := NormalizeURL(context.Background(), "https://example.com/article/1")
	if err == nil {
		t.Fatal("expected error")
	}
}
