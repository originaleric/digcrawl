package youtube

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type captionRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn captionRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestCaptionTransportReusesSessionWithoutMutatingCaller(t *testing.T) {
	t.Parallel()
	wantURL := "https://www.youtube.com/api/timedtext?v=dQw4w9WgXcQ"
	tr := &captionSessionTransport{
		cookieHeader: func(rawURL string) (string, error) {
			if rawURL != wantURL {
				t.Fatalf("unexpected cookie target: %s", rawURL)
			}
			return "test_session=local", nil
		},
		base: captionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Cookie") != "test_session=local" {
				t.Fatal("caption request did not receive its saved local session")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("<transcript/>")), Header: make(http.Header)}, nil
		}),
	}
	req, _ := http.NewRequest(http.MethodGet, wantURL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if req.Header.Get("Cookie") != "" {
		t.Fatal("caption transport mutated the caller's headers")
	}
}

func TestCaptionTransportRejectsCrossSiteBeforeResolvingCredentials(t *testing.T) {
	t.Parallel()
	tr := &captionSessionTransport{
		cookieHeader: func(string) (string, error) {
			t.Fatal("resolved cookies for an unapproved destination")
			return "", nil
		},
		base: captionRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("sent an unapproved request"); return nil, nil }),
	}
	for _, rawURL := range []string{"https://accounts.google.com/", "https://youtube.com.attacker.invalid/", "http://www.youtube.com/api/timedtext"} {
		req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatalf("accepted caption destination %s", rawURL)
		}
	}
}

func TestCaptionTransportStopsOnUnavailableSession(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("session unavailable")
	tr := &captionSessionTransport{
		cookieHeader: func(string) (string, error) { return "", wantErr },
		base:         captionRoundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("ignored unavailable session"); return nil, nil }),
	}
	req, _ := http.NewRequest(http.MethodGet, "https://www.youtube.com/api/timedtext", nil)
	if _, err := tr.RoundTrip(req); !errors.Is(err, wantErr) {
		t.Fatalf("session error was lost: %v", err)
	}
}

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
