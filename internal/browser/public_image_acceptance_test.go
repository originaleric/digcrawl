package browser

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestChromeCrossOriginImagesAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_BROWSER_ACCEPTANCE") != "1" {
		t.Skip("real Chrome acceptance is opt-in")
	}
	for _, mode := range []string{"public", "session", "denied"} {
		t.Run(mode, func(t *testing.T) {
			png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF1sAAAAASUVORK5CYII=")
			var reads, credentialed atomic.Int32
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "public" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if r.Header.Get("Sec-Fetch-Dest") != "image" {
					reads.Add(1)
					c, err := r.Cookie("image_session")
					if err == nil && c.Value == "fixture" {
						credentialed.Add(1)
					}
					if mode == "denied" || mode == "session" && err != nil {
						w.WriteHeader(401)
						return
					}
				} else {
					http.SetCookie(w, &http.Cookie{Name: "image_session", Value: "fixture", Path: "/", HttpOnly: true})
				}
				w.Header().Set("Content-Type", "image/png")
				w.Write(png)
			}))
			defer cdn.Close()
			doc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				io.WriteString(w, `<html><head><title>image acquisition</title></head><body><article>body<img style="width:120px;height:120px" src="`+cdn.URL+`/image.png" onload="this.id='ready'"></article></body></html>`)
			}))
			defer doc.Close()
			headless := true
			cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: os.Getenv("DIGCRAWL_CHROME_PATH"), Headless: &headless, UserDataDir: t.TempDir(), MaxConcurrency: 1, TotalTimeoutSec: 20, NavigateTimeoutSec: 5, WaitSelectorTimeoutSec: 3})
			previous := *config.Get()
			config.Get().LocalBrowser = cfg
			config.Get().Gateway.AllowedDomains = nil
			defer func() { *config.Get() = previous }()
			m := &browserManager{cfg: cfg, slots: make(chan struct{}, 1), sessions: make(map[uint64]*browserSession), stopCh: make(chan struct{})}
			defer m.shutdownImmediately(context.Background())
			resp, err := browserBrowseWithSessionRunner(context.Background(), &BrowserBrowseRequest{URL: doc.URL, Mode: "full", ContentSelector: "article", WaitSelector: "#ready", ImageMaxItems: 4, ImageMaxBytes: 4096}, func() *browserManager { return m }, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.ImageData) != 1 {
				t.Fatalf("images=%+v", resp.ImageData)
			}
			if mode == "denied" {
				if resp.ImageData[0]["error"] != "access_denied" || reads.Load() != 2 {
					t.Fatalf("authorization bypass/retry mismatch: %+v, reads=%d", resp.ImageData, reads.Load())
				}
			} else if resp.ImageData[0]["data"] == "" {
				t.Fatalf("loaded image acquisition failed: %+v", resp.ImageData)
			}
			if mode == "public" && (reads.Load() != 1 || credentialed.Load() != 0) {
				t.Fatalf("public read sent credentials or retried: reads=%d with_cookie=%d", reads.Load(), credentialed.Load())
			}
			if mode == "session" && (reads.Load() != 2 || credentialed.Load() != 1) {
				t.Fatalf("session fallback wrong: reads=%d with_cookie=%d", reads.Load(), credentialed.Load())
			}
		})
	}
}
