package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/originaleric/digcrawl/transport/bridge"
)

// runBridge is intentionally a narrow internal command. Normal users invoke
// `digcrawl mcp`; the MCP child starts this private bridge only when needed.
func runBridge(args []string) {
	if len(args) == 0 || args[0] != "serve" {
		log.Fatal("bridge: expected `serve --config <profile> --socket <path>`")
	}
	fs := flag.NewFlagSet("bridge serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile")
	socketPath := fs.String("socket", "", "private Unix socket path")
	idleTimeout := fs.Duration("idle-timeout", bridge.DefaultIdleTimeout, "exit after this much time without tool calls or artifact delivery")
	_ = fs.Parse(args[1:])
	if *idleTimeout <= 0 {
		log.Fatal("bridge: --idle-timeout must be positive")
	}
	cfg, err := loadRequiredConfig(*configPath)
	if err != nil {
		log.Fatalf("bridge: load profile: %v", err)
	}
	allowDefaultSkillProfileTools(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.Serve(ctx, cfg, *socketPath, *idleTimeout); err != nil && !errors.Is(err, bridge.ErrAlreadyRunning) {
		log.Fatalf("bridge: %v", err)
	}
}
