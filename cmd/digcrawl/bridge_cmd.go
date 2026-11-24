package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/transport/bridge"
)

const (
	bridgeStopTimeout  = 25 * time.Second
	bridgeStartTimeout = 10 * time.Second
)

// runBridge owns the private Bridge lifecycle. Normal users invoke
// `digcrawl mcp`; its child starts the Bridge only when needed, while stop and
// restart provide the explicit release-management path for one profile.
func runBridge(args []string) {
	if len(args) == 0 {
		log.Fatal("bridge: expected `serve`, `stop`, `restart`, `status`, or `watch`")
	}
	switch args[0] {
	case "status":
		runBridgeStatus(args[1:])
	case "watch":
		runBridgeWatch(args[1:])
	case "serve":
		runBridgeServe(args[1:])
	case "stop":
		runBridgeStop(args[1:], false)
	case "restart":
		runBridgeStop(args[1:], true)
	default:
		log.Fatal("bridge: expected `serve`, `stop`, `restart`, `status`, or `watch`")
	}
}

func runBridgeServe(args []string) {
	fs := flag.NewFlagSet("bridge serve", flag.ExitOnError)
	explicitRestart := fs.Bool("explicit-restart", false, "internal operator-requested start")
	managedWorker := fs.Bool("managed-worker", false, "internal bridge worker")
	configPath := fs.String("config", "", "path to profile")
	socketPath := fs.String("socket", "", "private Unix socket path")
	startupStatus := fs.String("startup-status", "", "private startup diagnostic path")
	_ = fs.Parse(args)
	reportFailure := func(err error) {
		writeBridgeStartupFailure(*startupStatus, err)
	}
	cfg, err := loadRequiredConfig(*configPath)
	if err != nil {
		reportFailure(err)
		bridge.RecordStartupFailure(*configPath, "config", err)
		log.Fatalf("bridge: load profile: %v", err)
	}
	allowDefaultSkillProfileTools(cfg)
	if !*managedWorker {
		log.Fatal("bridge serve is internal; use `digcrawl mcp` to start the local Bridge")
	}
	if err := bridge.ValidateManagedWorkerIsolation(); err != nil {
		reportFailure(err)
		log.Fatalf("bridge: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serve := bridge.Serve
	if *explicitRestart {
		serve = bridge.ServeAfterExplicitRestart
	}
	if err := serve(ctx, cfg, *socketPath); err != nil && !errors.Is(err, bridge.ErrAlreadyRunning) {
		reportFailure(err)
		log.Fatalf("bridge: %v", err)
	}
}

func runBridgeStop(args []string, restart bool) {
	command := "stop"
	if restart {
		command = "restart"
	}
	fs := flag.NewFlagSet("bridge "+command, flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile")
	_ = fs.Parse(args)
	if strings.TrimSpace(*configPath) == "" {
		log.Fatalf("bridge %s: --config is required", command)
	}
	guard, err := bridge.AcquireControl()
	if err != nil {
		log.Fatal(err)
	}
	defer guard.Close()
	canonical, err := browser.CanonicalProfilePath(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	*configPath = canonical
	if err := bridge.Pause(*configPath, true); err != nil {
		log.Fatal(err)
	}
	stopCtx, cancelStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelStop()
	stopped, err := bridge.Stop(stopCtx, *configPath)
	cancelStop()
	if err != nil {
		log.Fatalf("bridge %s: %v", command, err)
	}
	if stopped {
		log.Printf("DigCrawl Bridge stopped for %s", filepath.Clean(*configPath))
	}
	if !restart {
		return
	}
	cfg, err := loadRequiredConfig(*configPath)
	if err != nil {
		log.Fatalf("bridge restart: load profile: %v", err)
	}
	allowDefaultSkillProfileTools(cfg)
	if err := bridge.Pause(*configPath, false); err != nil {
		log.Fatal(err)
	}
	// Restart stops, then starts; it is not a handoff. Starting
	// gets its own complete budget after the old Bridge has released ownership.
	startCtx, cancelStart := context.WithTimeout(context.Background(), bridgeStartTimeout)
	defer cancelStart()
	if _, err := bridge.NewExecutorAfterRestart(startCtx, bridgeExecutable(), *configPath, cfg); err != nil {
		log.Fatalf("bridge restart: %v", err)
	}
	log.Printf("DigCrawl Bridge restarted for %s", filepath.Clean(*configPath))
}

func writeBridgeStartupFailure(path string, cause error) {
	path = strings.TrimSpace(path)
	if path == "" || cause == nil {
		return
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bridge-startup-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	// Keep only controlled failure text in the cross-process handoff. YAML
	// parse errors and raw exceptions can include credentials or signed URLs.
	message := "local Bridge startup failed; run doctor --config <actual profile> and check configuration and local ownership"
	for _, known := range []error{bridge.ErrBrowserProfileInUse, bridge.ErrAlreadyRunning, bridge.ErrBuildMismatch, bridge.ErrRecoveryExhausted, bridge.ErrBridgeStopping} {
		if errors.Is(cause, known) {
			message = known.Error()
			break
		}
	}
	if _, err := tmp.WriteString(message); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmp.Name(), path)
}

func runBridgeStatus(args []string) {
	fs := flag.NewFlagSet("bridge status", flag.ExitOnError)
	profile := fs.String("config", "", "actual profile path")
	_ = fs.Parse(args)
	if *profile == "" {
		log.Fatal("bridge status: --config is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := bridge.Inspect(ctx, *profile)
	if err != nil {
		log.Fatal(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(status)
	if !status.Ready || status.Paused || status.Stopping {
		os.Exit(1)
	}
}

func runBridgeWatch(args []string) {
	fs := flag.NewFlagSet("bridge watch", flag.ExitOnError)
	profile := fs.String("config", "", "actual profile path")
	install := fs.Bool("install", false, "install this profile watcher as a macOS LaunchAgent")
	remove := fs.Bool("uninstall", false, "remove this profile watcher LaunchAgent")
	_ = fs.Parse(args)
	if *profile == "" {
		log.Fatal("bridge watch: --config is required")
	}
	if *install || *remove {
		guard, err := bridge.AcquireControl()
		if err != nil {
			log.Fatal(err)
		}
		defer guard.Close()
	}
	canonical, err := browser.CanonicalProfilePath(*profile)
	if err != nil {
		log.Fatal(err)
	}
	*profile = canonical
	if *install && *remove {
		log.Fatal("select either --install or --uninstall")
	}
	if *remove {
		if err := configureBridgeWatch(*profile, true); err != nil {
			log.Fatal(err)
		}
		return
	}
	cfg, err := loadRequiredConfig(*profile)
	if err != nil {
		log.Fatal(err)
	}
	if *install {
		if err := configureBridgeWatch(*profile, false); err != nil {
			log.Fatal(err)
		}
		return
	}
	allowDefaultSkillProfileTools(cfg)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := bridge.Watch(ctx, bridgeExecutable(), *profile, cfg); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
