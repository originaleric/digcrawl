package watchlist

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const failureTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// FailedCollect is the only pending recrawl record. Feed is transient: the
// original URL remains in the discovered-item table and the current source
// configuration remains in the task definition.
type FailedCollect struct {
	ID, TaskID, ConfigHash, Platform, Stage, NotifyTo   string
	ContentKeyHash, CookieDomain, ErrorCode, Diagnostic string
	Feed                                                FeedItem
	// ItemStatus is an existing content status written atomically with a failure.
	// It is transient and is not stored in the failed-collect table.
	ItemStatus                                      string
	FirstFailedAt, ExpiresAt, LastFailedAt, ReadyAt time.Time
	Attempts                                        int
}

func failedCollectKey(item FailedCollect) string {
	if item.Stage == RecoveryDiscoverSource {
		return "source"
	}
	url := strings.TrimSpace(item.Feed.CanonicalURL)
	if url == "" {
		url = strings.TrimSpace(item.Feed.URL)
	}
	return hashFailedText(url)
}

func hashFailedText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func failedContentKey(value string) string { return "sha256:" + hashFailedText(value) }

func safeFailedCookieDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" || len(domain) > 253 {
		return ""
	}
	for _, char := range domain {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '-' {
			continue
		}
		return ""
	}
	return domain
}

// UpsertFailedCollect keeps the first failure's expiry within one window.
// A new attempt (empty ID) after expiry starts a new window. A replay supplies
// the original ID and always retains that record's first failure and expiry.
// Login failures wait for a later login; a successful pipeline deletes the row.
func (s *Store) UpsertFailedCollect(ctx context.Context, item FailedCollect, lookback time.Duration, waitLogin bool, now time.Time) (FailedCollect, bool, error) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if item.TaskID == "" || item.Platform == "" || (item.Stage != RecoveryDiscoverSource && item.Stage != RecoveryCollectItem) || lookback <= 0 {
		return item, false, fmt.Errorf("invalid failed collect")
	}
	if item.Stage == RecoveryCollectItem && strings.TrimSpace(item.Feed.URL) == "" {
		return item, false, fmt.Errorf("failed item URL is required")
	}
	if item.ItemStatus != "" && (item.Stage != RecoveryCollectItem || (item.ItemStatus != StatusCollectFailed && item.ItemStatus != StatusSummaryFailed && item.ItemStatus != StatusNotifyFailed)) {
		return item, false, fmt.Errorf("invalid failed item status")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	key := failedCollectKey(item)
	item.Platform = strings.ToLower(strings.TrimSpace(item.Platform))
	item.ContentKeyHash = ""
	if item.Stage == RecoveryCollectItem {
		item.ContentKeyHash = failedContentKey(ContentKey(item.Feed))
	}
	if raw, ok := item.Feed.Raw["use_cookie_domain"].(string); ok {
		item.CookieDomain = safeFailedCookieDomain(raw)
	}
	var existingID, first, expires, oldHash, oldReady string
	var attempts int
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return item, false, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT failure_id, first_failed_at, expires_at, config_hash, COALESCE(ready_at, ''), attempts FROM dc_watchlist_failed_collects WHERE task_id=? AND stage=? AND failure_key=?`, item.TaskID, item.Stage, key).Scan(&existingID, &first, &expires, &oldHash, &oldReady, &attempts)
	if err != nil && err != sql.ErrNoRows {
		return item, false, err
	}
	found := err == nil
	// An automatic replay updates its original record, even if the pipeline
	// finishes after expiry. It must never turn that attempt into a new window.
	replay := item.ID != ""
	if replay && (!found || existingID != item.ID || oldHash != item.ConfigHash) {
		return item, false, fmt.Errorf("failed collect replay record no longer matches")
	}
	created := !found || oldHash != item.ConfigHash
	if found && !created && !replay {
		oldExpiry, parseErr := time.Parse(failureTimeFormat, expires)
		created = parseErr != nil || !now.Before(oldExpiry)
	}
	if created {
		item.ID = uuid.NewString()
		item.FirstFailedAt = now
		item.ExpiresAt = now.Add(lookback)
		item.Attempts = 0
	} else {
		item.ID = existingID
		item.FirstFailedAt, _ = time.Parse(failureTimeFormat, first)
		item.ExpiresAt, _ = time.Parse(failureTimeFormat, expires)
		item.Attempts = attempts + 1
	}
	item.LastFailedAt = now
	item.ReadyAt = time.Time{}
	if !waitLogin && now.Before(item.ExpiresAt) {
		if oldReady != "" && !created {
			item.ReadyAt, _ = time.Parse(failureTimeFormat, oldReady)
		} else {
			item.ReadyAt = now
		}
	}
	if found {
		_, err = tx.ExecContext(ctx, `UPDATE dc_watchlist_failed_collects SET failure_id=?, config_hash=?, platform=?, first_failed_at=?, expires_at=?, last_failed_at=?, ready_at=?, attempts=?, error_code=?, diagnostic=?, content_key_hash=?, cookie_domain=?, notify_channel=? WHERE failure_id=?`, item.ID, item.ConfigHash, item.Platform, item.FirstFailedAt.Format(failureTimeFormat), item.ExpiresAt.Format(failureTimeFormat), now.Format(failureTimeFormat), nullableFailureTime(item.ReadyAt), item.Attempts, item.ErrorCode, item.Diagnostic, item.ContentKeyHash, item.CookieDomain, item.NotifyTo, existingID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO dc_watchlist_failed_collects (failure_id,task_id,failure_key,config_hash,platform,stage,content_key_hash,cookie_domain,notify_channel,first_failed_at,expires_at,last_failed_at,ready_at,attempts,error_code,diagnostic) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.TaskID, key, item.ConfigHash, item.Platform, item.Stage, item.ContentKeyHash, item.CookieDomain, item.NotifyTo, item.FirstFailedAt.Format(failureTimeFormat), item.ExpiresAt.Format(failureTimeFormat), now.Format(failureTimeFormat), nullableFailureTime(item.ReadyAt), item.Attempts, item.ErrorCode, item.Diagnostic)
	}
	if err != nil {
		return item, created, err
	}
	if item.ItemStatus != "" {
		contentKey := ContentKey(item.Feed)
		var persistedKey string
		if err = tx.QueryRowContext(ctx, `SELECT content_key FROM dc_watchlist_items WHERE watch_id=? AND content_key=?`, item.TaskID, contentKey).Scan(&persistedKey); err != nil {
			return item, created, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE dc_watchlist_items SET status=? WHERE watch_id=? AND content_key=?`, item.ItemStatus, item.TaskID, contentKey); err != nil {
			return item, created, err
		}
	}
	return item, created, tx.Commit()
}

func nullableFailureTime(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at.UTC().Format(failureTimeFormat)
}

// RecordLogin unlocks every unexpired failure for this platform that predates
// the login. The service validates current task configuration before replay.
func (s *Store) RecordLogin(ctx context.Context, platform string, at time.Time) error {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		return fmt.Errorf("login platform is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	stamp := at.UTC().Format(failureTimeFormat)
	_, err := s.db.ExecContext(ctx, `UPDATE dc_watchlist_failed_collects SET ready_at=? WHERE platform=? AND ready_at IS NULL AND expires_at>? AND last_failed_at<?`, stamp, platform, stamp, stamp)
	return err
}

func (s *Store) ListFailedCollects(ctx context.Context) ([]FailedCollect, error) {
	return s.listFailedCollects(ctx, `SELECT failure_id,task_id,config_hash,platform,stage,content_key_hash,cookie_domain,notify_channel,first_failed_at,expires_at,last_failed_at,COALESCE(ready_at,''),attempts,error_code,diagnostic FROM dc_watchlist_failed_collects ORDER BY first_failed_at`, nil)
}

func (s *Store) ListReadyFailedCollects(ctx context.Context, now time.Time) ([]FailedCollect, error) {
	return s.listFailedCollects(ctx, `SELECT failure_id,task_id,config_hash,platform,stage,content_key_hash,cookie_domain,notify_channel,first_failed_at,expires_at,last_failed_at,COALESCE(ready_at,''),attempts,error_code,diagnostic FROM dc_watchlist_failed_collects WHERE ready_at IS NOT NULL AND expires_at>? ORDER BY attempts, last_failed_at, failure_id`, []any{now.UTC().Format(failureTimeFormat)})
}

func (s *Store) listFailedCollects(ctx context.Context, query string, args []any) ([]FailedCollect, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FailedCollect
	for rows.Next() {
		var item FailedCollect
		var first, expires, last, ready string
		if err := rows.Scan(&item.ID, &item.TaskID, &item.ConfigHash, &item.Platform, &item.Stage, &item.ContentKeyHash, &item.CookieDomain, &item.NotifyTo, &first, &expires, &last, &ready, &item.Attempts, &item.ErrorCode, &item.Diagnostic); err != nil {
			return nil, err
		}
		item.FirstFailedAt, _ = time.Parse(failureTimeFormat, first)
		item.ExpiresAt, _ = time.Parse(failureTimeFormat, expires)
		item.LastFailedAt, _ = time.Parse(failureTimeFormat, last)
		item.ReadyAt, _ = time.Parse(failureTimeFormat, ready)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) FailedCollectReady(ctx context.Context, id string, now time.Time) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM dc_watchlist_failed_collects WHERE failure_id=? AND ready_at IS NOT NULL AND expires_at>?`, id, now.UTC().Format(failureTimeFormat)).Scan(&count)
	return count == 1, err
}

func (s *Store) DeleteFailedCollect(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dc_watchlist_failed_collects WHERE failure_id=?`, id)
	return err
}

func (s *Store) DeleteFailedCollectFor(ctx context.Context, taskID, stage string, feed FeedItem) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dc_watchlist_failed_collects WHERE task_id=? AND stage=? AND failure_key=?`, taskID, stage, failedCollectKey(FailedCollect{Stage: stage, Feed: feed}))
	return err
}

func (s *Store) HasFailedCollectForItem(ctx context.Context, taskID, contentKey string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM dc_watchlist_failed_collects WHERE task_id=? AND stage=? AND content_key_hash=? AND expires_at>?`, taskID, RecoveryCollectItem, failedContentKey(contentKey), time.Now().UTC().Format(failureTimeFormat)).Scan(&count)
	return count > 0, err
}

func (s *Store) HasFailedCollectForSource(ctx context.Context, taskID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM dc_watchlist_failed_collects WHERE task_id=? AND stage=? AND expires_at>?`, taskID, RecoveryDiscoverSource, time.Now().UTC().Format(failureTimeFormat)).Scan(&count)
	return count > 0, err
}

func (s *Store) FailedCollectFeed(ctx context.Context, taskID, digest string) (FeedItem, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT content_key,url,COALESCE(canonical_url,''),COALESCE(title,''),COALESCE(published_at,''),COALESCE(content_type,'') FROM dc_watchlist_items WHERE watch_id=?`, taskID)
	if err != nil {
		return FeedItem{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var feed FeedItem
		var published string
		if err := rows.Scan(&feed.ContentKey, &feed.URL, &feed.CanonicalURL, &feed.Title, &published, &feed.ContentType); err != nil {
			return FeedItem{}, false, err
		}
		if failedContentKey(feed.ContentKey) == digest {
			feed.PublishedAt, _ = time.Parse(time.RFC3339, published)
			return feed, true, nil
		}
	}
	return FeedItem{}, false, rows.Err()
}

func (s *Store) ShouldRetryLoginReminder(ctx context.Context, id string, now time.Time) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM dc_watchlist_failed_collects WHERE failure_id=? AND ready_at IS NULL AND expires_at>?`, id, now.UTC().Format(failureTimeFormat)).Scan(&count)
	return count == 1, err
}

// The earlier recovery schema was never released from main. Do not copy its
// transient state into the single failed-collect table on replacement.
func (s *Store) migrateFailedCollects(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS dc_watchlist_login_recovery`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS dc_watchlist_login_generations`)
	return err
}
