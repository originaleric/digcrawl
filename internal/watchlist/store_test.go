package watchlist

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreLockAndBaseline(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var foreignKeys int
	if err := st.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d", foreignKeys)
	}
	ok, err := st.AcquireLock(ctx, "w1", "owner1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first lock ok=%v err=%v", ok, err)
	}
	ok, err = st.AcquireLock(ctx, "w1", "owner2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second owner acquired active lock")
	}
	item := FeedItem{ID: "i1", ContentKey: "k1", URL: "https://example.com/a", Title: "A"}
	isNew, err := st.UpsertDiscovered(ctx, "w1", item, true)
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Fatal("baseline item should not be new")
	}
	status, err := st.ItemStatus(ctx, "w1", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusSkippedBaseline {
		t.Fatalf("status=%s", status)
	}
}

func TestStoreDeliveryRetryState(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDelivery(ctx, "w1", "k1", "c1", StatusNotifyFailed, "http 500", true, "title", "body"); err != nil {
		t.Fatal(err)
	}
	pending, err := st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("next_retry_at should delay pending delivery, got %+v", pending)
	}
	if err := st.UpsertDelivery(ctx, "w1", "k2", "c1", StatusNotifyFailed, "http 500", false, "title2", "body2"); err != nil {
		t.Fatal(err)
	}
	pending, err = st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("non-retryable failure should not be pending: %+v", pending)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE dc_watchlist_delivery SET next_retry_at = ? WHERE watch_id = ? AND content_key = ?`, "2000-01-01T00:00:00Z", "w1", "k1"); err != nil {
		t.Fatal(err)
	}
	pending, err = st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Text != "body" {
		t.Fatalf("expected due retryable delivery: %+v", pending)
	}
	if err := st.UpsertDelivery(ctx, "w1", "k1", "c1", StatusNotified, "", false, "title", "body"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreMigratesLegacyTables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE watchlist_items (
  watch_id TEXT NOT NULL,
  content_key TEXT NOT NULL,
  platform_item_id TEXT,
  url TEXT NOT NULL,
  canonical_url TEXT,
  title TEXT,
  published_at TEXT,
  content_type TEXT,
  content_hash TEXT,
  summary_hash TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  baseline_only INTEGER DEFAULT 0,
  status TEXT NOT NULL,
  collect_error TEXT,
  summary_error TEXT,
  PRIMARY KEY (watch_id, content_key)
);
CREATE TABLE watchlist_delivery (
  watch_id TEXT NOT NULL,
  content_key TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  status TEXT NOT NULL,
  notified_at TEXT,
  retry_count INTEGER DEFAULT 0,
  next_retry_at TEXT,
  notify_error TEXT,
  PRIMARY KEY (watch_id, content_key, channel_id)
);
INSERT INTO watchlist_items (
  watch_id, content_key, platform_item_id, url, canonical_url, title, published_at, content_type,
  content_hash, summary_hash, first_seen_at, last_seen_at, baseline_only, status, collect_error, summary_error
) VALUES ('w1', 'k1', 'p1', 'https://example.com/a', NULL, 'Legacy', NULL, 'article', NULL, NULL, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 0, 'notify_failed', NULL, NULL);
INSERT INTO watchlist_delivery (watch_id, content_key, channel_id, status, notified_at, retry_count, next_retry_at, notify_error)
VALUES ('w1', 'k1', 'c1', 'notify_failed', NULL, 0, '2000-01-01T00:00:00Z', 'http 500');
`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}

	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hasAny, err := st.HasAnyItem(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if !hasAny {
		t.Fatal("legacy item not migrated")
	}
	pending, err := st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ContentKey != "k1" {
		t.Fatalf("legacy delivery not migrated: %+v", pending)
	}
}

func TestStoreSourceCRUD(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertSource(ctx, WatchSource{
		ID: "reddit-ai", Name: "Reddit AI", Platform: "Reddit", Type: "Reddit_Subreddit",
		URL: "https://www.reddit.com/r/artificial/new/", Enabled: true, Interval: 30 * time.Minute,
		NotifyTo: "feishu-default", Options: map[string]any{"sort": "new", "limit": float64(25)},
	}); err != nil {
		t.Fatal(err)
	}
	sources, err := st.ListSources(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources len=%d", len(sources))
	}
	got := sources[0]
	if got.ID != "reddit-ai" || got.Platform != "reddit" || got.Type != "reddit_subreddit" || got.Interval != 30*time.Minute || got.NotifyTo != "feishu-default" {
		t.Fatalf("source mismatch: %+v", got)
	}
	if got.Options["sort"] != "new" {
		t.Fatalf("options mismatch: %+v", got.Options)
	}
	got, err = st.GetSource(ctx, "reddit-ai")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Reddit AI" {
		t.Fatalf("get source mismatch: %+v", got)
	}
	if err := st.SetSourceEnabled(ctx, "reddit-ai", false); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSourceEnabled(ctx, "reddit-ai", false); err != nil {
		t.Fatalf("setting the same enabled state should be idempotent: %v", err)
	}
	enabled, err := st.ListSources(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled) != 0 {
		t.Fatalf("disabled source returned in enabled list: %+v", enabled)
	}
	if err := st.DeleteSource(ctx, "reddit-ai"); err != nil {
		t.Fatal(err)
	}
	all, err := st.ListSources(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("source not deleted: %+v", all)
	}
}

func TestMySQLMigrationKeepsWatchIDLengthAlignedWithSources(t *testing.T) {
	sql := strings.Join(mysqlMigrationStatements(), "\n")
	if strings.Contains(sql, "watch_id VARCHAR(128)") {
		t.Fatal("mysql watch_id columns must stay aligned with source_id VARCHAR(191)")
	}
	for _, want := range []string{
		"source_id VARCHAR(191)",
		"watch_id VARCHAR(191)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("mysql migration missing %q", want)
		}
	}
}
