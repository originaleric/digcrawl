package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/summary"
	"github.com/originaleric/digcrawl/internal/watchlist"
)

type failedSummary struct{}

func (failedSummary) Summarize(context.Context, summary.Request) (*summary.Result, error) {
	return nil, errors.New("temporary summary failure")
}

func testFailedCollectService(t *testing.T, dbPath string, feedURL string) (*Service, WatchItem, FeedItem) {
	t.Helper()
	st, err := watchlist.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	enabled := true
	cfg := config.Get()
	cfg.Collect.OutputDir = t.TempDir()
	cfg.Collect.PersistMode = "files"
	cfg.Gateway.ArtifactEnabled = boolPtr(false)
	cfg.Watchlist.DryRun = &enabled
	cfg.Watchlist.LoginRecovery.Enabled = &enabled
	collector, err := collect.NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	item := WatchItem{ID: "source", Platform: "feed", Type: "rss", URL: "https://example.com/source", NotifyTo: "c1", Enabled: true, Interval: 6 * time.Hour}
	feed := FeedItem{ContentKey: "exact-1", URL: feedURL, CanonicalURL: feedURL}
	if _, err := st.UpsertDiscovered(context.Background(), item.ID, feed, false); err != nil {
		t.Fatal(err)
	}
	return &Service{cfg: cfg, store: st, collector: collector, feeds: &staticFeedReader{}, summarizer: summary.ExtractiveProvider{}, notifier: &recordingNotifier{}, owner: "test"}, item, feed
}

func testFailedCollectServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "feed-secret" {
			t.Errorf("wrong exact URL: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Recovered Feed</title><item><title>Exact Link</title><link>https://example.com/a</link><description>Recovered content</description></item></channel></rss>`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFailedCollectReplaysExactLinkAndDeletesOnSuccess(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	svc, item, feed := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), server.URL+"/feed.xml?access_token=feed-secret")
	_, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform, Stage: watchlist.RecoveryCollectItem, Feed: feed, NotifyTo: item.NotifyTo, ErrorCode: "LOGIN_REQUIRED"}, 24*time.Hour, true, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.RecordLogin(ctx, item.Platform, time.Now()); err != nil {
		t.Fatal(err)
	}
	ready, err := svc.store.ListReadyFailedCollects(ctx, time.Now())
	if err != nil || len(ready) != 1 {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if err := svc.replayOneLoginRecovery(ctx, ready[0], map[string]WatchItem{item.ID: item}); err != nil {
		t.Fatal(err)
	}
	if svc.notifier.(*recordingNotifier).calls != 1 {
		t.Fatal("exact replay did not notify once")
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("successful replay left failure=%+v err=%v", rows, err)
	}
}

func TestManualFailureAfterExpiryReentersLoginRecovery(t *testing.T) {
	ctx := context.Background()
	svc, item, _ := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), "https://example.com/item")
	stale, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
		TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
		Stage: watchlist.RecoveryDiscoverSource, NotifyTo: item.NotifyTo, ErrorCode: platform.ErrLoginRequired,
	}, 24*time.Hour, true, time.Now().Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc.feeds = &staticFeedReader{err: platform.NewReadError(platform.ErrLoginRequired, "login required", item.Platform, nil)}
	if _, err := svc.RunItem(ctx, item); err == nil {
		t.Fatal("manual source run did not report login failure")
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID == stale.ID || !rows[0].ExpiresAt.After(time.Now()) {
		t.Fatalf("manual failure did not renew its expired cycle: rows=%+v err=%v", rows, err)
	}
	if calls := svc.notifier.(*recordingNotifier).calls; calls != 1 {
		t.Fatalf("new login failure did not notify once: calls=%d", calls)
	}
	if err := svc.store.RecordLogin(ctx, item.Platform, time.Now()); err != nil {
		t.Fatal(err)
	}
	svc.feeds = &staticFeedReader{}
	svc.cfg.Watchlist.Items = []config.WatchlistItem{{ID: item.ID, Platform: item.Platform, Type: item.Type, URL: item.URL, NotifyTo: item.NotifyTo, Interval: "6h", Enabled: boolPtr(true)}}
	if _, err := svc.RunDue(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err = svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("successful replay left failed collect: rows=%+v err=%v", rows, err)
	}
}

func TestFailedCollectRetainsOneRowOnSummaryFailure(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	svc, item, feed := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), server.URL+"/feed.xml?access_token=feed-secret")
	svc.summarizer = failedSummary{}
	first := svc.processPendingFeed(ctx, item, feed, feed.ContentKey, "")
	if len(first.errors) == 0 || first.handoffError != nil {
		t.Fatalf("first=%+v", first)
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 || rows[0].ReadyAt.IsZero() {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	firstID, expiry := rows[0].ID, rows[0].ExpiresAt
	second := svc.processPendingFeed(ctx, item, feed, feed.ContentKey, "")
	if len(second.errors) == 0 {
		t.Fatalf("second=%+v", second)
	}
	rows, err = svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != firstID || !rows[0].ExpiresAt.Equal(expiry) {
		t.Fatalf("duplicate or extended failure=%+v err=%v", rows, err)
	}
}

func TestFailedCollectSurvivesDeliveryPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	path := filepath.Join(t.TempDir(), "watch.db")
	svc, item, feed := testFailedCollectService(t, path, server.URL+"/feed.xml?access_token=feed-secret")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER block_delivery BEFORE INSERT ON dc_watchlist_delivery BEGIN SELECT RAISE(FAIL, 'delivery unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	result := svc.processPendingFeed(ctx, item, feed, feed.ContentKey, "")
	if result.handoffError == nil {
		t.Fatalf("delivery failure was ignored: %+v", result)
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("failed handoff lost recrawl row=%+v err=%v", rows, err)
	}
}

func TestLoginReminderRetriesWhenDeliveryWriteFailed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	svc, item, _ := testFailedCollectService(t, path, "https://example.com/item")
	_, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
		TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
		Stage: watchlist.RecoveryDiscoverSource, NotifyTo: item.NotifyTo, ErrorCode: "LOGIN_REQUIRED",
	}, 24*time.Hour, true, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER block_reminder BEFORE INSERT ON dc_watchlist_delivery BEGIN SELECT RAISE(FAIL, 'delivery unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	current := map[string]WatchItem{item.ID: item}
	if err := svc.prepareLoginRecovery(ctx, current, nil); err == nil {
		t.Fatal("missing delivery write failure")
	}
	if _, err := db.Exec(`DROP TRIGGER block_reminder`); err != nil {
		t.Fatal(err)
	}
	if err := svc.prepareLoginRecovery(ctx, current, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.prepareLoginRecovery(ctx, current, nil); err != nil {
		t.Fatal(err)
	}
	if calls := svc.notifier.(*recordingNotifier).calls; calls != 2 {
		t.Fatalf("reminder sends=%d, want initial failure and one recovery", calls)
	}
}

func TestInvalidTaskConfigurationPreservesFailedCollect(t *testing.T) {
	ctx := context.Background()
	svc, item, _ := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), "https://example.com/item")
	validItem := item
	validItem.ID = "valid-source"
	validItem.URL = "https://example.com/valid-source"
	_, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
		TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
		Stage: watchlist.RecoveryDiscoverSource, NotifyTo: item.NotifyTo, ErrorCode: "LOGIN_REQUIRED",
	}, 24*time.Hour, true, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	validRow, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
		TaskID: validItem.ID, ConfigHash: taskDefinition(validItem).ConfigHash, Platform: validItem.Platform,
		Stage: watchlist.RecoveryDiscoverSource, NotifyTo: validItem.NotifyTo, ErrorCode: "LOGIN_REQUIRED",
	}, 24*time.Hour, true, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc.cfg.Watchlist.Items = []config.WatchlistItem{
		{ID: item.ID, Platform: item.Platform, Type: item.Type, URL: item.URL, NotifyTo: item.NotifyTo, Interval: "invalid", Enabled: boolPtr(true)},
		{ID: validItem.ID, Platform: validItem.Platform, Type: validItem.Type, URL: validItem.URL, NotifyTo: validItem.NotifyTo, Interval: "6h", Enabled: boolPtr(true)},
	}
	if _, err := svc.RunDue(ctx, time.Now()); err == nil {
		t.Fatal("invalid task configuration was not reported")
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("temporary invalid config lost failed collect: rows=%+v err=%v", rows, err)
	}
	delivered, err := svc.store.HasDelivery(ctx, validItem.ID, "__login_required__:"+validRow.ID)
	if err != nil || !delivered {
		t.Fatalf("invalid task blocked valid task reminder: delivered=%v err=%v", delivered, err)
	}
	if err := svc.store.RecordLogin(ctx, item.Platform, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunDue(ctx, time.Now()); err == nil {
		t.Fatal("invalid task configuration was not reported after login")
	}
	rows, err = svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 || rows[0].TaskID != item.ID {
		t.Fatalf("invalid task blocked valid task replay: rows=%+v err=%v", rows, err)
	}
}

func TestUnavailableNotificationChannelPreservesFailedCollect(t *testing.T) {
	ctx := context.Background()
	svc, item, _ := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), "https://example.com/item")
	_, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
		TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
		Stage: watchlist.RecoveryDiscoverSource, NotifyTo: item.NotifyTo, ErrorCode: "LOGIN_REQUIRED",
	}, 24*time.Hour, true, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	svc.cfg.Watchlist.DryRun = boolPtr(false)
	if err := svc.prepareLoginRecovery(ctx, map[string]WatchItem{item.ID: item}, nil); err == nil {
		t.Fatal("unavailable channel was not reported")
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("channel outage lost failed collect: rows=%+v err=%v", rows, err)
	}
}

func TestExistingDeliveryOwnsRetryAfterFailedRowDeletion(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	svc, item, feed := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), server.URL+"/feed.xml?access_token=feed-secret")
	row, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform, Stage: watchlist.RecoveryCollectItem, Feed: feed, NotifyTo: item.NotifyTo}, 24*time.Hour, false, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.UpsertDelivery(ctx, item.ID, feed.ContentKey, item.NotifyTo, StatusNotifyFailed, "temporary send error", true, "title", "text"); err != nil {
		t.Fatal(err)
	}
	if err := svc.replayOneLoginRecovery(ctx, row, map[string]WatchItem{item.ID: item}); err != nil {
		t.Fatal(err)
	}
	if svc.notifier.(*recordingNotifier).calls != 0 {
		t.Fatal("recrawl sent notification despite existing delivery")
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("stale failure row=%+v err=%v", rows, err)
	}
}

func TestDeliveryRetryReportsPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	path := filepath.Join(t.TempDir(), "watch.db")
	svc, item, feed := testFailedCollectService(t, path, server.URL+"/feed.xml?access_token=feed-secret")
	if err := svc.store.UpsertDelivery(ctx, item.ID, feed.ContentKey, item.NotifyTo, StatusNotifyFailed, "temporary", true, "title", "text"); err != nil {
		t.Fatal(err)
	}
	if err := forceDeliveryRetryAt(ctx, path, item.ID, feed.ContentKey, "2000-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER block_delivery_update BEFORE UPDATE ON dc_watchlist_delivery BEGIN SELECT RAISE(FAIL, 'delivery unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = svc.retryPendingDeliveries(ctx, item)
	if err == nil {
		t.Fatal("delivery update failure was ignored")
	}
	if svc.notifier.(*recordingNotifier).calls != 1 {
		t.Fatal("test did not reach delivery retry")
	}
}

func TestFailedRowWriteFailureCanRecoverOnNextSourceScan(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	path := filepath.Join(t.TempDir(), "watch.db")
	svc, item, feed := testFailedCollectService(t, path, server.URL+"/feed.xml?access_token=feed-secret")
	svc.summarizer = failedSummary{}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER block_failure BEFORE INSERT ON dc_watchlist_failed_collects BEGIN SELECT RAISE(FAIL, 'failure table unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	first := svc.processPendingFeed(ctx, item, feed, feed.ContentKey, "")
	if first.handoffError == nil {
		t.Fatalf("failure-row write error ignored: %+v", first)
	}
	if _, err := db.Exec(`DROP TRIGGER block_failure`); err != nil {
		t.Fatal(err)
	}
	svc.feeds = &staticFeedReader{items: []FeedItem{feed}}
	if _, err := svc.runItem(ctx, item, false, ""); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.store.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("normal scan did not restore missing failure row=%+v err=%v", rows, err)
	}
}

func TestAutomaticDiscoveryDoesNotRenewExpiredItemFailures(t *testing.T) {
	for _, entry := range []string{"schedule", "source-replay"} {
		for _, status := range []string{StatusCollectFailed, StatusSummaryFailed, StatusNotifyFailed} {
			t.Run(entry+"/"+status, func(t *testing.T) {
				ctx := context.Background()
				server := testFailedCollectServer(t)
				svc, item, feed := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), server.URL+"/feed.xml?access_token=feed-secret")
				svc.summarizer = failedSummary{}
				svc.feeds = &staticFeedReader{items: []FeedItem{feed}}
				svc.cfg.Watchlist.Items = []config.WatchlistItem{{ID: item.ID, Platform: item.Platform, Type: item.Type, URL: item.URL, NotifyTo: item.NotifyTo, Interval: "6h", Enabled: boolPtr(true)}}
				_, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
					TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
					Stage: watchlist.RecoveryCollectItem, Feed: feed, NotifyTo: item.NotifyTo, ItemStatus: status,
				}, 24*time.Hour, true, time.Now().Add(-25*time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if entry == "source-replay" {
					if _, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
						TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
						Stage: watchlist.RecoveryDiscoverSource, NotifyTo: item.NotifyTo,
					}, 24*time.Hour, false, time.Now().Add(-time.Hour)); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := svc.RunDue(ctx, time.Now()); err != nil {
					t.Fatal(err)
				}
				rows, err := svc.store.ListFailedCollects(ctx)
				if err != nil || len(rows) != 0 {
					t.Fatalf("automatic discovery recreated expired work: rows=%+v err=%v", rows, err)
				}
				got, err := svc.store.ItemStatus(ctx, item.ID, feed.ContentKey)
				if err != nil || got != status {
					t.Fatalf("automatic run changed expired failure status: status=%s err=%v", got, err)
				}
				sum, err := svc.RunItem(ctx, item)
				if err != nil || sum.Collected != 1 {
					t.Fatalf("explicit run did not start a new attempt: sum=%+v err=%v", sum, err)
				}
				rows, err = svc.store.ListFailedCollects(ctx)
				if err != nil || len(rows) != 1 || !rows[0].ExpiresAt.After(time.Now()) {
					t.Fatalf("explicit failure did not start a new window: rows=%+v err=%v", rows, err)
				}
			})
		}
	}
}

func TestRecoveryFailureWritePreservesPreviousItemState(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	path := filepath.Join(t.TempDir(), "watch.db")
	svc, item, feed := testFailedCollectService(t, path, server.URL+"/feed.xml?access_token=feed-secret")
	svc.summarizer = failedSummary{}
	first := svc.processPendingFeed(ctx, item, feed, feed.ContentKey, "")
	if first.handoffError != nil || len(first.errors) == 0 {
		t.Fatalf("first=%+v", first)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER block_failure_update BEFORE UPDATE ON dc_watchlist_failed_collects BEGIN SELECT RAISE(FAIL, 'failure table unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	second := svc.processPendingFeed(ctx, item, feed, feed.ContentKey, "")
	if second.handoffError == nil {
		t.Fatal("missing failure update error")
	}
	status, err := svc.store.ItemStatus(ctx, item.ID, feed.ContentKey)
	if err != nil || status != StatusSummaryFailed {
		t.Fatalf("retry progress revived failed content: status=%s err=%v", status, err)
	}
}

func TestAutomaticDiscoveryRetriesFailedItemsWhenRecoveryDisabled(t *testing.T) {
	ctx := context.Background()
	server := testFailedCollectServer(t)
	svc, item, feed := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), server.URL+"/feed.xml?access_token=feed-secret")
	svc.cfg.Watchlist.LoginRecovery.Enabled = boolPtr(false)
	svc.feeds = &staticFeedReader{items: []FeedItem{feed}}
	if err := svc.store.UpdateItemStatus(ctx, item.ID, feed.ContentKey, StatusCollectFailed, "previous failure", ""); err != nil {
		t.Fatal(err)
	}
	sum, err := svc.runItem(ctx, item, false, "")
	if err != nil || sum.Collected != 1 || sum.Notified != 1 {
		t.Fatalf("legacy automatic retry changed: sum=%+v err=%v", sum, err)
	}
}

type expirySummaryFailure struct{ expires time.Time }

func (s expirySummaryFailure) Summarize(ctx context.Context, req summary.Request) (*summary.Result, error) {
	time.Sleep(time.Until(s.expires.Add(20 * time.Millisecond)))
	return failedSummary{}.Summarize(ctx, req)
}

type expirySourceFailure struct {
	expires time.Time
	err     error
}

func (s expirySourceFailure) ListLatest(context.Context, WatchItem) ([]FeedItem, error) {
	time.Sleep(time.Until(s.expires.Add(20 * time.Millisecond)))
	return nil, s.err
}

func TestAutomaticReplayFinishingAfterExpiryDoesNotRenew(t *testing.T) {
	for _, scenario := range []string{"item-summary", "source-failure", "source-login"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			server := testFailedCollectServer(t)
			svc, item, feed := testFailedCollectService(t, filepath.Join(t.TempDir(), "watch.db"), server.URL+"/feed.xml?access_token=feed-secret")
			stage := watchlist.RecoveryCollectItem
			if scenario != "item-summary" {
				stage = watchlist.RecoveryDiscoverSource
			}
			first, _, err := svc.store.UpsertFailedCollect(ctx, watchlist.FailedCollect{
				TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash, Platform: item.Platform,
				Stage: stage, Feed: feed, NotifyTo: item.NotifyTo,
			}, 24*time.Hour, false, time.Now().Add(-24*time.Hour+time.Second))
			if err != nil {
				t.Fatal(err)
			}
			svc.summarizer = expirySummaryFailure{expires: first.ExpiresAt}
			failure := errors.New("source unavailable")
			if scenario == "source-login" {
				failure = platform.NewReadError(platform.ErrLoginRequired, "login required", item.Platform, nil)
			}
			svc.feeds = expirySourceFailure{expires: first.ExpiresAt, err: failure}
			current := map[string]WatchItem{item.ID: item}
			if err := svc.replayLoginRecovery(ctx, time.Now(), current, nil); err == nil {
				t.Fatal("missing pipeline failure")
			}
			rows, err := svc.store.ListFailedCollects(ctx)
			if err != nil || len(rows) != 1 || rows[0].ID != first.ID || !rows[0].FirstFailedAt.Equal(first.FirstFailedAt) || !rows[0].ExpiresAt.Equal(first.ExpiresAt) || rows[0].Attempts != first.Attempts+1 {
				t.Fatalf("automatic replay changed window: rows=%+v first=%+v err=%v", rows, first, err)
			}
			if calls := svc.notifier.(*recordingNotifier).calls; calls != 0 {
				t.Fatalf("expired replay sent a reminder: calls=%d", calls)
			}
			if err := svc.prepareLoginRecovery(ctx, current, nil); err != nil {
				t.Fatal(err)
			}
			rows, err = svc.store.ListFailedCollects(ctx)
			if err != nil || len(rows) != 0 {
				t.Fatalf("expired record did not settle: rows=%+v err=%v", rows, err)
			}
		})
	}
}
