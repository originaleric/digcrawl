package media

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadMP4WithContentRange(t *testing.T) {
	size := int64(len(audioVideoMP4))
	full := fmt.Sprintf("bytes 0-%d/%d", size-1, size)
	larger := fmt.Sprintf("bytes 0-%d/%d", size, size+1)
	for _, tc := range []struct {
		name     string
		ranges   []string
		status   int
		limit    int64
		chunked  bool
		wantFile bool
	}{
		{name: "full HTTP 200", ranges: []string{full}, status: http.StatusOK, limit: size, wantFile: true},
		{name: "full HTTP 200 without content length", ranges: []string{full}, status: http.StatusOK, limit: size, chunked: true, wantFile: true},
		{name: "full HTTP 206", ranges: []string{full}, status: http.StatusPartialContent, limit: size},
		{name: "nonzero start", ranges: []string{fmt.Sprintf("bytes 1-%d/%d", size-1, size)}, status: http.StatusOK, limit: size},
		{name: "incomplete range", ranges: []string{fmt.Sprintf("bytes 0-%d/%d", size-2, size)}, status: http.StatusOK, limit: size},
		{name: "duplicate ranges", ranges: []string{full, full}, status: http.StatusOK, limit: size},
		{name: "unknown total", ranges: []string{fmt.Sprintf("bytes 0-%d/*", size-1)}, status: http.StatusOK, limit: size},
		{name: "conflicting content length", ranges: []string{larger}, status: http.StatusOK, limit: size + 1},
		{name: "short body without content length", ranges: []string{larger}, status: http.StatusOK, limit: size + 1, chunked: true},
		{name: "declared size exceeds budget", ranges: []string{full}, status: http.StatusOK, limit: size - 1, chunked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "video/mp4")
				for _, value := range tc.ranges {
					w.Header().Add("Content-Range", value)
				}
				if !tc.chunked {
					w.Header().Set("Content-Length", fmt.Sprint(size))
				}
				w.WriteHeader(tc.status)
				if tc.chunked {
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write([]byte(audioVideoMP4))
			}))
			defer server.Close()

			path := filepath.Join(t.TempDir(), "download.mp4")
			_, err := DownloadFile(t.Context(), server.Client(), server.URL, path, tc.limit, "", "video/mp4")
			if tc.wantFile {
				if err != nil {
					t.Fatal(err)
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil || string(content) != audioVideoMP4 {
					t.Fatalf("saved bytes differ from the verified MP4: %v", readErr)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted invalid response")
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("incomplete media was left on disk: %v", statErr)
			}
		})
	}
}
