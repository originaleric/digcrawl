package media

import (
	_ "embed"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/originaleric/digcrawl/internal/testutil"
)

//go:embed testdata/fragmented.mp4
var fragmentedMP4 string

func TestValidateMP4ChecksReferencedSampleRanges(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"progressive", testutil.MP4}, {"fragmented", fragmentedMP4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			media := []byte(tc.data)
			var offset int
			for offset < len(media) && string(media[offset+4:offset+8]) != "mdat" {
				offset += int(binary.BigEndian.Uint32(media[offset:]))
			}
			if offset+9 >= len(media) {
				t.Fatal("missing mdat in fixture")
			}
			// A zero-sized mdat extends to EOF. Cut after one byte of
			// payload while keeping the complete track/sample metadata.
			full := append([]byte(nil), media[:offset+int(binary.BigEndian.Uint32(media[offset:]))]...)
			binary.BigEndian.PutUint32(full[offset:], 0)
			for _, fixture := range []struct {
				name  string
				data  []byte
				valid bool
			}{
				{"original", media, true},
				{"mdat to EOF", full, true},
				{"truncated mdat to EOF", full[:offset+9], false},
			} {
				t.Run(fixture.name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "video.mp4")
					if err := os.WriteFile(path, fixture.data, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := ValidateMP4(path); (err == nil) != fixture.valid {
						t.Fatalf("validation error = %v, want valid = %t", err, fixture.valid)
					}
				})
			}
		})
	}
}

//go:embed testdata/audio-video.mp4
var audioVideoMP4 string

func TestMP4DownloadRejectsTruncatedAudioTrack(t *testing.T) {
	body := []byte(audioVideoMP4)
	offset := 0
	for string(body[offset+4:offset+8]) != "mdat" {
		offset += int(binary.BigEndian.Uint32(body[offset:]))
	}
	for _, tc := range []struct {
		name  string
		body  []byte
		valid bool
	}{
		{"complete audio and video", body, true},
		{"truncated final audio packet", append([]byte(nil), body[:len(body)-1]...), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.valid {
				// The final mdat extends to EOF, so its box size still fits the response.
				binary.BigEndian.PutUint32(tc.body[offset:], 0)
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = w.Write(tc.body)
			}))
			defer s.Close()
			path := filepath.Join(t.TempDir(), "download.mp4")
			_, err := DownloadFile(t.Context(), s.Client(), s.URL, path, 1<<20, "", "video/mp4")
			if (err == nil) != tc.valid {
				t.Fatalf("download error = %v, want valid = %t", err, tc.valid)
			}
			if !tc.valid {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("incomplete media was left on disk: %v", err)
				}
			}
		})
	}
}
