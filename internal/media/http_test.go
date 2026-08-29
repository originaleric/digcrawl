package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
