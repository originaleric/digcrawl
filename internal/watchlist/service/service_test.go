package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/notify"
	"github.com/originaleric/digcrawl/internal/summary"
	"github.com/originaleric/digcrawl/internal/watchlist"
)

func boolPtr(v bool) *bool {
	return &v
}

type recordingNotifier struct {
	calls    int
	messages []notify.Message
}

func (n *recordingNotifier) Send(ctx context.Context, channelID string, msg notify.Message) notify.Result {
	n.calls++
	n.messages = append(n.messages, msg)
	return notify.Result{ChannelID: channelID, Status: StatusNotified}
}

type staticFeedReader struct {
	items []FeedItem
	err   error
}

func (r staticFeedReader) ListLatest(ctx context.Context, item WatchItem) ([]FeedItem, error) {
	return r.items, r.err
}

type fixedResultNotifier struct {
	calls  int
	result notify.Result
}

func (n *fixedResultNotifier) Send(ctx context.Context, channelID string, msg notify.Message) notify.Result {
	n.calls++
	res := n.result
	if res.ChannelID == "" {
		res.ChannelID = channelID
	}
	return res
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

func TestValidateNotificationConfigDatabaseModeIgnoresYAMLItems(t *testing.T) {
	cfg := baseNotificationConfig()
	cfg.Watchlist.Source.Mode = "database"
	cfg.Watchlist.Items[0].NotifyTo = "missing-channel"
	if err := validateNotificationConfig(cfg); err != nil {
		t.Fatalf("database source mode should not validate YAML watchlist items: %v", err)
	}
}

func TestValidateDatabaseNotificationConfigValidatesSourceNotifyTo(t *testing.T) {
	ctx := context.Background()
	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSource(ctx, watchlist.WatchSource{
		ID:       "db-source",
		Platform: "rss",
		Type:     "rss",
		URL:      "https://example.com/feed.xml",
		Enabled:  true,
		NotifyTo: "missing-channel",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := baseNotificationConfig()
	cfg.Watchlist.Source.Mode = "database"
	err = validateDatabaseNotificationConfig(ctx, cfg, st)
	if err == nil || !strings.Contains(err.Error(), "missing-channel") {
		t.Fatalf("expected database source NotifyTo validation error, got %v", err)
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

func TestDatabaseWatchlistItemsPreservesSourceSchedule(t *testing.T) {
	st, err := watchlist.OpenStore(t.TempDir() + "/watchlist.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertSource(ctx, watchlist.WatchSource{
		ID:           "daily-db-source",
		Platform:     "toutiao",
		Type:         "toutiao_author",
		URL:          "https://www.toutiao.com/c/user/token/example/?tab=wtt",
		Enabled:      true,
		ScheduleMode: "daily",
		Interval:     24 * time.Hour,
		ScheduleAt:   "09:00",
		NotifyTo:     "feishu-default",
		Options:      map[string]any{"limit": 2},
	}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{cfg: &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}}, store: st}
	items, err := svc.databaseWatchlistItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items len=%d", len(items))
	}
	item, err := svc.normalizeItem(items[0])
	if err != nil {
		t.Fatal(err)
	}
	if item.ScheduleMode != "daily" || item.ScheduleAt != "09:00" || item.Interval != 24*time.Hour {
		t.Fatalf("item schedule mismatch: %+v", item)
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

func TestSummaryTextForVideoKeepsASRSeparateForAgent(t *testing.T) {
	feed := FeedItem{ContentType: "video"}
	out := map[string]any{
		"content_type": "video",
		"text":         "看更多视频，上今日头条。",
		"markdown":     "页面 Markdown",
	}
	got, err := summaryTextForCollectedItem(feed, out, "视频口播文案", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected video page text to stay empty so ASR is passed separately to agent, got %q", got)
	}
}

func TestSummaryTextForVideoRequiresASR(t *testing.T) {
	feed := FeedItem{ContentType: "video"}
	out := map[string]any{
		"content_type": "video",
		"text":         "看更多视频，上今日头条。",
		"markdown":     "页面 Markdown",
		"platform_metadata": map[string]any{
			"asr_error": "local ASR produced no JSON transcript",
		},
	}
	_, err := summaryTextForCollectedItem(feed, out, "", nil)
	if err == nil || !strings.Contains(err.Error(), "local ASR produced no JSON transcript") {
		t.Fatalf("expected video without ASR to fail, got %v", err)
	}
}

func TestSummaryTextForVideoReportsDownloadFailure(t *testing.T) {
	feed := FeedItem{ContentType: "video"}
	out := map[string]any{"content_type": "video"}
	downloaded := []map[string]any{{
		"type": "video", "url": "https://example.com/a.mp4", "error": "response too large",
	}}
	_, err := summaryTextForCollectedItem(feed, out, "", downloaded)
	if err == nil || !strings.Contains(err.Error(), "video download failed: response too large") {
		t.Fatalf("expected video download failure reason, got %v", err)
	}
}

func TestSummaryTextForArticlePrefersPageText(t *testing.T) {
	feed := FeedItem{ContentType: "article"}
	out := map[string]any{
		"content_type": "article",
		"text":         "文章正文",
	}
	got, err := summaryTextForCollectedItem(feed, out, "误传 ASR", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "文章正文" {
		t.Fatalf("expected article summary text to prefer page text, got %q", got)
	}
}

func TestValidateVideoSummaryRejectsExtractiveFallback(t *testing.T) {
	collected := CollectedItem{
		Feed:    FeedItem{ContentType: "video"},
		ASRText: "这是一段视频口播转写。",
	}
	err := validateSummaryResultForCollectedItem(collected, &summary.Result{
		Provider: "extractive",
		Items: []summary.ResultItem{{
			Summary: "这是一段视频口播转写。",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "requires agent provider") {
		t.Fatalf("expected extractive fallback rejection, got %v", err)
	}
}

func TestValidateVideoSummaryRejectsCopiedTranscript(t *testing.T) {
	collected := CollectedItem{
		Feed:    FeedItem{ContentType: "video"},
		ASRText: "这是一段视频口播转写。",
	}
	err := validateSummaryResultForCollectedItem(collected, &summary.Result{
		Provider: "digflow_runtime",
		Items: []summary.ResultItem{{
			Summary: "这是一段 视频口播 转写。",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "copied ASR transcript") {
		t.Fatalf("expected copied transcript rejection, got %v", err)
	}
}

func TestValidateVideoSummaryAcceptsAgentSummary(t *testing.T) {
	collected := CollectedItem{
		Feed:    FeedItem{ContentType: "video"},
		ASRText: "这是一段视频口播转写，包含较多原始表达。",
	}
	err := validateSummaryResultForCollectedItem(collected, &summary.Result{
		Provider: "digflow_runtime",
		Items: []summary.ResultItem{{
			Summary: "作者回应观众关心，解释近期未继续使用某个表达，是因为表达需要符合当下语境。",
		}},
	})
	if err != nil {
		t.Fatalf("expected agent summary to pass, got %v", err)
	}
}

func TestRetryPendingDeliveriesRejectsStaleVideoMessage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	st, err := watchlist.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.UpsertDiscovered(ctx, "video-watch", FeedItem{
		ContentKey:  "video-1",
		URL:         "https://www.toutiao.com/video/1/",
		Title:       "Video",
		ContentType: "video",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDeliveryWithProvider(ctx, "video-watch", "video-1", "feishu-default", StatusNotifyFailed, "http 500", true, "Video", "raw transcript", ""); err != nil {
		t.Fatal(err)
	}
	if err := forceDeliveryRetryAt(ctx, path, "video-watch", "video-1", "2000-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{MaxItemsPerRun: 10}},
		store:    st,
		notifier: notifier,
	}
	notified, err := svc.retryPendingDeliveries(ctx, WatchItem{ID: "video-watch"})
	if err != nil {
		t.Fatal(err)
	}
	if notified != 0 || notifier.calls != 0 {
		t.Fatalf("stale video pending delivery should not be sent, notified=%d calls=%d", notified, notifier.calls)
	}
	status, err := st.ItemStatus(ctx, "video-watch", "video-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusSummaryFailed {
		t.Fatalf("stale video pending delivery should make the item retryable for regenerated summary, got status %q", status)
	}
}

func TestRetryPendingDeliveriesAllowsAgentVideoMessage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	st, err := watchlist.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.UpsertDiscovered(ctx, "video-watch", FeedItem{
		ContentKey:  "video-1",
		URL:         "https://www.toutiao.com/video/1/",
		Title:       "Video",
		ContentType: "video",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDeliveryWithProvider(ctx, "video-watch", "video-1", "feishu-default", StatusNotifyFailed, "http 500", true, "Video", "agent summary", "digflow_runtime"); err != nil {
		t.Fatal(err)
	}
	if err := forceDeliveryRetryAt(ctx, path, "video-watch", "video-1", "2000-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{MaxItemsPerRun: 10}},
		store:    st,
		notifier: notifier,
	}
	notified, err := svc.retryPendingDeliveries(ctx, WatchItem{ID: "video-watch"})
	if err != nil {
		t.Fatal(err)
	}
	if notified != 1 || notifier.calls != 1 {
		t.Fatalf("agent video pending delivery should be retried, notified=%d calls=%d", notified, notifier.calls)
	}
	status, err := st.ItemStatus(ctx, "video-watch", "video-1")
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusNotified {
		t.Fatalf("agent video pending delivery should become notified, got status %q", status)
	}
}

func forceDeliveryRetryAt(ctx context.Context, path, watchID, contentKey, retryAt string) error {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `UPDATE dc_watchlist_delivery SET next_retry_at = ? WHERE watch_id = ? AND content_key = ?`, retryAt, watchID, contentKey)
	return err
}

func TestShouldNotifyNoUpdateRequiresConfiguredLimitCompleteness(t *testing.T) {
	opts := map[string]any{"notify_no_update": true, "limit": 2}
	if shouldNotifyNoUpdate(opts, 1) {
		t.Fatal("no-update notification should be suppressed when discovery is below configured limit")
	}
	if reason := noUpdateSuppressedReason(opts, 1); !strings.Contains(reason, "discovered 1 below no-update threshold 2") {
		t.Fatalf("expected suppression reason, got %q", reason)
	}
	if !shouldNotifyNoUpdate(opts, 2) {
		t.Fatal("no-update notification should be allowed when discovery reaches configured limit")
	}
	if reason := noUpdateSuppressedReason(opts, 2); reason != "" {
		t.Fatalf("complete discovery should not report suppression, got %q", reason)
	}
}

func TestShouldNotifyNoUpdateUsesExplicitMinimum(t *testing.T) {
	opts := map[string]any{"notify_no_update": true, "limit": 5, "min_discovered_for_no_update": 2}
	if shouldNotifyNoUpdate(opts, 1) {
		t.Fatal("no-update notification should respect explicit minimum")
	}
	if !shouldNotifyNoUpdate(opts, 2) {
		t.Fatal("no-update notification should allow explicit minimum below source limit")
	}
}

func TestShouldNotifyNoUpdateWithoutLimitKeepsCompatibility(t *testing.T) {
	if !shouldNotifyNoUpdate(map[string]any{"notify_no_update": true}, 0) {
		t.Fatal("no-update notification without a discovery threshold should keep existing behavior")
	}
}

func TestShouldNotifyNoUpdateForRunSuppressesWhenRunHasErrors(t *testing.T) {
	opts := map[string]any{"notify_no_update": true, "limit": 2}
	errors := []string{"video transcript unavailable: ASR/subtitles are required for video summary"}
	if shouldNotifyNoUpdateForRun(opts, 2, errors, 0) {
		t.Fatal("no-update notification should be suppressed when the run has item processing errors")
	}
	reason := noUpdateSuppressedReasonForRun(opts, 2, errors, 0)
	if !strings.Contains(reason, "run has 1 error") {
		t.Fatalf("expected error suppression reason, got %q", reason)
	}
	if !shouldNotifyNoUpdateForRun(opts, 2, nil, 0) {
		t.Fatal("no-update notification should still be allowed for complete, error-free discovery")
	}
}

func TestShouldNotifyNoUpdateForRunSuppressesWhenContentWasProcessed(t *testing.T) {
	opts := map[string]any{"notify_no_update": true, "limit": 2}
	if shouldNotifyNoUpdateForRun(opts, 2, nil, 2) {
		t.Fatal("no-update notification should be suppressed when retryable content was processed")
	}
	reason := noUpdateSuppressedReasonForRun(opts, 2, nil, 2)
	if !strings.Contains(reason, "content activity") {
		t.Fatalf("expected content activity suppression reason, got %q", reason)
	}
}

func TestRunItemReportsSuppressedNoUpdateWhenDiscoveryIncomplete(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a</link><guid>g1</guid></item></channel></rss>`))
	}))
	defer srv.Close()

	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "rss-local", Platform: "rss", Type: "rss", URL: srv.URL,
		Options: map[string]any{"notify_no_update": true, "limit": 2},
	}
	if _, err := st.UpsertDiscovered(ctx, item.ID, FeedItem{
		ID: "g1", ContentKey: "g1", URL: "https://example.com/a", Title: "A",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateItemStatus(ctx, item.ID, "g1", StatusNotified, "", ""); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		cfg:   &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store: st,
		feeds: FeedRegistry{Client: srv.Client()},
	}
	sum, err := svc.RunItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.NoUpdateSuppressed {
		t.Fatalf("expected suppressed no-update state, got %+v", sum)
	}
	if !strings.Contains(sum.NoUpdateSuppressedReason, "discovered 1 below no-update threshold 2") {
		t.Fatalf("unexpected suppression reason: %+v", sum)
	}
	if sum.Notified != 0 || len(sum.Errors) != 0 {
		t.Fatalf("suppressed no-update should be observable but not a notify/error result: %+v", sum)
	}
}

func TestRunItemSuppressesNoUpdateAfterSameDayContentNotification(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>https://example.com/a</link><guid>g1</guid></item></channel></rss>`))
	}))
	defer srv.Close()

	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "rss-local", Platform: "rss", Type: "rss", URL: srv.URL, NotifyTo: "feishu-default",
		Options: map[string]any{"notify_no_update": true, "limit": 1},
	}
	if _, err := st.UpsertDiscovered(ctx, item.ID, FeedItem{
		ID: "g1", ContentKey: "g1", URL: "https://example.com/a", Title: "A",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateItemStatus(ctx, item.ID, "g1", StatusNotified, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertDelivery(ctx, item.ID, "g1", "feishu-default", StatusNotified, "", false, "A", "summary"); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store:    st,
		feeds:    FeedRegistry{Client: srv.Client()},
		notifier: notifier,
	}
	sum, err := svc.RunItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.NoUpdateSuppressed || !strings.Contains(sum.NoUpdateSuppressedReason, "already notified real content today") {
		t.Fatalf("expected same-day content suppression, got %+v", sum)
	}
	if sum.Notified != 0 || notifier.calls != 0 {
		t.Fatalf("same-day content notification should suppress no-update send, summary=%+v calls=%d", sum, notifier.calls)
	}
}

func TestRunItemSendsNoUpdateOnlyOncePerDay(t *testing.T) {
	ctx := context.Background()
	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "rss-local", Name: "RSS 列表", Platform: "rss", Type: "rss", URL: "https://example.com/feed", NotifyTo: "feishu-default",
		Options: map[string]any{"notify_no_update": true, "limit": 1},
	}
	feed := FeedItem{ID: "g1", ContentKey: "g1", URL: "https://example.com/a", Title: "A"}
	if _, err := st.UpsertDiscovered(ctx, item.ID, feed, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateItemStatus(ctx, item.ID, feed.ContentKey, StatusNotified, "", ""); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store:    st,
		feeds:    staticFeedReader{items: []FeedItem{feed}},
		notifier: notifier,
	}
	first, err := svc.RunItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if first.Notified != 1 || notifier.calls != 1 {
		t.Fatalf("first complete no-update run should notify once, summary=%+v calls=%d", first, notifier.calls)
	}
	second, err := svc.RunItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if !second.NoUpdateSuppressed || !strings.Contains(second.NoUpdateSuppressedReason, "already sent a no-update") {
		t.Fatalf("expected duplicate no-update suppression, got %+v", second)
	}
	if second.Notified != 0 || notifier.calls != 1 {
		t.Fatalf("duplicate no-update run should not notify, summary=%+v calls=%d", second, notifier.calls)
	}
}

func TestRunItemNotifiesLoginRequiredOnlyOncePerDay(t *testing.T) {
	ctx := context.Background()
	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "toutiao-video", Name: "头条作手阿飞视频列表", Platform: "toutiao", Type: "toutiao_author", URL: "https://www.toutiao.com/c/user/token/",
		NotifyTo: "feishu-default",
	}
	notifier := &recordingNotifier{}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store:    st,
		feeds:    staticFeedReader{err: errors.New("LOGIN_REQUIRED: toutiao author page requires login")},
		notifier: notifier,
	}
	first, err := svc.RunItem(ctx, item)
	if err == nil {
		t.Fatal("expected login-required discovery error")
	}
	if first.Notified != 1 || notifier.calls != 1 {
		t.Fatalf("first login-required run should notify once, summary=%+v calls=%d", first, notifier.calls)
	}
	if len(notifier.messages) != 1 || !strings.Contains(notifier.messages[0].Title, "需要重新登录") || !strings.Contains(notifier.messages[0].Text, item.Name) || !strings.Contains(notifier.messages[0].Text, "digcrawl browser login --platform toutiao") {
		t.Fatalf("unexpected login-required notification: %+v", notifier.messages)
	}
	second, err := svc.RunItem(ctx, item)
	if err == nil {
		t.Fatal("expected repeated login-required discovery error")
	}
	if second.Notified != 0 || notifier.calls != 1 {
		t.Fatalf("repeated login-required run should not notify again, summary=%+v calls=%d", second, notifier.calls)
	}
}

func TestRunItemDoesNotDoubleSendLoginRequiredAfterRetryFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	st, err := watchlist.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "toutiao-video", Name: "头条作手阿飞视频列表", Platform: "toutiao", Type: "toutiao_author", URL: "https://www.toutiao.com/c/user/token/",
		NotifyTo: "feishu-default",
	}
	notifier := &fixedResultNotifier{result: notify.Result{Status: StatusNotifyFailed, Error: "temporary failure", Retryable: true}}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store:    st,
		feeds:    staticFeedReader{err: errors.New("LOGIN_REQUIRED: toutiao author page requires login")},
		notifier: notifier,
	}
	if _, err := svc.RunItem(ctx, item); err == nil {
		t.Fatal("expected login-required discovery error")
	}
	if notifier.calls != 1 {
		t.Fatalf("expected the initial login-required notification attempt, got %d", notifier.calls)
	}
	if err := forceDeliveryRetryAt(ctx, path, item.ID, loginRequiredDeliveryKey(item.Platform, time.Now()), "2000-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunItem(ctx, item); err == nil {
		t.Fatal("expected repeated login-required discovery error")
	}
	if notifier.calls != 2 {
		t.Fatalf("a due retry must be the only notification attempt in its run, got %d", notifier.calls)
	}
}

func TestRunItemDoesNotDoubleSendNoUpdateAfterRetryFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	st, err := watchlist.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "rss-local", Name: "RSS 列表", Platform: "rss", Type: "rss", URL: "https://example.com/feed", NotifyTo: "feishu-default",
		Options: map[string]any{"notify_no_update": true, "limit": 1},
	}
	feed := FeedItem{ID: "g1", ContentKey: "g1", URL: "https://example.com/a", Title: "A"}
	if _, err := st.UpsertDiscovered(ctx, item.ID, feed, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateItemStatus(ctx, item.ID, feed.ContentKey, StatusNotified, "", ""); err != nil {
		t.Fatal(err)
	}
	notifier := &fixedResultNotifier{result: notify.Result{Status: StatusNotifyFailed, Error: "temporary failure", Retryable: true}}
	svc := &Service{
		cfg:      &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store:    st,
		feeds:    staticFeedReader{items: []FeedItem{feed}},
		notifier: notifier,
	}
	if _, err := svc.RunItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 1 {
		t.Fatalf("expected the initial no-update notification attempt, got %d", notifier.calls)
	}
	if err := forceDeliveryRetryAt(ctx, path, item.ID, noUpdateDeliveryKey(time.Now()), "2000-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if notifier.calls != 2 {
		t.Fatalf("a due retry must be the only no-update attempt in its run, got %d", notifier.calls)
	}
}

func TestRunItemSuppressesNoUpdateWhenRetryableItemProcessingFails(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>A</title><link>unsupported://example.com/a</link><guid>g1</guid></item></channel></rss>`))
	}))
	defer srv.Close()

	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	item := WatchItem{
		ID: "rss-local", Platform: "rss", Type: "rss", URL: srv.URL,
		Options: map[string]any{"notify_no_update": true, "limit": 1},
	}
	if _, err := st.UpsertDiscovered(ctx, item.ID, FeedItem{
		ID: "g1", ContentKey: "g1", URL: "unsupported://example.com/a", Title: "A",
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateItemStatus(ctx, item.ID, "g1", StatusCollectFailed, "previous collect failed", ""); err != nil {
		t.Fatal(err)
	}
	collector, err := collect.NewService(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	svc := &Service{
		cfg:       &config.Config{Watchlist: config.WatchlistConfig{DefaultInterval: "6h"}},
		store:     st,
		collector: collector,
		feeds:     FeedRegistry{Client: srv.Client()},
		notifier:  notifier,
	}
	sum, err := svc.RunItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Errors) == 0 {
		t.Fatalf("expected processing error, got %+v", sum)
	}
	if sum.Notified != 0 || notifier.calls != 0 {
		t.Fatalf("no-update must not notify when item processing fails, summary=%+v calls=%d", sum, notifier.calls)
	}
	if !sum.NoUpdateSuppressed || !strings.Contains(sum.NoUpdateSuppressedReason, "run has 1 error") {
		t.Fatalf("expected no-update suppression reason for run errors, got %+v", sum)
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

func TestRunBoundedHonorsLimitAndInputOrder(t *testing.T) {
	items := []string{"first", "second", "third"}
	started := make(chan struct{}, len(items))
	release := make(chan struct{})
	type output struct {
		summaries []RunSummary
		err       error
	}
	done := make(chan output, 1)
	go func() {
		summaries, err := runBounded(2, items, func(item string) (RunSummary, error) {
			started <- struct{}{}
			<-release
			return RunSummary{WatchID: item}, nil
		})
		done <- output{summaries: summaries, err: err}
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("expected two source runs to start concurrently")
		}
	}
	close(release)
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	for index, item := range items {
		if result.summaries[index].WatchID != item {
			t.Fatalf("summary %d WatchID=%q want %q", index, result.summaries[index].WatchID, item)
		}
	}
}

func TestRunPendingFeedsHonorsLimitAndInputOrder(t *testing.T) {
	feeds := []string{"first", "second", "third"}
	started := make(chan struct{}, len(feeds))
	release := make(chan struct{})
	done := make(chan []itemResult, 1)
	go func() {
		done <- runPendingFeeds(2, feeds, func(feed string) itemResult {
			started <- struct{}{}
			<-release
			return itemResult{errors: []string{feed}}
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("expected two detail jobs to start concurrently")
		}
	}
	close(release)
	results := <-done
	for index, feed := range feeds {
		if got := results[index].errors; len(got) != 1 || got[0] != feed {
			t.Fatalf("result %d errors=%v want [%s]", index, got, feed)
		}
	}
}

func TestRunItemDeduplicatesRepeatedContentKeysBeforeConcurrentProcessing(t *testing.T) {
	ctx := context.Background()
	st, err := watchlist.OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	collector, err := collect.NewService(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	item := WatchItem{ID: "duplicate-feed", Platform: "rss", Type: "rss", URL: "https://example.com/feed"}
	feed := FeedItem{ID: "entry-1", ContentKey: "entry-1", URL: "unsupported://example.com/article", Title: "same entry"}
	svc := &Service{
		cfg: &config.Config{Watchlist: config.WatchlistConfig{
			NotifyOnFirstRun: boolPtr(true),
			Collect:          config.WatchlistCollectConfig{MaxConcurrentItems: 2},
		}},
		store:     st,
		collector: collector,
		feeds:     staticFeedReader{items: []FeedItem{feed, feed}},
	}
	sum, err := svc.RunItem(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if sum.NewItems != 1 || len(sum.Errors) != 1 {
		t.Fatalf("duplicate content should be processed once, got %+v", sum)
	}
}
