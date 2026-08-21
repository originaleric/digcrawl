package main

import (
	"flag"
	"testing"
)

func TestNormalizeASRTranscribeArgsAllowsFlagsAfterPath(t *testing.T) {
	args := normalizeASRTranscribeArgs([]string{
		"video.mp4",
		"--provider", "local",
		"--model=small",
		"--response-format", "text",
	})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := fs.String("provider", "", "")
	model := fs.String("model", "", "")
	responseFormat := fs.String("response-format", "", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *provider != "local" || *model != "small" || *responseFormat != "text" {
		t.Fatalf("provider=%q model=%q responseFormat=%q args=%+v", *provider, *model, *responseFormat, args)
	}
	if got := fs.Arg(0); got != "video.mp4" {
		t.Fatalf("positional=%q args=%+v", got, args)
	}
}

func TestNormalizeASRTranscribeArgsKeepsFlagsBeforePath(t *testing.T) {
	args := normalizeASRTranscribeArgs([]string{
		"--provider", "local",
		"--timeout", "1200",
		"--model", "small",
		"video.mp4",
	})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	provider := fs.String("provider", "", "")
	timeout := fs.Int("timeout", 0, "")
	model := fs.String("model", "", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *provider != "local" || *timeout != 1200 || *model != "small" {
		t.Fatalf("provider=%q timeout=%d model=%q args=%+v", *provider, *timeout, *model, args)
	}
	if got := fs.Arg(0); got != "video.mp4" {
		t.Fatalf("positional=%q args=%+v", got, args)
	}
}
