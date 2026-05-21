package xiaohongshu

import (
	"context"
	"testing"
)

func TestNormalizeURLExplore(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.xiaohongshu.com/explore/662f8b3a000000001e031234")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "662f8b3a000000001e031234" {
		t.Fatalf("platform id: %s", n.PlatformID)
	}
	want := "https://www.xiaohongshu.com/explore/662f8b3a000000001e031234"
	if n.CanonicalURL != want {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
	if n.CookieDomain != CookieDomain {
		t.Fatalf("cookie domain: %s", n.CookieDomain)
	}
}

func TestNormalizeURLDiscovery(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.xiaohongshu.com/discovery/item/abc123def4567890")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "abc123def4567890" {
		t.Fatalf("platform id: %s", n.PlatformID)
	}
}

func TestNormalizeURLRejectForeign(t *testing.T) {
	t.Parallel()
	_, err := NormalizeURL(context.Background(), "https://example.com/note/1")
	if err == nil {
		t.Fatal("expected error")
	}
}
