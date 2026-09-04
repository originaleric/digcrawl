package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/asr"
)

func runASR(args []string) {
	if len(args) == 0 {
		printASRUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "setup":
		runASRSetup(args[1:])
	case "check":
		runASRCheck(args[1:])
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
  digcrawl asr setup [--config <path>] [--scope auto|root|skill] [flags]
  digcrawl asr check [--config <path>] [--scope auto|root|skill]
  digcrawl asr transcribe <video-or-audio-path> [--config <path>] [--scope auto|root|skill] [flags]

示例:
  digcrawl asr setup --yes
  digcrawl asr setup --model base --yes
  digcrawl asr check
  digcrawl asr transcribe ./video.mp4 --provider local --model small
  digcrawl asr transcribe ./video.mp4 --provider api --api-key "$OPENAI_API_KEY"

`)
}

func runASRSetup(args []string) {
	fs := flag.NewFlagSet("asr setup", flag.ExitOnError)
	configPath := fs.String("config", "", "path to root config.yaml or Skill profile; defaults to project config, then the user Skill profile")
	scope := fs.String("scope", "auto", "ASR configuration scope: auto | root | skill")
	python := fs.String("python", "", "supported Python 3.9-3.13 interpreter used to create the isolated local runtime")
	model := fs.String("model", "", "local faster-whisper model to prepare; default uses ASR.Model for a local provider, otherwise local default")
	skipModel := fs.Bool("skip-model", false, "prepare only the default faster-whisper runtime; a custom runner is left unchanged")
	yes := fs.Bool("yes", false, "confirm downloading local ASR runtime dependencies and, unless --skip-model is set, the selected model")
	_ = fs.Parse(args)
	target, err := loadASRTarget(*configPath, *scope)
	if err != nil {
		log.Fatalf("load ASR configuration: %v", err)
	}
	asrCfg := localASRSetupConfig(target.settings, *model)
	plan, err := planLocalASRSetup(asrCfg, *skipModel)
	if err != nil {
		log.Fatalf("local ASR model setup: %v", err)
	}
	if !plan.managesRuntime {
		fmt.Fprintln(os.Stdout, "Custom local ASR integration selected; DigCrawl did not create a Python runtime or download a model. Its runtime and model remain managed by ASR.LocalCommand/LocalArgs.")
		return
	}
	if !*yes {
		confirmed, err := confirmASRSetup(os.Stdin, os.Stderr, asrCfg.ModelOrDefault(), *skipModel)
		if err != nil {
			log.Fatalf("confirm local ASR setup: %v", err)
		}
		if !confirmed {
			log.Printf("local ASR setup cancelled")
			return
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintln(os.Stderr, "Checking or preparing the isolated local ASR runtime. Dependency installation may take several minutes when needed...")
	runtimePython, err := asr.PrepareLocalRuntime(ctx, asrCfg, *python)
	if err != nil {
		log.Fatalf("prepare local ASR runtime: %v", err)
	}
	fmt.Fprintf(os.Stdout, "Local ASR runtime is ready: %s\n", runtimePython)
	if plan.skipModel {
		fmt.Fprintln(os.Stdout, "Model download was skipped. Local transcription and ASR health checks stay unavailable until you rerun setup without --skip-model.")
		return
	}
	fmt.Fprintf(os.Stderr, "Downloading and verifying local ASR model %q. This may take several minutes on the first run...\n", asrCfg.ModelOrDefault())
	if err := asr.PrepareLocalModel(ctx, asrCfg); err != nil {
		log.Fatalf("prepare local ASR model: %v", err)
	}
	readiness, err := asr.CheckReadiness(asrCfg)
	if err != nil {
		log.Fatalf("verify local ASR setup: %v", err)
	}
	if !readiness.OfflineModelVerified {
		log.Fatal("verify local ASR setup: default faster-whisper model was not verified")
	}
	if localASRSetupMatchesConfiguredDefault(target.settings, asrCfg) {
		fmt.Fprintf(os.Stdout, "Local ASR (%s scope) is ready for offline transcription: model %q in %s\n", target.scope, asrCfg.ModelOrDefault(), asrCfg.ModelCacheDirOrDefault())
		return
	}
	fmt.Fprintf(os.Stdout, "Local ASR model %q is prepared in %s. The selected %s ASR configuration is unchanged; use `--provider local --model %s` when transcribing, or set its Provider=local and Model=%s before relying on default transcription or `asr check`.\n", asrCfg.ModelOrDefault(), asrCfg.ModelCacheDirOrDefault(), target.scope, asrCfg.ModelOrDefault(), asrCfg.ModelOrDefault())
}

type localASRSetupPlan struct {
	managesRuntime bool
	skipModel      bool
}

// planLocalASRSetup defines the ownership boundary for setup. Only the
// bundled faster-whisper runner has a DigCrawl-managed Python runtime and
// model cache. A custom command owns both, so --skip-model is deliberately a
// no-op rather than an installation of unrelated dependencies.
func planLocalASRSetup(cfg config.ASRConfig, skipModel bool) (localASRSetupPlan, error) {
	if err := asr.ValidateLocalModelSetup(cfg); err != nil {
		if skipModel {
			return localASRSetupPlan{}, nil
		}
		return localASRSetupPlan{}, fmt.Errorf("%w; use --skip-model only when you intentionally manage the model outside DigCrawl", err)
	}
	return localASRSetupPlan{managesRuntime: true, skipModel: skipModel}, nil
}

type asrTarget struct {
	settings config.ASRConfig
	scope    config.ASRScope
	path     string
}

// loadASRTarget makes setup and direct transcription share exactly one
// configuration-domain and cache-selection rule. Release users never fall
// back to a working-directory-relative runtime: they must have either the
// project config or the user Skill profile that owns their ASR state.
func loadASRTarget(requestedPath, requestedScope string) (asrTarget, error) {
	scope := config.ASRScope(strings.ToLower(strings.TrimSpace(requestedScope)))
	if path := strings.TrimSpace(requestedPath); path != "" {
		exists, err := asrConfigFileExists(path)
		if err != nil {
			return asrTarget{}, err
		}
		if !exists {
			return asrTarget{}, fmt.Errorf("configuration file %s does not exist", path)
		}
		target, _, err := loadASRTargetFromPath(path, scope, false)
		return target, err
	}
	for _, path := range []string{"config/config.yaml", config.DefaultSkillProfilePath()} {
		exists, err := asrConfigFileExists(path)
		if err != nil {
			return asrTarget{}, err
		}
		if !exists {
			continue
		}
		target, found, err := loadASRTargetFromPath(path, scope, scope == config.ASRScopeAuto || scope == "")
		if err != nil {
			return asrTarget{}, err
		}
		if found {
			return target, nil
		}
	}
	return asrTarget{}, fmt.Errorf("no ASR configuration found at config/config.yaml or %s; run `digcrawl init --profile skill` or pass --config", config.DefaultSkillProfilePath())
}

func loadASRTargetFromPath(path string, requestedScope config.ASRScope, skipNoASRDomain bool) (asrTarget, bool, error) {
	// Config discovery must not activate a candidate. In particular, a project
	// config without ASR settings must not apply its RuntimeEnv before the user
	// Skill profile is selected. Validate is intentionally side-effect free;
	// Load below preserves the normal runtime-environment behavior only for the
	// selected configuration.
	preflight, err := config.Validate(path)
	if err != nil {
		return asrTarget{}, false, err
	}
	if skipNoASRDomain && !preflight.HasRootASR && !preflight.HasSkillProfile {
		return asrTarget{}, false, nil
	}
	if _, _, err := preflight.SelectASRConfig(requestedScope); err != nil {
		return asrTarget{}, false, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return asrTarget{}, false, err
	}
	settings, scope, err := cfg.SelectASRConfig(requestedScope)
	if err != nil {
		return asrTarget{}, false, err
	}
	return asrTarget{settings: settings, scope: scope, path: path}, true, nil
}

func asrConfigFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

func localASRSetupConfig(configured config.ASRConfig, modelOverride string) config.ASRConfig {
	setup := configured
	// A configured API model (for example whisper-1) is not a faster-whisper
	// model. `asr setup` is an explicit local operation, so select the local
	// default unless the caller names a model for this preparation.
	if configured.ProviderOrDefault() != "local" && strings.TrimSpace(modelOverride) == "" {
		setup.Model = ""
	}
	setup.Provider = "local"
	if strings.TrimSpace(modelOverride) != "" {
		setup.Model = strings.TrimSpace(modelOverride)
	}
	return setup
}

func localASRSetupMatchesConfiguredDefault(configured, setup config.ASRConfig) bool {
	return configured.ProviderOrDefault() == "local" && configured.ModelOrDefault() == setup.ModelOrDefault()
}

func confirmASRSetup(in io.Reader, out io.Writer, model string, skipModel bool) (bool, error) {
	if skipModel {
		fmt.Fprint(out, "Install faster-whisper into DigCrawl's isolated local ASR runtime? This may download Python packages but will not download a model or modify system Python. Continue? [y/N]: ")
	} else {
		fmt.Fprintf(out, "Install faster-whisper and download local ASR model %q into DigCrawl's isolated runtime? This may download Python packages and model files but does not modify system Python. Continue? [y/N]: ", model)
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// runASRCheck is the readiness check paired with setup and transcribe. The
// general `digcrawl doctor` validates an MCP Skill profile; it intentionally
// does not claim readiness for a root ASR configuration.
func runASRCheck(args []string) {
	fs := flag.NewFlagSet("asr check", flag.ExitOnError)
	configPath := fs.String("config", "", "path to root config.yaml or Skill profile; defaults to project config, then the user Skill profile")
	scope := fs.String("scope", "auto", "ASR configuration scope: auto | root | skill")
	_ = fs.Parse(args)
	target, err := loadASRTarget(*configPath, *scope)
	if err != nil {
		log.Fatalf("load ASR configuration: %v", err)
	}
	if target.settings.ProviderOrDefault() != "local" {
		log.Fatalf("ASR check: selected %s scope uses provider %q; local model readiness is available only for provider local", target.scope, target.settings.ProviderOrDefault())
	}
	readiness, err := asr.CheckReadiness(target.settings)
	if err != nil {
		log.Fatalf("ASR check (%s scope): %v", target.scope, err)
	}
	if !readiness.OfflineModelVerified {
		fmt.Printf("Local ASR command is available (%s scope), but its runtime and model are externally managed and were not verified.\n", target.scope)
		return
	}
	fmt.Printf("Local ASR (%s scope) is ready for offline transcription: model %q in %s\n", target.scope, target.settings.ModelOrDefault(), target.settings.ModelCacheDirOrDefault())
}

func runASRTranscribe(args []string) {
	fs := flag.NewFlagSet("asr transcribe", flag.ExitOnError)
	configPath := fs.String("config", "", "path to root config.yaml or Skill profile; defaults to project config, then the user Skill profile")
	scope := fs.String("scope", "auto", "ASR configuration scope: auto | root | skill")
	timeoutSec := fs.Int("timeout", 0, "operation timeout floor in seconds; 0 uses ASR.TimeoutSec, always bounded by ASR.MaxTimeoutSec")
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
	target, err := loadASRTarget(*configPath, *scope)
	if err != nil {
		log.Fatalf("load ASR configuration: %v", err)
	}
	asrCfg := target.settings
	if *timeoutSec > 0 {
		asrCfg.TimeoutSec = *timeoutSec
	}
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
		"-scope":            {},
		"--scope":           {},
		"-timeout":          {},
		"--timeout":         {},
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
