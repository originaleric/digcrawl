package reddit

import "testing"

func TestNormalizePostURLCanonicalizesSubredditPost(t *testing.T) {
	t.Parallel()
	norm, err := NormalizePostURL("https://www.reddit.com/r/golang/comments/abc123/title/?utm_source=x")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "abc123" || norm.CanonicalURL != "https://www.reddit.com/r/golang/comments/abc123/" {
		t.Fatalf("unexpected normalization: %+v", norm)
	}
}

func TestNormalizePostURLAcceptsShortLink(t *testing.T) {
	t.Parallel()
	norm, err := NormalizePostURL("https://redd.it/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if norm.PlatformID != "abc123" || norm.CanonicalURL != "https://www.reddit.com/comments/abc123/" {
		t.Fatalf("unexpected normalization: %+v", norm)
	}
}

func TestNormalizePostURLRejectsListing(t *testing.T) {
	t.Parallel()
	if _, err := NormalizePostURL("https://www.reddit.com/r/golang/new/"); err == nil {
		t.Fatal("expected listing URL to be rejected as a post")
	}
}
