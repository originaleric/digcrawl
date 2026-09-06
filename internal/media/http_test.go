package media

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPreferOCRCompatibleImageFormat(t *testing.T) {
	for _, tc := range []struct {
		mediaType string
		want      string
	}{
		{mediaType: "image", want: OCRImageAccept},
		{mediaType: "photo", want: OCRImageAccept},
		{mediaType: "screenshot", want: OCRImageAccept},
		{mediaType: "video", want: ""},
		{mediaType: "audio", want: ""},
	} {
		t.Run(tc.mediaType, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "https://example.com/media", nil)
			if err != nil {
				t.Fatal(err)
			}
			PreferOCRCompatibleImageFormat(req, tc.mediaType)
			if got := req.Header.Get("Accept"); got != tc.want {
				t.Fatalf("Accept=%q want %q", got, tc.want)
			}
		})
	}
}

func TestDownloadFileRemovesPartialOutputOnStreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("partial"))
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "asset.jpg")
	if _, err := DownloadFile(context.Background(), server.Client(), server.URL, dest, 1024, "", "image"); err == nil {
		t.Fatal("expected truncated stream to fail")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial download must be removed, stat err=%v", err)
	}
}

func TestDownloadFileRejectsHTMLForVideoAndCleansOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>login required</html>"))
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "video.mp4")
	if _, err := DownloadFile(context.Background(), server.Client(), server.URL, dest, 1024, "", "video"); err == nil {
		t.Fatal("expected HTML response to be rejected as video")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("rejected video response must be removed, stat err=%v", err)
	}
}

func TestDownloadFileAcceptsRecognizedVideoWithGenericContentType(t *testing.T) {
	video := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(video)
	}))
	defer server.Close()
	dest := filepath.Join(t.TempDir(), "video.mp4")
	if _, err := DownloadFile(context.Background(), server.Client(), server.URL, dest, 1024, "", "video"); err != nil {
		t.Fatalf("recognized video should download: %v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != string(video) {
		t.Fatalf("saved video=%q err=%v", got, err)
	}
}

func TestValidateRemoteURLRejectsUnsafeInputs(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1/video.mp4",
		"http://100.64.0.1/video.mp4",
		"https://[::1]/video.mp4",
		"https://localhost/video.mp4",
		"https://user:pass@example.com/video.mp4",
		"file:///tmp/video.mp4",
	} {
		if err := ValidateRemoteURL(rawURL); err == nil {
			t.Fatalf("ValidateRemoteURL(%q) succeeded", rawURL)
		}
	}
	if err := ValidateRemoteURL("https://cdn.example.com/video.mp4?token=a"); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePublicHostRejectsPrivateLiteralsAndDNSAnswers(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1"} {
		if _, err := resolvePublicHost(context.Background(), host); err == nil {
			t.Fatalf("resolvePublicHost(%q) succeeded", host)
		}
	}
	_, err := resolvePublicHostWithLookup(context.Background(), "cdn.example.test", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("192.168.1.10")}}, nil
	})
	if err == nil {
		t.Fatal("mixed public/private DNS answer was accepted")
	}
}

func TestNewSafeHTTPClientRejectsPrivateRedirect(t *testing.T) {
	client := NewSafeHTTPClient(time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatalf("safe transport not installed: %#v", client.Transport)
	}
	privateURL, err := url.Parse("http://127.0.0.1/internal")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(&http.Request{URL: privateURL}, nil); err == nil {
		t.Fatal("private redirect target was accepted")
	}
}
