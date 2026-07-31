package threads

import "testing"

func TestNormalizeThreadsPostURL(t *testing.T) {
	norm, err := NormalizeURL("https://threads.net/@zuck/post/ABC_123?x=y")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "ABC_123" || norm.ContentKind != "post" {
		t.Fatalf("unexpected norm: %+v", norm)
	}
	if norm.CanonicalURL != "https://www.threads.com/@zuck/post/ABC_123" {
		t.Fatalf("canonical=%q", norm.CanonicalURL)
	}
}

func TestNormalizeThreadsDotComPostURL(t *testing.T) {
	norm, err := NormalizeURL("https://www.threads.com/@zuck/post/ABC_123")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "ABC_123" || norm.ContentKind != "post" {
		t.Fatalf("unexpected norm: %+v", norm)
	}
	if norm.CanonicalURL != "https://www.threads.com/@zuck/post/ABC_123" {
		t.Fatalf("canonical=%q", norm.CanonicalURL)
	}
}

func TestNormalizeThreadsUserURL(t *testing.T) {
	norm, err := NormalizeURL("https://www.threads.net/@meta/")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "meta" || norm.ContentKind != "user" {
		t.Fatalf("unexpected norm: %+v", norm)
	}
	if norm.CanonicalURL != "https://www.threads.com/@meta" {
		t.Fatalf("canonical=%q", norm.CanonicalURL)
	}
}
