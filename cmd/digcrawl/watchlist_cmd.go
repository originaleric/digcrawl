package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/internal/watchlist"
)

func runWatchlist(args []string) {
	if len(args) == 0 {
		log.Fatal("watchlist: expected subcommand `run`")
	}
	switch args[0] {
	case "run":
		runWatchlistRun(args[1:])
	default:
		log.Fatalf("watchlist: unknown subcommand %q", args[0])
	}
}

func runWatchlistRun(args []string) {
	fs := flag.NewFlagSet("watchlist run", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	once := fs.Bool("once", false, "run one watchlist round and exit")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if !cfg.Watchlist.EnabledOrDefault() {
		log.Fatal("watchlist: disabled; set Watchlist.Enabled=true")
	}
	if len(cfg.Watchlist.Items) == 0 {
		log.Fatal("watchlist: no items configured")
	}
	svc, err := watchlist.NewService(cfg)
	if err != nil {
		log.Fatalf("watchlist service: %v", err)
	}
	defer svc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run := func() {
		summaries, err := svc.RunOnce(ctx)
		for _, s := range summaries {
			log.Printf("watchlist run=%s watch=%s discovered=%d new=%d collected=%d summarized=%d notified=%d locked=%v errors=%s",
				s.RunID, s.WatchID, s.Discovered, s.NewItems, s.Collected, s.Summarized, s.Notified, s.SkippedLocked, strings.Join(s.Errors, " | "))
		}
		if err != nil {
			log.Printf("watchlist run error: %v", err)
		}
	}
	run()
	if *once {
		return
	}
	interval, err := time.ParseDuration(cfg.Watchlist.DefaultIntervalOrDefault())
	if err != nil || interval <= 0 {
		interval = 6 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
