package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/notify"
	"github.com/originaleric/digcrawl/internal/watchlist"
)

func boolPtr(v bool) *bool {
	return &v
}

func baseNotificationConfig() *config.Config {
	return &config.Config{
		Watchlist: config.WatchlistConfig{
			Enabled: boolPtr(true),
			Items: []config.WatchlistItem{{
				ID:      "source-1",
				URL:     "https://example.com/feed",
				Enabled: boolPtr(true),
			}},
		},
		Notifications: config.NotificationsConfig{
			Feishu: config.FeishuNotificationConfig{
				Enabled:        boolPtr(true),
				DefaultChannel: "default",
				Channels: []config.FeishuChannel{{
					ID:         "default",
					WebhookURL: "https://example.com/webhook",
				}},
			},
		},
	}
}

func TestValidateNotificationConfigAllowsDryRunWithoutFeishu(t *testing.T) {
	cfg := baseNotificationConfig()
	cfg.Watchlist.DryRun = boolPtr(true)
	cfg.Notifications.Feishu = config.FeishuNotificationConfig{}
	if err := validateNotificationConfig(cfg); err != nil {
		t.Fatalf("dry-run should not require feishu config: %v", err)
	}
}

func TestNewNotifierPrefersDryRunOverFeishu(t *testing.T) {
	cfg := baseNotificationConfig()
	cfg.Watchlist.DryRun = boolPtr(true)
	n, err := newNotifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := n.(notify.NoopNotifier); !ok {
		t.Fatalf("dry-run should use noop notifier, got %T", n)
	}
}

func TestValidateNotificationConfigRequiresFeishuChannel(t *testing.T) {
	cfg := baseNotificationConfig()
	cfg.Notifications.Feishu.Channels = nil
	err := validateNotificationConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "at least one channel") {
		t.Fatalf("expected missing channel error, got %v", err)
	}
}

func TestValidateNotificationConfigRequiresWebhookAfterEnvExpansion(t *testing.T) {
	cfg := baseNotificationConfig()
	cfg.Notifications.Feishu.Channels[0].WebhookURL = "${MISSING_FEISHU_WEBHOOK}"
	err := validateNotificationConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "webhook url is required") {
		t.Fatalf("expected webhook error, got %v", err)
	}
}

func TestValidateNotificationConfigValidatesItemNotifyTo(t *testing.T) {
	cfg := baseNotificationConfig()
	cfg.Watchlist.Items[0].NotifyTo = "missing-channel"
	err := validateNotificationConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "missing-channel") {
		t.Fatalf("expected missing NotifyTo channel error, got %v", err)
	}
}

func TestValidateNotificationConfigAcceptsEnvWebhook(t *testing.T) {
	t.Setenv("FEISHU_WEBHOOK_URL", "https://example.com/env-webhook")
	cfg := baseNotificationConfig()
	cfg.Notifications.Feishu.Channels[0].WebhookURL = "${FEISHU_WEBHOOK_URL}"
	if err := validateNotificationConfig(cfg); err != nil {
		t.Fatalf("env webhook should be accepted: %v", err)
	}
}

func TestNormalizeItemNormalizesIdentifiers(t *testing.T) {
	svc := &Service{cfg: &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}}}
	item, err := svc.normalizeItem(config.WatchlistItem{
		ID:       " source-1 ",
		Platform: " Douyin ",
		Type:     " Feed ",
		URL:      " https://example.com/feed ",
		Name:     " Source Name ",
		NotifyTo: " default ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "source-1" || item.NotifyTo != "default" || item.Platform != "douyin" || item.Type != "feed" || item.Name != "Source Name" {
		t.Fatalf("item not normalized: %+v", item)
	}
}

func TestNormalizeItemPreservesOptions(t *testing.T) {
	svc := &Service{cfg: &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}}}
	item, err := svc.normalizeItem(config.WatchlistItem{
		ID:       "reddit-local",
		Platform: "reddit",
		Type:     "reddit_subreddit",
		URL:      "https://www.reddit.com/r/golang/new/",
		Options:  map[string]any{"sort": "new", "limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Options["sort"] != "new" || item.Options["limit"] != 10 {
		t.Fatalf("options not preserved: %+v", item.Options)
	}
}

func TestConfiguredWatchlistItemsIncludesTasks(t *testing.T) {
	cfg := &config.Config{
		Watchlist: config.WatchlistConfig{
			Items: []config.WatchlistItem{{
				ID:       "legacy",
				Platform: "rss",
				Type:     "rss",
				URL:      "https://example.com/feed.xml",
			}},
		},
		Tasks: []config.TaskConfig{{
			ID:      "reddit-ai",
			Name:    "Reddit AI",
			Type:    "watchlist.collect",
			Enabled: boolPtr(true),
			Schedule: config.TaskScheduleConfig{
				Mode:  "interval",
				Every: "6h",
			},
			Source: config.TaskSourceConfig{
				Platform: "reddit",
				Kind:     "reddit_subreddit",
				URL:      "https://www.reddit.com/r/artificial/new/",
				Options:  map[string]any{"sort": "new"},
			},
			Notify:  config.TaskNotifyConfig{Channel: "feishu-default"},
			Options: map[string]any{"limit": 25},
		}},
	}
	items := configuredWatchlistItems(cfg)
	if len(items) != 2 {
		t.Fatalf("expected legacy item plus task item, got %d", len(items))
	}
	task := items[1]
	if task.ID != "reddit-ai" || task.Platform != "reddit" || task.Type != "reddit_subreddit" || task.Interval != "6h" || task.NotifyTo != "feishu-default" {
		t.Fatalf("task mapping mismatch: %+v", task)
	}
	if task.Options["sort"] != "new" || task.Options["limit"] != 25 {
		t.Fatalf("task options not merged: %+v", task.Options)
	}
}

func TestNormalizeTaskRejectsUnsupportedScheduleMode(t *testing.T) {
	cfg := &config.Config{
		Watchlist: config.WatchlistConfig{DefaultInterval: "6h"},
		Tasks: []config.TaskConfig{{
			ID:   "daily",
			Type: "watchlist.collect",
			Schedule: config.TaskScheduleConfig{
				Mode: "cron",
			},
			Source: config.TaskSourceConfig{Platform: "rss", Kind: "rss", URL: "https://example.com/feed.xml"},
		}},
	}
	items := configuredWatchlistItems(cfg)
	if len(items) != 1 {
		t.Fatalf("items len=%d", len(items))
	}
	svc := &Service{cfg: cfg}
	_, err := svc.normalizeItem(items[0])
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected unsupported schedule error, got %v", err)
	}
}

func TestNormalizeTaskRejectsUnsupportedTaskType(t *testing.T) {
	cfg := &config.Config{
		Watchlist: config.WatchlistConfig{DefaultInterval: "6h"},
		Tasks: []config.TaskConfig{{
			ID:     "unknown",
			Type:   "unknown.task",
			Source: config.TaskSourceConfig{Platform: "rss", URL: "https://example.com/feed.xml"},
		}},
	}
	items := configuredWatchlistItems(cfg)
	if len(items) != 1 {
		t.Fatalf("items len=%d", len(items))
	}
	svc := &Service{cfg: cfg}
	_, err := svc.normalizeItem(items[0])
	if err == nil || !strings.Contains(err.Error(), "unsupported task type") {
		t.Fatalf("expected unsupported task type error, got %v", err)
	}
}

func TestSummaryTextForVideoPrefersASR(t *testing.T) {
	feed := FeedItem{ContentType: "video"}
	out := map[string]any{
		"content_type": "video",
		"text":         "看更多视频，上今日头条。",
		"markdown":     "页面 Markdown",
	}
	got := summaryTextForCollectedItem(feed, out, "视频口播文案")
	if got != "视频口播文案" {
		t.Fatalf("expected video summary text to prefer ASR, got %q", got)
	}
}

func TestSummaryTextForArticlePrefersPageText(t *testing.T) {
	feed := FeedItem{ContentType: "article"}
	out := map[string]any{
		"content_type": "article",
		"text":         "文章正文",
	}
	got := summaryTextForCollectedItem(feed, out, "误传 ASR")
	if got != "文章正文" {
		t.Fatalf("expected article summary text to prefer page text, got %q", got)
	}
}

func TestConfiguredItemsLoadsDatabaseSources(t *testing.T) {
	ctx := context.Background()
	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSource(ctx, watchlist.WatchSource{
		ID: "reddit-db", Name: "Reddit DB", Platform: "reddit", Type: "reddit_subreddit",
		URL: "https://www.reddit.com/r/artificial/new/", Enabled: true, Interval: time.Hour,
		NotifyTo: "feishu-default", Options: map[string]any{"sort": "new"},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{cfg: &config.Config{Watchlist: config.WatchlistConfig{
		Source:          config.WatchlistSourceConfig{Mode: "database"},
		DefaultInterval: "6h",
	}}, store: st}
	items, err := svc.configuredItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "reddit-db" || items[0].Interval != "1h0m0s" {
		t.Fatalf("database items mismatch: %+v", items)
	}
}

func TestConfiguredItemsMixedModeLetsDatabaseOverrideYaml(t *testing.T) {
	ctx := context.Background()
	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSource(ctx, watchlist.WatchSource{
		ID: "same-id", Name: "DB Source", Platform: "reddit", Type: "reddit_subreddit",
		URL: "https://www.reddit.com/r/artificial/new/", Enabled: true, Interval: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{cfg: &config.Config{Watchlist: config.WatchlistConfig{
		Source:          config.WatchlistSourceConfig{Mode: "mixed"},
		DefaultInterval: "6h",
		Items: []config.WatchlistItem{{
			ID: "same-id", Platform: "rss", Type: "rss", URL: "https://example.com/feed.xml",
		}},
	}}, store: st}
	items, err := svc.configuredItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Platform != "reddit" || items[0].URL != "https://www.reddit.com/r/artificial/new/" {
		t.Fatalf("mixed override mismatch: %+v", items)
	}
}

func TestDiscoverConfiguredItemUsesConfiguredSourceWithoutPersisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a</link><guid>g1</guid></item><item><title>B</title><link>https://example.com/b</link><guid>g2</guid></item></channel></rss>`))
	}))
	defer srv.Close()

	cfg := &config.Config{Watchlist: config.WatchlistConfig{
		DefaultInterval: "6h",
		MaxItemsPerRun:  1,
		Items: []config.WatchlistItem{{
			ID: "rss-local", Platform: "rss", Type: "rss", URL: srv.URL, Enabled: boolPtr(true),
		}},
	}}
	svc := &Service{cfg: cfg, feeds: FeedRegistry{Client: srv.Client()}}
	items, err := svc.DiscoverConfiguredItem(context.Background(), "rss-local")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentKey != "g1" {
		t.Fatalf("unexpected discovered items: %+v", items)
	}
}
