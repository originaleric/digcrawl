// fake_ytdlp is a native test fixture for the YouTube downloader. Keeping it
// in Go avoids shell and executable-extension assumptions in Windows CI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	mode := os.Getenv("DIGCRAWL_TEST_FAKE_YTDLP_MODE")
	switch mode {
	case "video", "oversized":
		writeVideo(mode)
	case "failure":
		fmt.Fprintln(os.Stderr, "provider diagnostic")
		os.Exit(7)
	case "temporary-oversized":
		writeTemporaryOversized()
		time.Sleep(2 * time.Second)
	case "sleep":
		time.Sleep(2 * time.Second)
	default:
		fmt.Fprintf(os.Stderr, "unexpected fake yt-dlp mode %q\n", mode)
		os.Exit(64)
	}
}

func writeVideo(mode string) {
	template, ok := argumentValue("--output")
	if !ok {
		fmt.Fprintln(os.Stderr, "fake yt-dlp requires --output")
		os.Exit(64)
	}
	path := filepath.Join(filepath.Dir(template), "video.mp4")
	contents := []byte("video")
	if mode == "oversized" {
		contents = []byte("this file is too large")
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(path)
}

func writeTemporaryOversized() {
	template, ok := argumentValue("--output")
	if !ok {
		fmt.Fprintln(os.Stderr, "fake yt-dlp requires --output")
		os.Exit(64)
	}
	path := filepath.Join(filepath.Dir(template), "partial")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 13)), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func argumentValue(flag string) (string, bool) {
	args := os.Args[1:]
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}
