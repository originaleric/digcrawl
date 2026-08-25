package youtube

import "testing"

func TestNormalizeVideoURLVariants(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&utm_source=test",
		"https://youtu.be/dQw4w9WgXcQ?t=42",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
	} {
		norm, err := NormalizeURL(rawURL)
		if err != nil {
			t.Fatalf("NormalizeURL(%q): %v", rawURL, err)
		}
		if norm.PlatformID != "dQw4w9WgXcQ" || norm.ContentKind != ContentType || norm.CanonicalURL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Fatalf("unexpected normalization for %q: %+v", rawURL, norm)
		}
	}
}

func TestNormalizeRejectsNonVideoPage(t *testing.T) {
	t.Parallel()
	norm, err := NormalizeURL("https://www.youtube.com/@OpenAI")
	if err != nil {
		t.Fatal(err)
	}
	if norm.ContentKind != "non_video" || norm.PlatformID != "" {
		t.Fatalf("unexpected page classification: %+v", norm)
	}
}

func TestNormalizeRejectsForeignHost(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeURL("https://example.com/watch?v=dQw4w9WgXcQ"); err == nil {
		t.Fatal("expected foreign host rejection")
	}
}
