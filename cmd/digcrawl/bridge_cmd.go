package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
		log.Fatal("bridge: expected `serve`, `stop`, or `restart`")
	}
	switch args[0] {
	case "serve":
		runBridgeServe(args[1:])
	case "stop":
		runBridgeStop(args[1:], false)
	case "restart":
		runBridgeStop(args[1:], true)
	default:
		log.Fatal("bridge: expected `serve`, `stop`, or `restart`")
	}
}

func runBridgeServe(args []string) {
	fs := flag.NewFlagSet("bridge serve", flag.ExitOnError)
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
	if err := bridge.Serve(ctx, cfg, *socketPath); err != nil && !errors.Is(err, bridge.ErrAlreadyRunning) {
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
	stopCtx, cancelStop := context.WithTimeout(context.Background(), bridgeStopTimeout)
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
	// Restart stops, then starts; it is not a handoff. Starting
	// gets its own complete budget after the old Bridge has released ownership.
	startCtx, cancelStart := context.WithTimeout(context.Background(), bridgeStartTimeout)
	defer cancelStart()
	if _, err := bridge.NewExecutor(startCtx, bridgeExecutable(), *configPath, cfg); err != nil {
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
	message := strings.ReplaceAll(strings.TrimSpace(cause.Error()), "\n", " ")
	if len(message) > 1024 {
		message = message[:1024]
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
