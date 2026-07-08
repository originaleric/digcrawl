package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/originaleric/digcrawl/internal/asr"
)

func runASR(args []string) {
	if len(args) == 0 {
		printASRUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "transcribe":
		runASRTranscribe(args[1:])
	case "help", "-h", "--help":
		printASRUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知 asr 命令: %s\n\n", args[0])
		printASRUsage()
		os.Exit(1)
	}
}

func printASRUsage() {
	fmt.Fprintf(os.Stderr, `用法:
  digcrawl asr transcribe <video-or-audio-path> [flags]

示例:
  digcrawl asr transcribe ./video.mp4 --provider local --model small
  digcrawl asr transcribe ./video.mp4 --provider api --api-key "$OPENAI_API_KEY"

`)
}

func runASRTranscribe(args []string) {
	fs := flag.NewFlagSet("asr transcribe", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	provider := fs.String("provider", "", "ASR provider: api | local | stub")
	apiKey := fs.String("api-key", "", "ASR API key override")
	baseURL := fs.String("base-url", "", "ASR API base URL override")
	model := fs.String("model", "", "ASR model override")
	language := fs.String("language", "", "language code override")
	prompt := fs.String("prompt", "", "ASR prompt override")
	responseFormat := fs.String("response-format", "", "ASR API response format override: verbose_json | json | text | srt | vtt")
	ffmpegPath := fs.String("ffmpeg", "", "ffmpeg executable path override")
	localCommand := fs.String("local-command", "", "local ASR command override")
	_ = fs.Parse(normalizeASRTranscribeArgs(args))

	if len(fs.Args()) == 0 {
		log.Fatal("asr transcribe: provide a video or audio path")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	asrCfg := cfg.ASR
	if *provider != "" {
		asrCfg.Provider = *provider
	}
	if *apiKey != "" {
		asrCfg.APIKey = *apiKey
	}
	if *baseURL != "" {
		asrCfg.BaseURL = *baseURL
	}
	if *model != "" {
		asrCfg.Model = *model
	}
	if *language != "" {
		asrCfg.Language = *language
	}
	if *prompt != "" {
		asrCfg.Prompt = *prompt
	}
	if *responseFormat != "" {
		asrCfg.ResponseFormat = *responseFormat
	}
	if *ffmpegPath != "" {
		asrCfg.FFmpegPath = *ffmpegPath
	}
	if *localCommand != "" {
		asrCfg.LocalCommand = *localCommand
	}

	result, err := asr.Transcribe(context.Background(), fs.Args()[0], asrCfg, asr.Options{})
	if err != nil {
		log.Fatalf("asr transcribe: %v", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	fmt.Println(string(data))
}

func normalizeASRTranscribeArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	flagNamesWithValue := map[string]struct{}{
		"-config":           {},
		"--config":          {},
		"-provider":         {},
		"--provider":        {},
		"-api-key":          {},
		"--api-key":         {},
		"-base-url":         {},
		"--base-url":        {},
		"-model":            {},
		"--model":           {},
		"-language":         {},
		"--language":        {},
		"-prompt":           {},
		"--prompt":          {},
		"-response-format":  {},
		"--response-format": {},
		"-ffmpeg":           {},
		"--ffmpeg":          {},
		"-local-command":    {},
		"--local-command":   {},
	}
	var flags []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if strings.Contains(arg, "=") {
				continue
			}
			if _, ok := flagNamesWithValue[arg]; ok && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, arg)
	}
	out := make([]string, 0, len(args))
	out = append(out, flags...)
	out = append(out, positional...)
	return out
}
