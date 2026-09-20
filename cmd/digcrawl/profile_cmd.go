package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/runtime"
	"gopkg.in/yaml.v3"
)

var skillReadTools = []string{
	"gateway.ping",
	"skill.asr.enable",
	"skill.media.configure",
	"browser.browse",
	"wechat.article.read",
	"xiaohongshu.note.read",
	"douyin.video.read",
	"toutiao.article.read",
	"toutiao.video.read",
	"bilibili.video.read",
	"weibo.post.read",
	"kuaishou.video.read",
	"zhihu.content.read",
	"youtube.video.read",
	"x.post.read",
	"reddit.post.read",
	"reddit.subreddit.read",
	"reddit.user.read",
	"threads.post.read",
	"instagram.content.read",
	"feed.read",
}

// legacySkillReadTools is the shipped Skill allowlist before ASR opt-in and
// YouTube support. Keep this list frozen: deriving it from skillReadTools
// would let future default-tool additions change which user policies qualify
// for a backward-compatible migration.
var legacySkillReadTools = []string{
	"gateway.ping",
	"browser.browse",
	"wechat.article.read",
	"xiaohongshu.note.read",
	"douyin.video.read",
	"toutiao.article.read",
	"toutiao.video.read",
	"bilibili.video.read",
	"weibo.post.read",
	"kuaishou.video.read",
	"zhihu.content.read",
	"x.post.read",
	"reddit.post.read",
	"reddit.subreddit.read",
	"reddit.user.read",
	"threads.post.read",
	"instagram.content.read",
	"feed.read",
}

// preSkillMediaConfigureTools is the shipped default Skill allowlist before
// user-controlled media retention and download budgets were configurable from
// the host Agent. It is deliberately built from the frozen pre-ASR/YouTube
// list rather than skillReadTools, so a later default tool cannot broaden an
// operator-customized allowlist by accident.
var preSkillMediaConfigureTools = append(append([]string{}, legacySkillReadTools...), "skill.asr.enable", "youtube.video.read")

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	profile := fs.String("profile", "", "profile to create (currently: skill)")
	force := fs.Bool("force", false, "replace an existing profile")
	_ = fs.Parse(args)
	if *profile != "skill" {
		log.Fatal("init: --profile skill is required")
	}
	path := config.DefaultSkillProfilePath()
	if err := writeSkillProfile(path, *force); err != nil {
		log.Fatalf("init: %v", err)
	}
	fmt.Printf("created profile: %s\n", path)
}

func runConfig(args []string) {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprint(os.Stderr, "用法: digcrawl config validate --config <profile>\n")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("config validate", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile")
	_ = fs.Parse(args[1:])
	if strings.TrimSpace(*configPath) == "" {
		log.Fatal("config validate: --config is required")
	}
	if _, err := validateProfile(*configPath); err != nil {
		log.Fatalf("config validate: %v", err)
	}
	fmt.Printf("profile valid: %s\n", *configPath)
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile")
	_ = fs.Parse(args)
	if strings.TrimSpace(*configPath) == "" {
		log.Fatal("doctor: --config is required")
	}
	cfg, err := loadDoctorProfile(*configPath)
	if err != nil {
		log.Fatalf("doctor: %v", err)
	}

	checks := []doctorCheck{
		{name: "binary", err: checkExecutable(os.Args[0])},
		{name: "profile", err: nil},
		{name: "artifact directory", err: checkWritableDirectory(cfg.Gateway.ArtifactDir)},
		// MCP uses the on-demand local bridge, which owns Chrome directly. Its
		// health must not depend on the optional legacy fixed-CDP service.
		{name: "browser", err: browser.CheckManagedAvailable(cfg.LocalBrowser)},
		{name: "Skill ffmpeg", err: checkSkillFFmpeg(cfg)},
		{name: "Skill ASR provider", err: checkSkillASRProvider(cfg)},
		{name: "mcp startup", err: checkMCPStartup(cfg)},
	}
	failed := false
	for _, check := range checks {
		if check.err != nil {
			failed = true
			fmt.Printf("FAIL %s: %v\n", check.name, check.err)
			continue
		}
		fmt.Printf("OK   %s\n", check.name)
	}
	if failed {
		os.Exit(1)
	}
}

// loadDoctorProfile keeps the generic doctor on the MCP Skill boundary. A
// root-only config has no MCP Skill runtime to validate; its ASR health check
// must use the shared ASR domain resolver instead.
func loadDoctorProfile(path string) (*config.Config, error) {
	cfg, err := validateProfile(path)
	if err != nil {
		return nil, err
	}
	if !cfg.HasSkillProfile {
		return nil, errors.New("profile has no Skill section; use `digcrawl asr check --config <path> --scope root` for root ASR readiness")
	}
	return cfg, nil
}

type doctorCheck struct {
	name string
	err  error
}

func defaultSkillProfile(baseDir string) map[string]any {
	return map[string]any{
		"Gateway": map[string]any{
			"InstanceID":             "digcrawl-skill-local",
			"AllowedDomains":         []string{},
			"AllowedTools":           skillReadTools,
			"ArtifactEnabled":        true,
			"ArtifactDir":            filepath.Join(baseDir, "artifacts"),
			"ArtifactTTLMinutes":     1440,
			"ArtifactMaxInlineBytes": 10 * 1024 * 1024,
			"RequireApproval":        true,
		},
		"LocalBrowser": map[string]any{
			"CookieStoreDir": filepath.Join(baseDir, "browser_cookies"),
		},
		"VideoDownload": map[string]any{
			"Dir":        filepath.Join(baseDir, "media"),
			"Providers":  map[string]bool{},
			"MaxBytes":   config.DefaultVideoDownloadMaxBytes,
			"TimeoutSec": config.DefaultVideoDownloadTimeoutSec,
		},
		"Skill": map[string]any{
			"Media": map[string]any{
				"DownloadMedia":         false,
				"RetainDownloadedVideo": false,
				"Dir":                   filepath.Join(baseDir, "media"),
				"MaxBytes":              128 * 1024 * 1024,
				"TimeoutSec":            config.DefaultSkillMediaTimeoutSec,
				"MaxASRCandidates":      config.DefaultSkillMaxASRCandidates,
			},
			"ASR": map[string]any{
				"Enabled":       false,
				"Provider":      "local",
				"MaxTimeoutSec": 1800,
				"ModelCacheDir": filepath.Join(baseDir, "asr_models"),
			},
		},
		"Providers": map[string]any{
			// The provider registers the confirmation-protected Skill setting
			// tool. asr.transcribe itself stays outside AllowedTools.
			"ASREnabled": true,
		},
	}
}

func writeSkillProfile(path string, force bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("profile path is empty")
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("profile already exists at %s; use --force to replace it", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	data, err := yaml.Marshal(defaultSkillProfile(filepath.Dir(path)))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func validateProfile(path string) (*config.Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("profile path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	cfg, err := config.Validate(path)
	if err != nil {
		return nil, fmt.Errorf("YAML: %w", err)
	}
	if !filepath.IsAbs(cfg.Gateway.ArtifactDir) {
		return nil, errors.New("Gateway.ArtifactDir must be an absolute path")
	}
	if !filepath.IsAbs(cfg.LocalBrowser.CookieStoreDir) {
		return nil, errors.New("LocalBrowser.CookieStoreDir must be an absolute path")
	}
	if cfg.Skill.Media.DownloadMedia && !filepath.IsAbs(cfg.Skill.Media.Dir) {
		return nil, errors.New("Skill.Media.Dir must be an absolute path when Skill.Media.DownloadMedia is enabled")
	}
	if cfg.Skill.ASR.EnabledOrDefault() && !cfg.Skill.Media.DownloadMedia {
		return nil, errors.New("Skill.ASR.Enabled requires Skill.Media.DownloadMedia=true")
	}
	if err := validateSkillASRConfig(cfg.Skill.ASR); err != nil {
		return nil, err
	}
	if cfg.Gateway.ArtifactEnabled != nil && *cfg.Gateway.ArtifactEnabled && cfg.Gateway.ArtifactMaxInlineBytes <= 0 {
		return nil, errors.New("Gateway.ArtifactMaxInlineBytes must be positive when artifacts are enabled")
	}
	return cfg, nil
}

func checkExecutable(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("executable path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func checkWritableDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("path is not absolute")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(path, ".digcrawl-doctor-")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

// checkSkillFFmpeg is intentionally scoped to the MCP Skill profile. Root
// ASR readiness is checked by `digcrawl asr check`, which shares setup and
// transcription's explicit domain resolver.
func checkSkillFFmpeg(cfg *config.Config) error {
	if !cfg.Skill.ASR.EnabledOrDefault() {
		return nil
	}
	path := cfg.Skill.ASR.FFmpegPathOrDefault()
	if strings.ContainsRune(path, filepath.Separator) {
		return checkExecutable(path)
	}
	if _, err := exec.LookPath(path); err != nil {
		return fmt.Errorf("%s is not available on PATH", path)
	}
	return nil
}

func validateSkillASRConfig(cfg config.ASRConfig) error {
	if !cfg.EnabledOrDefault() {
		return nil
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "", "api", "openai_compatible", "openai-compatible":
		if strings.TrimSpace(os.ExpandEnv(cfg.APIKey)) == "" {
			return errors.New("Skill.ASR.APIKey is required when Skill.ASR.Provider is api")
		}
		if !config.SupportedASRResponseFormat(cfg.ResponseFormatOrDefault()) {
			return fmt.Errorf("Skill.ASR.ResponseFormat %q is unsupported", cfg.ResponseFormat)
		}
	case "local", "faster_whisper", "faster-whisper", "whisper", "stub":
		return nil
	default:
		return fmt.Errorf("Skill.ASR.Provider %q is unsupported", cfg.Provider)
	}
	return nil
}

func checkSkillASRProvider(cfg *config.Config) error {
	if !cfg.Skill.ASR.EnabledOrDefault() || cfg.Skill.ASR.ProviderOrDefault() != "local" {
		return nil
	}
	return asr.CheckAvailable(cfg.Skill.ASR)
}

func checkMCPStartup(cfg *config.Config) error {
	_, err := runtime.NewRuntime(cfg)
	return err
}

func loadRequiredConfig(path string) (*config.Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("profile path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	return config.Load(path)
}

func profilePathForMCP(configPath string) string {
	if strings.TrimSpace(configPath) != "" {
		return configPath
	}
	return config.DefaultSkillProfilePath()
}
