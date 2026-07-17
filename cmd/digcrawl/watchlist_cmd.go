package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	watchlistservice "github.com/originaleric/digcrawl/internal/watchlist/service"
)

func runWatchlist(args []string) {
	if len(args) == 0 {
		log.Fatal("watchlist: expected subcommand `run`")
	}
	switch args[0] {
	case "run":
		runWatchlistRun(args[1:])
	case "serve":
		runWatchlistServe(args[1:])
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
	if watchlistservice.ConfiguredItemCount(cfg) == 0 {
		log.Fatal("watchlist: no items configured")
	}
	svc, err := watchlistservice.NewService(cfg)
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

func runWatchlistServe(args []string) {
	fs := flag.NewFlagSet("watchlist serve", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if !cfg.Watchlist.EnabledOrDefault() {
		log.Fatal("watchlist: disabled; set Watchlist.Enabled=true")
	}
	if !cfg.Watchlist.Scheduler.EnabledOrDefault() {
		log.Fatal("watchlist scheduler: disabled; set Watchlist.Scheduler.Enabled=true")
	}
	if watchlistservice.ConfiguredItemCount(cfg) == 0 {
		log.Fatal("watchlist: no items configured")
	}
	svc, err := watchlistservice.NewService(cfg)
	if err != nil {
		log.Fatalf("watchlist service: %v", err)
	}
	defer svc.Close()

	interval, err := time.ParseDuration(cfg.Watchlist.Scheduler.TickIntervalOrDefault())
	if err != nil || interval <= 0 {
		interval = 5 * time.Minute
	}
	jitter, err := time.ParseDuration(cfg.Watchlist.Scheduler.JitterOrDefault())
	if err != nil || jitter < 0 {
		jitter = 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	runDue := func() {
		if jitter > 0 {
			delay := time.Duration(rng.Int63n(int64(jitter)))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		summaries, err := svc.RunDue(ctx, time.Now())
		for _, s := range summaries {
			log.Printf("watchlist due run=%s watch=%s discovered=%d new=%d collected=%d summarized=%d notified=%d locked=%v errors=%s",
				s.RunID, s.WatchID, s.Discovered, s.NewItems, s.Collected, s.Summarized, s.Notified, s.SkippedLocked, strings.Join(s.Errors, " | "))
		}
		if err != nil {
			log.Printf("watchlist due error: %v", err)
		}
	}
	runDue()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runDue()
		}
	}
}
