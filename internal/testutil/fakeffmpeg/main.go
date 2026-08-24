package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fake ffmpeg requires an output path")
		os.Exit(64)
	}
	output := args[len(args)-1]
	if strings.Contains(output, "%") {
		output = strings.Replace(output, "%06d", "000001", 1)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(output, []byte("wav"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
