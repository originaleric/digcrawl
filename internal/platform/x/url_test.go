package x

import (
	"testing"
)

func TestNormalizeURLTwitterToX(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL("https://twitter.com/elonmusk/status/1234567890")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://x.com/elonmusk/status/1234567890"
	if n.CanonicalURL != want {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
	if n.PlatformID != "1234567890" {
		t.Fatalf("id: %s", n.PlatformID)
	}
	if n.CookieDomain != CookieDomain {
		t.Fatalf("cookie: %s", n.CookieDomain)
	}
}

func TestNormalizeURLXHost(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL("https://x.com/jack/status/42")
	if err != nil {
		t.Fatal(err)
	}
	if n.CanonicalURL != "https://x.com/jack/status/42" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
}

func TestNormalizeURLWWWXHost(t *testing.T) {
	t.Parallel()
	n, err := NormalizeURL("https://www.x.com/jack/status/42")
	if err != nil {
		t.Fatal(err)
	}
	if n.CanonicalURL != "https://x.com/jack/status/42" {
		t.Fatalf("canonical: %s", n.CanonicalURL)
	}
}
