package douyin

import (
	"context"
	"testing"
)

func TestNormalizeURLVideo(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.douyin.com/video/7123456789012345678")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7123456789012345678" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.CanonicalURL != "https://www.douyin.com/video/7123456789012345678" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
}

func TestNormalizeURLShare(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.iesdouyin.com/share/video/999")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "999" {
		t.Fatalf("id: %s", n.PlatformID)
	}
}
