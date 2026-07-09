package watchlist

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("watchlist db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS watchlist_locks (
  watch_id TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS watchlist_runs (
  run_id TEXT PRIMARY KEY,
  watch_id TEXT NOT NULL,
  owner TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  discovered_count INTEGER DEFAULT 0,
  new_count INTEGER DEFAULT 0,
  notified_count INTEGER DEFAULT 0,
  error TEXT
)`,
		`CREATE TABLE IF NOT EXISTS watchlist_items (
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
)`,
		`CREATE TABLE IF NOT EXISTS watchlist_delivery (
  watch_id TEXT NOT NULL,
  content_key TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  status TEXT NOT NULL,
  notified_at TEXT,
  retry_count INTEGER DEFAULT 0,
	  next_retry_at TEXT,
	  notify_error TEXT,
	  message_title TEXT,
	  message_text TEXT,
	  PRIMARY KEY (watch_id, content_key, channel_id)
)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	_ = s.addColumnIfMissing("watchlist_delivery", "message_title", "TEXT")
	_ = s.addColumnIfMissing("watchlist_delivery", "message_text", "TEXT")
	return nil
}

func (s *Store) addColumnIfMissing(table, column, typ string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + typ)
	return err
}

func (s *Store) AcquireLock(ctx context.Context, watchID, owner string, ttl time.Duration) (bool, error) {
	now := time.Now().UTC()
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var expires string
	err = tx.QueryRowContext(ctx, `SELECT expires_at FROM watchlist_locks WHERE watch_id = ?`, watchID).Scan(&expires)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil {
		t, parseErr := time.Parse(time.RFC3339, expires)
		if parseErr == nil && t.After(now) {
			return false, tx.Commit()
		}
		_, err = tx.ExecContext(ctx, `UPDATE watchlist_locks SET owner = ?, acquired_at = ?, expires_at = ? WHERE watch_id = ?`,
			owner, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339), watchID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO watchlist_locks (watch_id, owner, acquired_at, expires_at) VALUES (?, ?, ?, ?)`,
			watchID, owner, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) ReleaseLock(ctx context.Context, watchID, owner string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM watchlist_locks WHERE watch_id = ? AND owner = ?`, watchID, owner)
}

func (s *Store) StartRun(ctx context.Context, watchID, owner string) (string, error) {
	runID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO watchlist_runs (run_id, watch_id, owner, started_at, status) VALUES (?, ?, ?, ?, ?)`,
		runID, watchID, owner, now, "running")
	return runID, err
}

func (s *Store) FinishRun(ctx context.Context, runID string, summary RunSummary, status string, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE watchlist_runs SET finished_at = ?, status = ?, discovered_count = ?, new_count = ?, notified_count = ?, error = ? WHERE run_id = ?`,
		time.Now().UTC().Format(time.RFC3339), status, summary.Discovered, summary.NewItems, summary.Notified, nullStr(errText), runID)
	return err
}

func (s *Store) UpsertDiscovered(ctx context.Context, watchID string, item FeedItem, baseline bool) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	key := contentKey(item)
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT content_key FROM watchlist_items WHERE watch_id = ? AND content_key = ?`, watchID, key).Scan(&existing)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE watchlist_items SET last_seen_at = ?, title = ?, url = ?, canonical_url = ?, published_at = ?, content_type = ? WHERE watch_id = ? AND content_key = ?`,
			now, nullStr(item.Title), item.URL, nullStr(item.CanonicalURL), timeStr(item.PublishedAt), nullStr(item.ContentType), watchID, key)
		return false, err
	}
	status := StatusDiscovered
	if baseline {
		status = StatusSkippedBaseline
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO watchlist_items (
  watch_id, content_key, platform_item_id, url, canonical_url, title, published_at, content_type,
  first_seen_at, last_seen_at, baseline_only, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		watchID, key, nullStr(item.ID), item.URL, nullStr(item.CanonicalURL), nullStr(item.Title),
		timeStr(item.PublishedAt), nullStr(item.ContentType), now, now, boolInt(baseline), status)
	return !baseline, err
}

func (s *Store) UpdateItemStatus(ctx context.Context, watchID, contentKey, status, collectErr, summaryErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE watchlist_items SET status = ?, collect_error = ?, summary_error = ? WHERE watch_id = ? AND content_key = ?`,
		status, nullStr(collectErr), nullStr(summaryErr), watchID, contentKey)
	return err
}

func (s *Store) ItemStatus(ctx context.Context, watchID, key string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM watchlist_items WHERE watch_id = ? AND content_key = ?`, watchID, key).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return status, err
}

type PendingDelivery struct {
	WatchID    string
	ContentKey string
	ChannelID  string
	Title      string
	Text       string
}

func (s *Store) UpsertDelivery(ctx context.Context, watchID, contentKey, channelID, status, errText string, retryable bool, messageTitle, messageText string) error {
	now := time.Now().UTC()
	var notified any
	if status == StatusNotified || status == "notified" {
		notified = now.Format(time.RFC3339)
		status = StatusNotified
	}
	nextRetry := ""
	if retryable {
		nextRetry = now.Add(5 * time.Minute).Format(time.RFC3339)
	} else if status == StatusNotifyFailed {
		status = StatusNotifyFinalFail
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO watchlist_delivery (
  watch_id, content_key, channel_id, status, notified_at, retry_count, next_retry_at, notify_error, message_title, message_text
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(watch_id, content_key, channel_id) DO UPDATE SET
  status = excluded.status,
  notified_at = COALESCE(excluded.notified_at, watchlist_delivery.notified_at),
  retry_count = watchlist_delivery.retry_count + CASE WHEN excluded.status = ? THEN 1 ELSE 0 END,
  next_retry_at = excluded.next_retry_at,
  notify_error = excluded.notify_error,
  message_title = COALESCE(excluded.message_title, watchlist_delivery.message_title),
  message_text = COALESCE(excluded.message_text, watchlist_delivery.message_text)`,
		watchID, contentKey, channelID, status, notified, 0, nullStr(nextRetry), nullStr(errText), nullStr(messageTitle), nullStr(messageText), StatusNotifyFailed)
	return err
}

func (s *Store) ListPendingDeliveries(ctx context.Context, watchID string, limit int) ([]PendingDelivery, error) {
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
SELECT watch_id, content_key, channel_id, COALESCE(message_title, ''), COALESCE(message_text, '')
FROM watchlist_delivery
WHERE watch_id = ? AND status = ? AND next_retry_at IS NOT NULL AND next_retry_at != '' AND next_retry_at <= ?
ORDER BY next_retry_at, content_key
LIMIT ?`, watchID, StatusNotifyFailed, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingDelivery
	for rows.Next() {
		var d PendingDelivery
		if err := rows.Scan(&d.WatchID, &d.ContentKey, &d.ChannelID, &d.Title, &d.Text); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) HasAnyItem(ctx context.Context, watchID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM watchlist_items WHERE watch_id = ?`, watchID).Scan(&n)
	return n > 0, err
}

func contentKey(item FeedItem) string {
	for _, v := range []string{item.ContentKey, item.ID, item.CanonicalURL, item.URL} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return uuid.NewString()
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func timeStr(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
