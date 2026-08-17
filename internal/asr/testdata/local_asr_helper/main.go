package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	switch os.Getenv("DIGCRAWL_TEST_LOCAL_ASR_MODE") {
	case "transcript":
		fmt.Print(`{"text":"local transcript","segments":[{"start":0,"end":1,"text":"local transcript"}]}`)
	case "cache":
		if strings.HasSuffix(filepath.Clean(os.Getenv("HF_HOME")), filepath.Join("storage", "digcrawl", "asr_models")) {
			fmt.Print(`{"text":"local transcript"}`)
			return
		}
		fmt.Fprintf(os.Stderr, "bad HF_HOME=%s\n", os.Getenv("HF_HOME"))
		os.Exit(2)
	case "preflight-ok":
		args := os.Args[1:]
		if len(args) != 5 || !strings.HasPrefix(filepath.Base(args[0]), "faster_whisper_asr-") || args[1] != "--check" || args[2] != "--check-model" || args[3] != "--model" || args[4] != "small" {
			fmt.Fprintf(os.Stderr, "unexpected preflight arguments: %q\n", args)
			os.Exit(13)
		}
		fmt.Print(`{"status":"ok"}`)
	case "preflight-fail":
		fmt.Fprintln(os.Stderr, "faster_whisper import failed: missing")
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "unexpected local ASR test helper mode %q\n", os.Getenv("DIGCRAWL_TEST_LOCAL_ASR_MODE"))
		os.Exit(64)
	}
}
