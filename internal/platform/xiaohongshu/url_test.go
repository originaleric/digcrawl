package xiaohongshu

import (
	"context"
	"strings"
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
	if !containsFlag(n.RiskFlags, "xsec_token_missing") {
		t.Fatalf("expected xsec_token_missing flag: %v", n.RiskFlags)
	}
}

func TestNormalizeURLWithXsecToken(t *testing.T) {
	t.Parallel()
	raw := "https://www.xiaohongshu.com/explore/662f8b3a000000001e031234?xsec_token=abc123&xsec_source=pc_search"
	n, err := NormalizeURL(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if n.QueryParams["xsec_token"] != "abc123" {
		t.Fatalf("token: %v", n.QueryParams)
	}
	if n.QueryParams["xsec_source"] != "pc_search" {
		t.Fatalf("source: %v", n.QueryParams)
	}
	if !strings.Contains(n.BrowseURL, "xsec_token=abc123") {
		t.Fatalf("browse url missing token: %s", n.BrowseURL)
	}
	if containsFlag(n.RiskFlags, "xsec_token_missing") {
		t.Fatalf("unexpected missing flag: %v", n.RiskFlags)
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

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}
