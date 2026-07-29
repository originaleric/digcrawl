package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/internal/watchlist"
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
	case "source":
		runWatchlistSource(args[1:])
	default:
		log.Fatalf("watchlist: unknown subcommand %q", args[0])
	}
}

func runWatchlistRun(args []string) {
	fs := flag.NewFlagSet("watchlist run", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	once := fs.Bool("once", false, "run one watchlist round and exit")
	id := fs.String("id", "", "run one configured watchlist item by id")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if !cfg.Watchlist.EnabledOrDefault() {
		log.Fatal("watchlist: disabled; set Watchlist.Enabled=true")
	}
	svc, err := watchlistservice.NewService(cfg)
	if err != nil {
		log.Fatalf("watchlist service: %v", err)
	}
	defer svc.Close()
	if err := requireConfiguredWatchlistItems(context.Background(), svc); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run := func() error {
		if strings.TrimSpace(*id) != "" {
			s, err := svc.RunConfiguredItem(ctx, *id)
			logWatchlistRunSummary(s)
			if err != nil {
				log.Printf("watchlist run error: %v", err)
			}
			return err
		}
		summaries, err := svc.RunOnce(ctx)
		for _, s := range summaries {
			logWatchlistRunSummary(s)
		}
		if err != nil {
			log.Printf("watchlist run error: %v", err)
		}
		return err
	}
	if *once {
		if err := run(); err != nil {
			os.Exit(1)
		}
		return
	}
	run()
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

func logWatchlistRunSummary(s watchlistservice.RunSummary) {
	suppressed := ""
	if s.NoUpdateSuppressed {
		suppressed = s.NoUpdateSuppressedReason
	}
	log.Printf("watchlist run=%s watch=%s discovered=%d new=%d collected=%d summarized=%d notified=%d locked=%v no_update_suppressed=%v no_update_suppressed_reason=%q errors=%s",
		s.RunID, s.WatchID, s.Discovered, s.NewItems, s.Collected, s.Summarized, s.Notified, s.SkippedLocked, s.NoUpdateSuppressed, suppressed, strings.Join(s.Errors, " | "))
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
	svc, err := watchlistservice.NewService(cfg)
	if err != nil {
		log.Fatalf("watchlist service: %v", err)
	}
	defer svc.Close()
	if err := requireConfiguredWatchlistItems(context.Background(), svc); err != nil {
		log.Fatal(err)
	}

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

func requireConfiguredWatchlistItems(ctx context.Context, svc interface {
	ConfiguredItemCount(context.Context) (int, error)
}) error {
	n, err := svc.ConfiguredItemCount(ctx)
	if err != nil {
		return fmt.Errorf("watchlist: count configured items: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("watchlist: no items configured")
	}
	return nil
}

func runWatchlistSource(args []string) {
	if len(args) == 0 {
		log.Fatal("watchlist source: expected subcommand list|add|enable|disable|delete|test-discover")
	}
	switch args[0] {
	case "list":
		runWatchlistSourceList(args[1:])
	case "add":
		runWatchlistSourceAdd(args[1:])
	case "enable":
		runWatchlistSourceSetEnabled(args[1:], true)
	case "disable":
		runWatchlistSourceSetEnabled(args[1:], false)
	case "delete":
		runWatchlistSourceDelete(args[1:])
	case "test-discover":
		runWatchlistSourceTestDiscover(args[1:])
	default:
		log.Fatalf("watchlist source: unknown subcommand %q", args[0])
	}
}

func openWatchlistSourceStore(configPath string) (*watchlist.Store, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return watchlistservice.OpenConfiguredStore(cfg)
}

func runWatchlistSourceList(args []string) {
	fs := flag.NewFlagSet("watchlist source list", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	enabledOnly := fs.Bool("enabled-only", false, "only list enabled sources")
	_ = fs.Parse(args)
	st, err := openWatchlistSourceStore(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	sources, err := st.ListSources(context.Background(), *enabledOnly)
	if err != nil {
		log.Fatal(err)
	}
	out := make([]watchlistSourceView, 0, len(sources))
	for _, src := range sources {
		out = append(out, watchlistSourceView{
			ID: src.ID, Name: src.Name, Platform: src.Platform, Kind: src.Type, URL: src.URL,
			Enabled: src.Enabled, ScheduleMode: src.ScheduleMode, Every: src.Interval.String(),
			NotifyTo: src.NotifyTo, Options: src.Options,
		})
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
}

type watchlistSourceView struct {
	ID           string         `json:"id"`
	Name         string         `json:"name,omitempty"`
	Platform     string         `json:"platform"`
	Kind         string         `json:"kind"`
	URL          string         `json:"url"`
	Enabled      bool           `json:"enabled"`
	ScheduleMode string         `json:"schedule_mode"`
	Every        string         `json:"every"`
	NotifyTo     string         `json:"notify_to,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
}

func runWatchlistSourceAdd(args []string) {
	fs := flag.NewFlagSet("watchlist source add", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	id := fs.String("id", "", "source id")
	name := fs.String("name", "", "source display name")
	platform := fs.String("platform", "", "source platform, e.g. reddit/x/toutiao/bilibili/weibo/kuaishou/zhihu/rss")
	kind := fs.String("kind", "", "source kind, e.g. rss/sitemap/reddit_subreddit/reddit_user/reddit_post_comments/toutiao_author/toutiao_article/toutiao_video/x_user/x_post/douyin_user/douyin_video/xiaohongshu_user/xiaohongshu_note/wechat_list/wechat_article/bilibili_user/bilibili_video/weibo_user/weibo_post/kuaishou_user/kuaishou_video/zhihu_user/zhihu_answer/zhihu_article/zhihu_zvideo/web_list")
	sourceURL := fs.String("url", "", "source URL")
	every := fs.String("every", "6h", "scan interval")
	notify := fs.String("notify", "", "notification channel id")
	enabled := fs.Bool("enabled", true, "enable source")
	optionsRaw := fs.String("options", "", "JSON object options")
	_ = fs.Parse(args)
	interval, err := time.ParseDuration(*every)
	if err != nil {
		log.Fatalf("watchlist source add: invalid --every: %v", err)
	}
	options := map[string]any(nil)
	if strings.TrimSpace(*optionsRaw) != "" {
		if err := json.Unmarshal([]byte(*optionsRaw), &options); err != nil {
			log.Fatalf("watchlist source add: invalid --options JSON: %v", err)
		}
	}
	st, err := openWatchlistSourceStore(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSource(context.Background(), watchlist.WatchSource{
		ID: *id, Name: *name, Platform: *platform, Type: *kind, URL: *sourceURL,
		Enabled: *enabled, ScheduleMode: "interval", Interval: interval, NotifyTo: *notify, Options: options,
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("watchlist source %q saved", strings.TrimSpace(*id))
}

func runWatchlistSourceSetEnabled(args []string, enabled bool) {
	fs := flag.NewFlagSet("watchlist source enabled", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	id := fs.String("id", "", "source id")
	_ = fs.Parse(args)
	st, err := openWatchlistSourceStore(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSourceEnabled(context.Background(), *id, enabled); err != nil {
		log.Fatal(err)
	}
	log.Printf("watchlist source %q enabled=%v", strings.TrimSpace(*id), enabled)
}

func runWatchlistSourceDelete(args []string) {
	fs := flag.NewFlagSet("watchlist source delete", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	id := fs.String("id", "", "source id")
	_ = fs.Parse(args)
	st, err := openWatchlistSourceStore(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.DeleteSource(context.Background(), *id); err != nil {
		log.Fatal(err)
	}
	log.Printf("watchlist source %q deleted", strings.TrimSpace(*id))
}

func runWatchlistSourceTestDiscover(args []string) {
	fs := flag.NewFlagSet("watchlist source test-discover", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	id := fs.String("id", "", "source id")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	svc, err := watchlistservice.NewDiscoverService(cfg)
	if err != nil {
		log.Fatalf("watchlist service: %v", err)
	}
	defer svc.Close()
	items, err := svc.DiscoverConfiguredItem(context.Background(), *id)
	if err != nil {
		log.Fatal(err)
	}
	out := make([]watchlistDiscoveredItemView, 0, len(items))
	for _, item := range items {
		out = append(out, watchlistDiscoveredItemView{
			ID: item.ID, ContentKey: item.ContentKey, URL: item.URL, CanonicalURL: item.CanonicalURL,
			Title: item.Title, Author: item.Author, PublishedAt: formatOptionalTime(item.PublishedAt),
			ContentType: item.ContentType, Raw: item.Raw,
		})
	}
	data, _ := json.MarshalIndent(map[string]any{"count": len(out), "items": out}, "", "  ")
	fmt.Println(string(data))
}

type watchlistDiscoveredItemView struct {
	ID           string         `json:"id,omitempty"`
	ContentKey   string         `json:"content_key"`
	URL          string         `json:"url"`
	CanonicalURL string         `json:"canonical_url,omitempty"`
	Title        string         `json:"title,omitempty"`
	Author       string         `json:"author,omitempty"`
	PublishedAt  string         `json:"published_at,omitempty"`
	ContentType  string         `json:"content_type,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
