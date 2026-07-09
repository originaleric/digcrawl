package watchlist

import (
	"strings"
	"testing"

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
