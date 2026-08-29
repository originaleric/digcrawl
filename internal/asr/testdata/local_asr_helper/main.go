package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 3 && args[0] == "-m" && args[1] == "venv" {
		createVenv(args[2])
		return
	}
	if os.Getenv("DIGCRAWL_TEST_LOCAL_ASR_MODE") == "runtime-existing-health-fail" && len(args) >= 2 && args[0] == "-m" && args[1] == "pip" {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-c" {
		switch os.Getenv("DIGCRAWL_TEST_LOCAL_ASR_MODE") {
		case "runtime-health-hang":
			select {}
		case "runtime-health-fail":
			fmt.Fprintln(os.Stderr, "faster_whisper import failed: missing dependency")
			os.Exit(2)
		case "runtime-existing-health-fail":
			if strings.Contains(args[1], "import faster_whisper") && strings.Contains(filepath.Clean(os.Args[0]), string(filepath.Separator)+"asr_venv"+string(filepath.Separator)) {
				fmt.Fprintln(os.Stderr, "faster_whisper import failed: existing runtime is broken")
				os.Exit(2)
			}
		}
		return
	}
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
	case "ffprobe":
		fmt.Print("426.926500\n")
	case "slow-transcript-with-probe":
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"), "ffprobe") {
			fmt.Print("426.926500\n")
			return
		}
		time.Sleep(2 * time.Second)
		fmt.Print(`{"text":"local transcript"}`)
	case "slow-probe":
		time.Sleep(2 * time.Second)
		fmt.Print("426.926500\n")
	default:
		fmt.Fprintf(os.Stderr, "unexpected local ASR test helper mode %q\n", os.Getenv("DIGCRAWL_TEST_LOCAL_ASR_MODE"))
		os.Exit(64)
	}
}

func createVenv(dir string) {
	pythonDir := "bin"
	pythonName := "python"
	if runtime.GOOS == "windows" {
		pythonDir = "Scripts"
		pythonName = "python.exe"
	}
	python := filepath.Join(dir, pythonDir, pythonName)
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(python, data, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
