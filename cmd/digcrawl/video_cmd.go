package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
	internalvideo "github.com/originaleric/digcrawl/internal/video"
)

func runVideo(args []string) {
	fs := flag.NewFlagSet("video", flag.ExitOnError)
	configPath := fs.String("config", "", "config path; defaults to project config, then the user Skill profile")
	mode := fs.String("mode", "auto", "processors: auto | asr | ocr | both")
	format := fs.String("format", "json", "output format: json | text | md")
	outPath := fs.String("out", "", "write output to this file instead of stdout")
	maxBytes := fs.Int64("max-bytes", internalvideo.DefaultMaxBytes, "maximum URL download size in bytes")
	downloadTimeout := fs.Int("download-timeout", 300, "URL download timeout in seconds")
	keepPath := fs.String("keep", "", "persist a downloaded URL video at this new path")
	_ = fs.Parse(normalizeVideoArgs(args))
	if len(fs.Args()) != 1 {
		printVideoUsage()
		os.Exit(1)
	}
	if err := validateVideoCLIOptions(*maxBytes, *downloadTimeout); err != nil {
		log.Fatalf("video: %v", err)
	}
	cfg, err := loadVideoConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	result, analyzeErr := internalvideo.Analyze(context.Background(), fs.Arg(0), cfg, internalvideo.Options{
		Mode:            *mode,
		MaxBytes:        *maxBytes,
		DownloadTimeout: time.Duration(*downloadTimeout) * time.Second,
		KeepPath:        *keepPath,
	})
	if result != nil {
		data, err := formatVideoResult(result, *format)
		if err != nil {
			log.Fatalf("format video result: %v", err)
		}
		if *outPath == "" {
			fmt.Println(string(data))
		} else if err := os.WriteFile(*outPath, data, 0o600); err != nil {
			log.Fatalf("write video output: %v", err)
		}
	}
	if analyzeErr != nil {
		log.Fatalf("video: %v", analyzeErr)
	}
}

// loadVideoConfig gives the standalone command a useful default outside a
// repository while preserving the existing project-config workflow. A caller
// supplied --config always wins. With the implicit project config, an active
// processor in the standard user Skill profile fills only a processor the
// project config did not activate.
func loadVideoConfig(requestedPath string) (*config.Config, error) {
	path, err := videoConfigPath(requestedPath)
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}
	effective := videoAnalysisConfig(cfg)
	if strings.TrimSpace(requestedPath) != "" || path != "config/config.yaml" {
		return effective, nil
	}

	skillPath := config.DefaultSkillProfilePath()
	exists, err := videoConfigFileExists(skillPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return effective, nil
	}
	skillCfg, err := loadConfig(skillPath)
	if err != nil {
		return nil, err
	}
	return mergeVideoProcessors(effective, videoAnalysisConfig(skillCfg)), nil
}

func videoConfigPath(requestedPath string) (string, error) {
	if requestedPath = strings.TrimSpace(requestedPath); requestedPath != "" {
		return requestedPath, nil
	}
	for _, path := range []string{"config/config.yaml", config.DefaultSkillProfilePath()} {
		exists, err := videoConfigFileExists(path)
		if err != nil {
			return "", err
		}
		if exists {
			return path, nil
		}
	}
	// Keep loadConfig's established missing-config behavior when neither
	// location exists: it logs the condition and falls back to defaults.
	return "config/config.yaml", nil
}

func videoConfigFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

// videoAnalysisConfig bridges the user Skill profile and the standalone
// command without changing the configuration model. Root ASR/OCR settings are
// the existing CLI source of truth whenever they enable a processor. A Skill
// profile can independently enable the same processor, so use its active
// settings only when the root counterpart is not active.
func videoAnalysisConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	effective := *cfg
	if !effective.ASR.EnabledOrDefault() && effective.Skill.ASR.EnabledOrDefault() {
		effective.ASR = effective.Skill.ASR
	}
	if !videoOCREnabled(effective.OCR) && videoOCREnabled(effective.Skill.OCR) {
		effective.OCR = effective.Skill.OCR
	}
	return &effective
}

// mergeVideoProcessors fills inactive processors from a fallback profile. It
// deliberately copies only the processor settings: command flags control URL
// acquisition and output, and neither profile should override the other
// profile's unrelated runtime configuration.
func mergeVideoProcessors(primary, fallback *config.Config) *config.Config {
	if primary == nil {
		return fallback
	}
	if fallback == nil {
		return primary
	}
	effective := *primary
	if !effective.ASR.EnabledOrDefault() && fallback.ASR.EnabledOrDefault() {
		effective.ASR = fallback.ASR
	}
	if !videoOCREnabled(effective.OCR) && videoOCREnabled(fallback.OCR) {
		effective.OCR = fallback.OCR
	}
	return &effective
}

func videoOCREnabled(cfg config.OCRConfig) bool {
	return cfg.EnabledOrDefault() && cfg.VideoFrames.EnabledOrDefault()
}

func validateVideoCLIOptions(maxBytes int64, downloadTimeout int) error {
	if maxBytes <= 0 {
		return fmt.Errorf("--max-bytes must be greater than zero")
	}
	if downloadTimeout <= 0 {
		return fmt.Errorf("--download-timeout must be greater than zero")
	}
	return nil
}

func printVideoUsage() {
	fmt.Fprintf(os.Stderr, `用法:
  digcrawl video <local-video-path-or-direct-video-url> [flags]

示例:
  digcrawl video ./meeting.mp4
  digcrawl video "https://cdn.example.com/meeting.mp4" --mode both
  digcrawl video ./meeting.mp4 --mode asr --format text
  digcrawl video "https://cdn.example.com/meeting.mp4" --keep ./meeting.mp4

参数:
  --mode auto|asr|ocr|both  auto 仅运行配置中已启用的处理器
  --format json|text|md     默认 json
  --out <file>              将结果写入文件
  --max-bytes <n>           URL 下载大小上限，默认 512 MiB
  --download-timeout <sec>  URL 下载超时，默认 300 秒
  --keep <path>             保留远程 URL 下载的视频；目标文件不得已存在

说明:
  URL 必须是可直接下载的视频 URL。YouTube、抖音等页面 URL 请使用对应平台读取工具。

`)
}

func formatVideoResult(result *internalvideo.Result, rawFormat string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(rawFormat)) {
	case "", "json":
		return json.MarshalIndent(result, "", "  ")
	case "text":
		if strings.TrimSpace(result.Text) != "" {
			return []byte(result.Text), nil
		}
		if len(result.Warnings) > 0 {
			return []byte(strings.Join(result.Warnings, "\n")), nil
		}
		return []byte{}, nil
	case "md", "markdown":
		var b strings.Builder
		fmt.Fprintf(&b, "# Video analysis\n\n- Source: `%s`\n- Duration: %.2fs\n- Size: %d bytes\n\n", result.Source.Input, result.Source.DurationSeconds, result.Source.Bytes)
		if result.Text != "" {
			b.WriteString(result.Text)
			b.WriteString("\n")
		}
		if len(result.Warnings) > 0 {
			b.WriteString("\n## Warnings\n\n")
			for _, warning := range result.Warnings {
				fmt.Fprintf(&b, "- %s\n", warning)
			}
		}
		return []byte(b.String()), nil
	default:
		return nil, fmt.Errorf("unsupported --format %q (want json, text, or md)", rawFormat)
	}
}

// normalizeVideoArgs lets the source appear before or after flags, consistent
// with `digcrawl asr transcribe` and other single-source CLI commands.
func normalizeVideoArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	flagsWithValue := map[string]struct{}{
		"-config": {}, "--config": {}, "-mode": {}, "--mode": {},
		"-format": {}, "--format": {}, "-out": {}, "--out": {},
		"-max-bytes": {}, "--max-bytes": {}, "-download-timeout": {}, "--download-timeout": {},
		"-keep": {}, "--keep": {},
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, 1)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positionals = append(positionals, args[index+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") {
				if _, ok := flagsWithValue[arg]; ok && index+1 < len(args) {
					index++
					flags = append(flags, args[index])
				}
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...)
}
