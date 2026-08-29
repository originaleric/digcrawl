package youtube

import (
	"strings"
	"testing"
)

func TestParseCaptionXML(t *testing.T) {
	t.Parallel()
	text, err := parseCaptionXML([]byte(`<transcript><text start="0">Hello &amp; welcome</text><text start="2">to DigCrawl</text></transcript>`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello & welcome\nto DigCrawl" {
		t.Fatalf("text=%q", text)
	}
}

func TestCaptionTracksRejectUntrustedURLs(t *testing.T) {
	t.Parallel()
	tracks := captionTracksFromMeta(map[string]any{"caption_tracks": []any{
		map[string]any{"base_url": "https://www.youtube.com/api/timedtext?v=dQw4w9WgXcQ", "language_code": "en"},
		map[string]any{"base_url": "https://127.0.0.1/private", "language_code": "en"},
	}})
	if len(tracks) != 1 || !strings.Contains(tracks[0].BaseURL, "youtube.com") {
		t.Fatalf("tracks=%+v", tracks)
	}
}

func TestCaptionTracksHaveBoundedRetrySet(t *testing.T) {
	t.Parallel()
	items := make([]any, 0, maxCaptionTracks+2)
	for i := 0; i < maxCaptionTracks+2; i++ {
		items = append(items, map[string]any{"base_url": "https://www.youtube.com/api/timedtext?v=dQw4w9WgXcQ&lang=en"})
	}
	if tracks := captionTracksFromMeta(map[string]any{"caption_tracks": items}); len(tracks) != maxCaptionTracks {
		t.Fatalf("tracks=%d, want %d", len(tracks), maxCaptionTracks)
	}
}
