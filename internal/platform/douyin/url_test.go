package douyin

import (
	"context"
	"strings"
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

func TestNormalizeURLModalID(t *testing.T) {
	t.Parallel()
	raw := "https://www.douyin.com/user/MS4wLjABAAAAtest?modal_id=7123456789012345678"
	n, err := NormalizeURL(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7123456789012345678" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.CanonicalURL != "https://www.douyin.com/video/7123456789012345678" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
	if !containsFlag(n.RiskFlags, "modal_id_resolved") {
		t.Fatalf("flags: %v", n.RiskFlags)
	}
}

func TestNormalizeURLSearchModalID(t *testing.T) {
	t.Parallel()
	raw := "https://www.douyin.com/root/search/something?modal_id=7123456789012345678"
	n, err := NormalizeURL(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7123456789012345678" {
		t.Fatalf("id: %s", n.PlatformID)
	}
}

func TestNormalizeURLNumericID(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "7123456789012345678")
	if err != nil {
		t.Fatal(err)
	}
	if n.PlatformID != "7123456789012345678" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if !containsFlag(n.RiskFlags, "numeric_id_input") {
		t.Fatalf("flags: %v", n.RiskFlags)
	}
}

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func TestNormalizeURLBrowsePreservesResolved(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL(context.Background(), "https://www.douyin.com/user/x?modal_id=7123456789012345678&foo=bar")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.BrowseURL, "modal_id=") {
		t.Fatalf("browse should keep resolved url: %s", n.BrowseURL)
	}
}
