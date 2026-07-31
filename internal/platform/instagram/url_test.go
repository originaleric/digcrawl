package instagram

import "testing"

func TestNormalizeInstagramPostURL(t *testing.T) {
	norm, err := NormalizeURL("https://instagram.com/p/CODE_123/?utm_source=x")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "CODE_123" || norm.ContentKind != "p" {
		t.Fatalf("unexpected norm: %+v", norm)
	}
	if norm.CanonicalURL != "https://www.instagram.com/p/CODE_123/" {
		t.Fatalf("canonical=%q", norm.CanonicalURL)
	}
}

func TestNormalizeInstagramReelURL(t *testing.T) {
	norm, err := NormalizeURL("https://www.instagram.com/reel/R123/")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "R123" || norm.ContentKind != "reel" {
		t.Fatalf("unexpected norm: %+v", norm)
	}
	if !IsMediaURL(norm.CanonicalURL) {
		t.Fatalf("expected media url")
	}
}
