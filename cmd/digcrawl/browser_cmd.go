package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform/healthcheck"
	watchlistservice "github.com/originaleric/digcrawl/internal/watchlist/service"
)

func runBrowser(args []string) {
	if len(args) < 1 {
		printBrowserUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "login":
		runBrowserLogin(args[1:])
	case "check":
		runBrowserCheck(args[1:])
	default:
		printBrowserUsage()
		os.Exit(1)
	}
}

func printBrowserUsage() {
	fmt.Fprintf(os.Stderr, `浏览器会话命令:
  digcrawl browser login --platform xiaohongshu|douyin|toutiao|threads|instagram
  digcrawl browser check --platform xiaohongshu|douyin|toutiao|threads|instagram [--url URL] [--trial]

不传 --config 时，优先使用当前项目的 config/config.yaml，其次使用用户级 Skill profile；定制 profile 请显式传 --config。
`)
}

func runBrowserLogin(args []string) {
	fs := flag.NewFlagSet("browser login", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile (default: project config, then user skill profile)")
	platform := fs.String("platform", "", "platform: xiaohongshu, douyin, toutiao, threads, instagram")
	_ = fs.Parse(args)

	path, err := browserProfilePath(*configPath)
	if err != nil {
		log.Fatalf("login: %v", err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(*platform) == "" {
		log.Fatal("--platform is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := browser.InteractiveLogin(ctx, *platform, cfg.LocalBrowser); err != nil {
		if cfg.Watchlist.LoginRecovery.EnabledOrDefault() {
			log.Fatalf("login: %v；自动回补未登记", err)
		}
		log.Fatalf("login: %v", err)
	}
	if err := recordWatchlistLogin(cfg, *platform); err != nil {
		log.Fatalf("login: Cookie 已保存，但自动回补未登记: %v", err)
	}
}

func recordWatchlistLogin(cfg *config.Config, platform string) error {
	if !cfg.Watchlist.LoginRecovery.EnabledOrDefault() {
		return nil
	}
	store, err := watchlistservice.OpenConfiguredStore(cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.RecordLogin(context.Background(), platform, time.Now())
}

func runBrowserCheck(args []string) {
	fs := flag.NewFlagSet("browser check", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile (default: project config, then user skill profile)")
	platform := fs.String("platform", "", "platform: xiaohongshu, douyin, toutiao, threads, instagram")
	probeURL := fs.String("url", "", "optional probe URL for trial read")
	trial := fs.Bool("trial", false, "require trial read (needs --url or Platform.HealthProbes)")
	_ = fs.Parse(args)

	path, err := browserProfilePath(*configPath)
	if err != nil {
		log.Fatalf("check: %v", err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(*platform) == "" {
		log.Fatal("--platform is required")
	}
	_ = cfg

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := healthcheck.Check(ctx, healthcheck.Options{
		Platform:  *platform,
		ProbeURL:  *probeURL,
		TrialRead: *trial,
	}); err != nil {
		log.Fatalf("check: %v", err)
	}
}

// browserProfilePath makes browser commands work from a DigCrawl checkout
// without flags, while retaining the standard user Skill profile fallback and
// explicit custom-profile support.
func browserProfilePath(explicitPath string) (string, error) {
	if path := strings.TrimSpace(explicitPath); path != "" {
		return path, nil
	}
	for _, path := range []string{"config/config.yaml", config.DefaultSkillProfilePath()} {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return "", fmt.Errorf("config path is a directory: %s", path)
			}
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect config %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("no default config found at config/config.yaml or %s; run `digcrawl init --profile skill` or pass --config", config.DefaultSkillProfilePath())
}
