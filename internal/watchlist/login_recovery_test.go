package watchlist

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFailedCollectLoginRestartAndOneDayExpiry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	feed := FeedItem{ContentKey: "post-1", URL: "https://example.com/?token=secret"}
	if _, err := st.UpsertDiscovered(ctx, "task", feed, false); err != nil {
		t.Fatal(err)
	}
	failure := FailedCollect{TaskID: "task", ConfigHash: "v1", Platform: "toutiao", Stage: RecoveryCollectItem, Feed: feed, ErrorCode: "LOGIN_REQUIRED"}
	first, created, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, true, start)
	if err != nil || !created {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	again, created, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, true, start.Add(time.Hour))
	if err != nil || created || again.ID != first.ID || !again.ExpiresAt.Equal(start.Add(24*time.Hour)) {
		t.Fatalf("again=%+v created=%v err=%v", again, created, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.RecordLogin(ctx, "toutiao", start.Add(23*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ready, err := st.ListReadyFailedCollects(ctx, start.Add(23*time.Hour))
	if err != nil || len(ready) != 1 || ready[0].ID != first.ID {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if ready[0].Feed.URL != "" || strings.Contains(ready[0].Diagnostic, "secret") {
		t.Fatalf("failure row persisted URL: %+v", ready[0])
	}
	exact, found, err := st.FailedCollectFeed(ctx, "task", ready[0].ContentKeyHash)
	if err != nil || !found || exact.URL != feed.URL {
		t.Fatalf("exact=%+v found=%v err=%v", exact, found, err)
	}
	if _, _, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, true, start.Add(23*time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	ready, err = st.ListReadyFailedCollects(ctx, start.Add(23*time.Hour+2*time.Minute))
	if err != nil || len(ready) != 0 {
		t.Fatalf("login failure retried without another login: %+v err=%v", ready, err)
	}
	if err := st.RecordLogin(ctx, "toutiao", start.Add(23*time.Hour+3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ready, err = st.ListReadyFailedCollects(ctx, start.Add(24*time.Hour))
	if err != nil || len(ready) != 0 {
		t.Fatalf("expired failure ready=%+v err=%v", ready, err)
	}
}

func TestFailedCollectAndItemStatusCommitTogether(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	feed := FeedItem{ContentKey: "item", URL: "https://example.com/item"}
	if _, err := st.UpsertDiscovered(ctx, "source", feed, false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER block_failed_status BEFORE UPDATE ON dc_watchlist_items WHEN NEW.status = 'summary_failed' BEGIN SELECT RAISE(FAIL, 'status unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	failure := FailedCollect{TaskID: "source", ConfigHash: "v1", Platform: "feed", Stage: RecoveryCollectItem, Feed: feed, ItemStatus: StatusSummaryFailed}
	if _, _, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, false, time.Now()); err == nil {
		t.Fatal("failed status write was not reported")
	}
	rows, err := st.ListFailedCollects(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("failed transaction left a recovery row: rows=%+v err=%v", rows, err)
	}
	status, err := st.ItemStatus(ctx, "source", feed.ContentKey)
	if err != nil || status != StatusDiscovered {
		t.Fatalf("failed transaction changed unfinished item: status=%s err=%v", status, err)
	}
	if _, err := st.db.Exec(`DROP TRIGGER block_failed_status`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err = st.ListFailedCollects(ctx)
	status, statusErr := st.ItemStatus(ctx, "source", feed.ContentKey)
	if err != nil || statusErr != nil || len(rows) != 1 || status != StatusSummaryFailed {
		t.Fatalf("failure did not commit together: rows=%+v status=%s err=%v statusErr=%v", rows, status, err, statusErr)
	}
}

func TestNewFailureAfterExpiryStartsNewWindow(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	failure := FailedCollect{TaskID: "source", ConfigHash: "v1", Platform: "toutiao", Stage: RecoveryDiscoverSource, ErrorCode: "LOGIN_REQUIRED"}
	first, _, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, true, start)
	if err != nil {
		t.Fatal(err)
	}
	same, created, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, true, start.Add(23*time.Hour))
	if err != nil || created || same.ID != first.ID || !same.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("failure inside window changed cycle: row=%+v created=%v err=%v", same, created, err)
	}
	nextAt := start.Add(24 * time.Hour)
	next, created, err := st.UpsertFailedCollect(ctx, failure, 24*time.Hour, true, nextAt)
	if err != nil || !created || next.ID == first.ID || !next.FirstFailedAt.Equal(nextAt) || !next.ExpiresAt.Equal(nextAt.Add(24*time.Hour)) || next.Attempts != 0 {
		t.Fatalf("expired failure did not start new cycle: row=%+v created=%v err=%v", next, created, err)
	}
	rows, err := st.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 || rows[0].ID != next.ID {
		t.Fatalf("failure table has stale or duplicate rows: rows=%+v err=%v", rows, err)
	}
	if err := st.RecordLogin(ctx, "toutiao", nextAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ready, err := st.ListReadyFailedCollects(ctx, nextAt.Add(time.Minute))
	if err != nil || len(ready) != 1 || ready[0].ID != next.ID {
		t.Fatalf("new cycle was not unlocked by login: rows=%+v err=%v", ready, err)
	}
}

func TestFailedCollectFairnessAndConfigReset(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		_, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: id, ConfigHash: "v1", Platform: "feed", Stage: RecoveryDiscoverSource}, 24*time.Hour, false, start)
		if err != nil {
			t.Fatal(err)
		}
	}
	ready, err := st.ListReadyFailedCollects(ctx, start.Add(time.Minute))
	if err != nil || len(ready) != 2 {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	first := ready[0]
	if _, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: first.TaskID, ConfigHash: "v1", Platform: "feed", Stage: RecoveryDiscoverSource, ErrorCode: "PIPELINE_FAILED"}, 24*time.Hour, false, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ready, err = st.ListReadyFailedCollects(ctx, start.Add(2*time.Minute))
	if err != nil || len(ready) != 2 || ready[0].ID == first.ID {
		t.Fatalf("one failure starved another: %+v err=%v", ready, err)
	}
	changed, created, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: first.TaskID, ConfigHash: "v2", Platform: "feed", Stage: RecoveryDiscoverSource}, 24*time.Hour, true, start.Add(3*time.Minute))
	if err != nil || !created || changed.ID == first.ID || !changed.ReadyAt.IsZero() || !changed.FirstFailedAt.Equal(start.Add(3*time.Minute)) {
		t.Fatalf("changed=%+v created=%v err=%v", changed, created, err)
	}
	rows, err := st.ListFailedCollects(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err := st.DeleteFailedCollect(ctx, changed.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = st.ListFailedCollects(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("delete rows=%+v err=%v", rows, err)
	}
}

func TestLoginUnlocksAllEligibleFailures(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Now().UTC().Add(-time.Hour)
	for index := range 25 {
		if _, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: fmt.Sprintf("task-%d", index), ConfigHash: "v1", Platform: "toutiao", Stage: RecoveryDiscoverSource}, 24*time.Hour, true, start); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: "other-platform", ConfigHash: "v1", Platform: "douyin", Stage: RecoveryDiscoverSource}, 24*time.Hour, true, start); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []struct {
		id string
		at time.Time
	}{
		{id: "expired", at: start.Add(-25 * time.Hour)},
		{id: "after-login", at: start.Add(2 * time.Minute)},
	} {
		if _, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: failure.id, ConfigHash: "v1", Platform: "toutiao", Stage: RecoveryDiscoverSource}, 24*time.Hour, true, failure.at); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.RecordLogin(ctx, "toutiao", start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ready, err := st.ListReadyFailedCollects(ctx, start.Add(3*time.Minute))
	if err != nil || len(ready) != 25 {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	for _, row := range ready {
		if row.Platform != "toutiao" {
			t.Fatalf("unrelated platform unlocked: %+v", row)
		}
	}
}

func TestExpiredSourceFailureDoesNotBlockSchedule(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, _, err = st.UpsertFailedCollect(ctx, FailedCollect{TaskID: "source", ConfigHash: "v1", Platform: "feed", Stage: RecoveryDiscoverSource}, 24*time.Hour, true, time.Now().UTC().Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := st.HasFailedCollectForSource(ctx, "source")
	if err != nil || blocked {
		t.Fatalf("expired failure blocked schedule=%v err=%v", blocked, err)
	}
	feed := FeedItem{ContentKey: "post-1", URL: "https://example.com/post-1"}
	if _, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: "source", ConfigHash: "v1", Platform: "feed", Stage: RecoveryCollectItem, Feed: feed}, 24*time.Hour, true, time.Now().UTC().Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	blocked, err = st.HasFailedCollectForItem(ctx, "source", feed.ContentKey)
	if err != nil || blocked {
		t.Fatalf("expired item failure blocked manual collection=%v err=%v", blocked, err)
	}
}

func TestOldUnreleasedRecoveryTablesAreDiscarded(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, err = st.db.ExecContext(ctx, `CREATE TABLE dc_watchlist_login_recovery (recovery_id TEXT PRIMARY KEY, task_id TEXT, recovery_key TEXT, config_hash TEXT, platform TEXT, stage TEXT, feed_json TEXT, notify_channel TEXT, first_failed_at TEXT, expires_at TEXT, last_failed_at TEXT, attempts INTEGER, error_code TEXT, diagnostic TEXT, status TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Hour)
	_, err = st.db.ExecContext(ctx, `INSERT INTO dc_watchlist_login_recovery VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "old-1", "source", "source", "v1", "feed", RecoveryDiscoverSource, `{}`, "c1", start.Format(failureTimeFormat), start.Add(24*time.Hour).Format(failureTimeFormat), start.Format(failureTimeFormat), 0, "LOGIN_REQUIRED", "", "pending")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.ListFailedCollects(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("old state was copied into failed collects: rows=%+v err=%v", rows, err)
	}
	old, err := st.tableExists("dc_watchlist_login_recovery")
	if err != nil || old {
		t.Fatalf("old table survived=%v err=%v", old, err)
	}
}

func TestAutomaticReplayRetainsExpiredWindow(t *testing.T) {
	for _, waitLogin := range []bool{false, true} {
		t.Run(fmt.Sprintf("wait-login=%t", waitLogin), func(t *testing.T) {
			ctx := context.Background()
			st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
			first, _, err := st.UpsertFailedCollect(ctx, FailedCollect{TaskID: "source", ConfigHash: "v1", Platform: "feed", Stage: RecoveryDiscoverSource}, 24*time.Hour, false, start)
			if err != nil {
				t.Fatal(err)
			}
			first.ErrorCode = "PIPELINE_FAILED"
			again, created, err := st.UpsertFailedCollect(ctx, first, 24*time.Hour, waitLogin, start.Add(24*time.Hour))
			if err != nil || created || again.ID != first.ID || !again.FirstFailedAt.Equal(first.FirstFailedAt) || !again.ExpiresAt.Equal(first.ExpiresAt) || again.Attempts != first.Attempts+1 || !again.ReadyAt.IsZero() {
				t.Fatalf("automatic replay renewed expired window: row=%+v created=%v err=%v", again, created, err)
			}
			ready, err := st.ListReadyFailedCollects(ctx, start.Add(24*time.Hour))
			if err != nil || len(ready) != 0 {
				t.Fatalf("expired replay remains ready: rows=%+v err=%v", ready, err)
			}
			if err := st.DeleteFailedCollect(ctx, first.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := st.UpsertFailedCollect(ctx, first, 24*time.Hour, waitLogin, start.Add(25*time.Hour)); err == nil {
				t.Fatal("replay recreated a deleted record")
			}
		})
	}
}
