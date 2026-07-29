package bilibili

import (
	"context"
	"testing"
)

func TestNormalizeVideoBVURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD/?spm_id_from=333")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "BV1xx411c7mD" || n.ContentKind != "bv" || n.CanonicalURL != "https://www.bilibili.com/video/BV1xx411c7mD" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}

func TestNormalizeSpaceURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://space.bilibili.com/123456/video")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "123456" || n.ContentKind != "user" || n.CanonicalURL != "https://space.bilibili.com/123456" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}
