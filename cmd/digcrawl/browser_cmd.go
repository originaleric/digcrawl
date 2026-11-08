package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/internal/platform/healthcheck"
	watchlistservice "github.com/originaleric/digcrawl/internal/watchlist/service"
	"github.com/originaleric/digcrawl/transport/bridge"
)

func runBrowser(args []string) {
	if len(args) < 1 {
		printBrowserUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "install":
		runBrowserInstall(args[1:])
	case "runtime":
		runBrowserRuntime(args[1:])
	case "migrate":
		runBrowserMigrate(args[1:])
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
  digcrawl browser install [--archive-dir DIR] [--repair] [--migrate-default]
  digcrawl browser runtime [--interactive] [--expect PATH]
  digcrawl browser migrate --config PATH [--apply]
  digcrawl browser login --platform xiaohongshu|douyin|toutiao|threads|instagram|youtube
  digcrawl browser check --platform xiaohongshu|douyin|toutiao|threads|instagram|youtube [--url URL] [--trial]

不传 --config 时，优先使用当前项目的 config/config.yaml，其次使用用户级 Skill profile；定制 profile 请显式传 --config。
`)
}

func runBrowserLogin(args []string) {
	fs := flag.NewFlagSet("browser login", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile (default: project config, then user skill profile)")
	platform := fs.String("platform", "", "platform: xiaohongshu, douyin, toutiao, threads, instagram, youtube")
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
	platform := fs.String("platform", "", "platform: xiaohongshu, douyin, toutiao, threads, instagram, youtube")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A running Bridge owns the persistent background profile. The CLI check
	// reads the same saved cookies using its own short-lived browser/profile,
	// so checking a session cannot compete with ongoing MCP work.
	profile, err := os.MkdirTemp("", "digcrawl-browser-check-")
	if err != nil {
		log.Fatalf("check: prepare temporary profile: %v", err)
	}
	cfg.LocalBrowser = browser.ManagedConfig(cfg.LocalBrowser)
	cfg.LocalBrowser.UserDataDir = profile
	checkErr := healthcheck.Check(ctx, healthcheck.Options{
		Platform:  *platform,
		ProbeURL:  *probeURL,
		TrialRead: *trial,
	})
	cleanupCtx, finish := context.WithTimeout(context.Background(), bridgeStopTimeout)
	cleanupErr := browser.ShutdownImmediately(cleanupCtx)
	finish()
	if cleanupErr == nil {
		cleanupErr = os.RemoveAll(profile)
	} else {
		cleanupErr = fmt.Errorf("temporary check profile retained at %s: %w", profile, cleanupErr)
	}
	if err := errors.Join(checkErr, cleanupErr); err != nil {
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

func runBrowserInstall(args []string) {
	fs := flag.NewFlagSet("browser install", flag.ExitOnError)
	archiveDir := fs.String("archive-dir", strings.TrimSpace(os.Getenv("DIGCRAWL_BROWSER_ARCHIVE_DIR")), "directory of official pinned runtime ZIPs for offline installation")
	var installerLocks []string
	fs.Func("installer-lock", "internal resource directory to guard; repeat for shared targets", func(path string) error { installerLocks = append(installerLocks, path); return nil })
	repair := fs.Bool("repair", false, "preserve and replace a damaged runtime after stopping its owners")
	migrateDefault := fs.Bool("migrate-default", false, "prepare the standard profile before an installer replaces the CLI")
	_ = fs.Parse(args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var release chan bool
	holding := len(installerLocks) > 0
	multiResource := len(installerLocks) > 1
	if holding || *migrateDefault {
		profile, err := browser.CanonicalProfilePath(config.DefaultSkillProfilePath())
		if err != nil {
			log.Fatal(err)
		}
		installerLocks = append(installerLocks, filepath.Dir(profile))
		unique := map[string]bool{}
		for _, path := range installerLocks {
			canonical, err := browser.CanonicalProfilePath(path)
			if err != nil {
				log.Fatal(err)
			}
			unique[canonical] = true
		}
		paths := make([]string, 0, len(unique))
		for path := range unique {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		var guards []*browser.ProfileLock
		for _, path := range paths {
			guard, err := browser.AcquireInstallLock(path)
			if err != nil {
				for _, held := range guards {
					held.Close()
				}
				log.Fatalf("another installation is active: %v", err)
			}
			guards = append(guards, guard)
		}
		defer func() {
			for i := len(guards) - 1; i >= 0; i-- {
				guards[i].Close()
			}
		}()
	}
	if holding {
		// Parent death closes its pipe, cancelling downloads as well as releasing
		// the existing OS lock. No stale file is used as proof of a live owner.
		release = make(chan bool, 1)
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			released := scanner.Scan() && scanner.Text() == "release"
			release <- released
			stop()
		}()
	}
	install := browser.InstallBrowserRuntime
	if *repair {
		install = browser.RepairBrowserRuntime
	}
	if err := install(ctx, *archiveDir); err != nil {
		log.Fatal(err)
	}
	if *migrateDefault {
		if err := migrateDefaultBrowserProfile(ctx); err != nil {
			log.Fatal(err)
		}
	}
	if holding {
		if multiResource {
			fmt.Println("DIGCRAWL_INSTALL_READY_V2")
		} else {
			fmt.Println("DIGCRAWL_INSTALL_READY")
		}
		<-ctx.Done()
		select {
		case released := <-release:
			if released {
				fmt.Println("DIGCRAWL_INSTALL_RELEASED")
				return
			}
		default:
		}
		log.Fatal("installation lock owner interrupted before release")
	}
	fmt.Println("DigCrawl isolated background and login browsers are ready.")
}
func runBrowserRuntime(args []string) {
	fs := flag.NewFlagSet("browser runtime", flag.ExitOnError)
	interactive := fs.Bool("interactive", false, "resolve isolated headed login browser")
	expect := fs.String("expect", "", "reject a legacy service path that differs from the pinned runtime")
	_ = fs.Parse(args)
	path, err := browser.RuntimeExecutable(*interactive)
	if err != nil {
		log.Fatal(err)
	}
	if *expect != "" {
		actual, e := filepath.Abs(*expect)
		if e != nil || actual != path {
			log.Fatal("legacy desktop Chrome override rejected; reinstall the CDP service with the isolated runtime")
		}
	}
	fmt.Println(path)
}
func runBrowserMigrate(args []string) {
	fs := flag.NewFlagSet("browser migrate", flag.ExitOnError)
	profile := fs.String("config", "", "existing profile to inspect; repeat for each embedded installation")
	apply := fs.Bool("apply", false, "stop only verified Bridge owners for this profile")
	_ = fs.Parse(args)
	if strings.TrimSpace(*profile) == "" {
		log.Fatal("browser migrate requires --config with the actual caller profile")
	}
	path, err := browserProfilePath(*profile)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		log.Fatal(err)
	}
	socket, err := bridge.SocketPath(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Profile: %s\nBridge socket: %s\nLegacy ChromePath: %s\nLegacy UserDataDir: %s\n", path, socket, cfg.LocalBrowser.ChromePath, cfg.LocalBrowser.UserDataDir)
	inspectCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
	owners, inspectErr := bridge.InspectOwners(inspectCtx, path)
	finish()
	if inspectErr != nil {
		log.Fatalf("migration ownership inspection failed: %v", inspectErr)
	}
	for _, owner := range owners {
		fmt.Printf("Owner PID=%d executable=%q verified=%v\n", owner.PID, owner.Executable, owner.Verified)
	}
	if !*apply {
		fmt.Println("Read-only. Stop verified old owners with browser migrate --config PATH --apply. Then remove desktop ChromePath overrides, install the isolated runtime, and restart all callers (including embedded copies). Existing CookieStoreDir is retained; dedicated login profiles may need sign-in again.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeStopTimeout)
	defer cancel()
	stopped, err := bridge.Stop(ctx, path)
	if err != nil {
		log.Fatal(err)
	}
	if err := config.MigrateBrowserRuntimeProfile(ctx, path); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Verified Bridge owners stopped: %v. Legacy executable override cleared; original profile backed up. No daily browser was targeted. Restart callers with the updated binary.\n", stopped)
}

// Shared by package installers that cannot embed platform/profile path logic.
// Runtime readiness precedes profile mutation and executable replacement.
func migrateDefaultBrowserProfile(ctx context.Context) error {
	path := config.DefaultSkillProfilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	stopCtx, cancel := context.WithTimeout(ctx, bridgeStopTimeout)
	defer cancel()
	if _, err := bridge.Stop(stopCtx, path); err != nil {
		return err
	}
	return config.MigrateBrowserRuntimeProfile(stopCtx, path)
}
