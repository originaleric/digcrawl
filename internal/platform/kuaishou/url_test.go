package kuaishou

import (
	"context"
	"testing"
)

func TestNormalizeKuaishouVideoURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://www.kuaishou.com/short-video/3xf6abc")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "3xf6abc" || n.ContentKind != "video" || n.CanonicalURL != "https://www.kuaishou.com/short-video/3xf6abc" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}

func TestNormalizeKuaishouUserURL(t *testing.T) {
	n, err := NormalizeURL(context.Background(), "https://www.kuaishou.com/profile/3xuser")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "3xuser" || n.ContentKind != "user" || n.CanonicalURL != "https://www.kuaishou.com/profile/3xuser" {
		t.Fatalf("unexpected normalized url: %+v", n)
	}
}
