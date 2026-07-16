package watchlist

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/notify"
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

func TestIsDueUsesPersistedRunHistory(t *testing.T) {
	st, err := OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := &Service{
		cfg:   &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store: st,
	}
	item := WatchItem{ID: "source-1", Interval: 6 * time.Hour, Enabled: true}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()
	due, err := svc.isDue(ctx, item, now)
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("item with no run history should be due")
	}
	runID, err := st.StartRun(ctx, item.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(ctx, runID, RunSummary{WatchID: item.ID}, "success", ""); err != nil {
		t.Fatal(err)
	}
	due, err = svc.isDue(ctx, item, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("item should not be due before its interval has elapsed")
	}
}
