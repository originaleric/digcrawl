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

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform/healthcheck"
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

不传 --config 时，使用用户级 Skill profile；定制 profile 请显式传 --config。
`)
}

func runBrowserLogin(args []string) {
	fs := flag.NewFlagSet("browser login", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile (default: user skill profile)")
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
		log.Fatalf("login: %v", err)
	}
}

func runBrowserCheck(args []string) {
	fs := flag.NewFlagSet("browser check", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile (default: user skill profile)")
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

// browserProfilePath aligns the short browser-login command with the standard
// MCP installation while retaining explicit custom-profile support.
func browserProfilePath(explicitPath string) (string, error) {
	if path := strings.TrimSpace(explicitPath); path != "" {
		return path, nil
	}
	path := config.DefaultSkillProfilePath()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect user Skill profile: %w", err)
	}
	return "", fmt.Errorf("user Skill profile not found at %s; run `digcrawl init --profile skill` or pass --config", path)
}
