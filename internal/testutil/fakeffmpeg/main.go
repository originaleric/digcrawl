package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fake ffmpeg requires an output path")
		os.Exit(64)
	}
	if err := os.WriteFile(args[len(args)-1], []byte("wav"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
