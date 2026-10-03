package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/watchlist"
)

func (s *Service) queueFailedCollect(ctx context.Context, item WatchItem, stage string, feed FeedItem, failure error, waitLogin bool, itemStatus, failureID string) (watchlist.FailedCollect, bool, error) {
	lookback, err := time.ParseDuration(s.cfg.Watchlist.LoginRecovery.LookbackOrDefault())
	if err != nil || lookback <= 0 {
		return watchlist.FailedCollect{}, false, fmt.Errorf("invalid failed-collect lookback")
	}
	code := "PIPELINE_FAILED"
	if waitLogin {
		var readErr *platform.ReadError
		if !errors.As(failure, &readErr) || readErr.Code != platform.ErrLoginRequired {
			return watchlist.FailedCollect{}, false, fmt.Errorf("login recovery requires structured LOGIN_REQUIRED")
		}
		code = readErr.Code
	}
	row := watchlist.FailedCollect{ID: failureID, TaskID: item.ID, ConfigHash: taskDefinition(item).ConfigHash,
		Platform: item.Platform, Stage: stage, Feed: feed, NotifyTo: s.effectiveNotifyChannel(item),
		ErrorCode: code, Diagnostic: code, ItemStatus: itemStatus}
	return s.store.UpsertFailedCollect(ctx, row, lookback, waitLogin, time.Now().UTC())
}

func (s *Service) queueLoginRecovery(ctx context.Context, item WatchItem, stage string, feed FeedItem, failure error, itemStatus, failureID string) (watchlist.FailedCollect, bool, error) {
	row, created, err := s.queueFailedCollect(ctx, item, stage, feed, failure, true, itemStatus, failureID)
	if err != nil || created || !row.ReadyAt.IsZero() || !time.Now().Before(row.ExpiresAt) {
		return row, created, err
	}
	recorded, err := s.store.HasDelivery(ctx, item.ID, "__login_required__:"+row.ID)
	return row, !recorded, err
}

func (s *Service) effectiveNotifyChannel(item WatchItem) string {
	if id := strings.TrimSpace(item.NotifyTo); id != "" {
		return id
	}
	if s.cfg == nil {
		return ""
	}
	if id := strings.TrimSpace(s.cfg.Notifications.Feishu.DefaultChannel); id != "" {
		return id
	}
	if len(s.cfg.Notifications.Feishu.Channels) > 0 {
		return strings.TrimSpace(s.cfg.Notifications.Feishu.Channels[0].ID)
	}
	return ""
}

func (s *Service) validateCurrentChannel(item WatchItem) error {
	if s.cfg.Watchlist.DryRunOrDefault() {
		return nil
	}
	channels, _, err := validatedFeishuChannels(s.cfg)
	if err != nil {
		return err
	}
	return validateFeishuChannel(channels, s.effectiveNotifyChannel(item), "watchlist item "+item.ID)
}

func (s *Service) currentRecoveryItem(row watchlist.FailedCollect, current map[string]WatchItem) (WatchItem, string) {
	item, ok := current[row.TaskID]
	if !ok || !item.Enabled || taskDefinition(item).ConfigHash != row.ConfigHash || s.effectiveNotifyChannel(item) != row.NotifyTo || item.Platform != row.Platform {
		return WatchItem{}, "task disabled, deleted, or changed"
	}
	item.NotifyTo = row.NotifyTo
	if s.cfg.Watchlist.DryRunOrDefault() {
		return item, ""
	}
	channels, defaultID, err := validatedFeishuChannels(s.cfg)
	if err != nil {
		return WatchItem{}, "notification channel unavailable"
	}
	channel := strings.TrimSpace(item.NotifyTo)
	if channel == "" {
		channel = defaultID
	}
	if err := validateFeishuChannel(channels, channel, "failed collect"); err != nil {
		return WatchItem{}, "notification channel unavailable"
	}
	return item, ""
}

func (s *Service) prepareLoginRecovery(ctx context.Context, current map[string]WatchItem, invalidIDs map[string]bool) error {
	rows, err := s.store.ListFailedCollects(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	now := time.Now()
	for _, row := range rows {
		if !now.Before(row.ExpiresAt) {
			if err := s.store.DeleteFailedCollect(ctx, row.ID); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if invalidIDs[row.TaskID] {
			continue
		}
		item, reason := s.currentRecoveryItem(row, current)
		if reason == "notification channel unavailable" {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed collect %s: %s", row.ID, reason)
			}
			continue
		}
		if reason != "" {
			if err := s.store.DeleteFailedCollect(ctx, row.ID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}
			continue
		}
		if row.ErrorCode != platform.ErrLoginRequired || !row.ReadyAt.IsZero() {
			continue
		}
		key := "__login_required__:" + row.ID
		if _, err := s.notifyLoginRequiredWithKey(ctx, item, key, row.Stage); err != nil && firstErr == nil {
			firstErr = err
		}
		if _, err := s.retryPendingDeliveries(ctx, item); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) replayLoginRecovery(ctx context.Context, now time.Time, current map[string]WatchItem, invalidIDs map[string]bool) error {
	rows, err := s.store.ListReadyFailedCollects(ctx, now)
	if err != nil {
		return err
	}
	var firstErr error
	for _, row := range rows {
		if invalidIDs[row.TaskID] {
			continue
		}
		ready, err := s.store.FailedCollectReady(ctx, row.ID, time.Now())
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !ready {
			continue
		}
		if err := s.replayOneLoginRecovery(ctx, row, current); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) replayOneLoginRecovery(ctx context.Context, row watchlist.FailedCollect, current map[string]WatchItem) error {
	item, reason := s.currentRecoveryItem(row, current)
	if reason == "notification channel unavailable" {
		return fmt.Errorf("failed collect %s: %s", row.ID, reason)
	}
	if reason != "" {
		return s.store.DeleteFailedCollect(ctx, row.ID)
	}
	ttl, err := time.ParseDuration(s.cfg.Watchlist.LockTTLOrDefault())
	if err != nil || ttl <= 0 {
		ttl = 30 * time.Minute
	}
	owner := s.owner + ":" + uuid.NewString()
	locked, err := s.store.AcquireTaskLock(ctx, item.ID, owner, ttl)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("task lock held during failed collect for %s", item.ID)
	}
	defer s.store.ReleaseTaskLock(context.Background(), item.ID, owner)
	ctx, stop := keepLockAlive(ctx, ttl, func(renewCtx context.Context) (bool, error) {
		return s.store.RenewTaskLock(renewCtx, item.ID, owner, ttl)
	})
	defer stop()
	if row.Stage == watchlist.RecoveryDiscoverSource {
		result, runErr := s.runItem(ctx, item, false, row.ID)
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		if result.SkippedLocked {
			return fmt.Errorf("watchlist lock held during failed collect for %s", item.ID)
		}
		return runErr
	}
	watchOwner := s.owner + ":" + uuid.NewString()
	locked, err = s.store.AcquireLock(ctx, item.ID, watchOwner, ttl)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("watchlist lock held during failed collect for %s", item.ID)
	}
	defer s.store.ReleaseLock(context.Background(), item.ID, watchOwner)
	ctx, stopWatch := keepLockAlive(ctx, ttl, func(renewCtx context.Context) (bool, error) {
		return s.store.RenewLock(renewCtx, item.ID, watchOwner, ttl)
	})
	defer stopWatch()
	feed, found, err := s.store.FailedCollectFeed(ctx, item.ID, row.ContentKeyHash)
	if err != nil {
		return err
	}
	if !found || strings.TrimSpace(feed.URL) == "" {
		return s.store.DeleteFailedCollect(ctx, row.ID)
	}
	if row.CookieDomain != "" {
		feed.Raw = map[string]any{"use_cookie_domain": row.CookieDomain}
	}
	key := feed.ContentKey
	if notified, err := s.store.HasNotifiedDelivery(ctx, item.ID, key); err != nil {
		return err
	} else if notified {
		return s.store.DeleteFailedCollect(ctx, row.ID)
	}
	// A persisted delivery owns notification retries, including final failures.
	// A leftover collect row can only result from failure to delete it after
	// that handoff and must not trigger another collection or send.
	if delivered, err := s.store.HasDelivery(ctx, item.ID, key); err != nil {
		return err
	} else if delivered {
		return s.store.DeleteFailedCollect(ctx, row.ID)
	}
	result := s.processPendingFeed(ctx, item, feed, key, row.ID)
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	if result.handoffError != nil {
		return result.handoffError
	}
	if len(result.errors) > 0 {
		return errors.New(strings.Join(result.errors, "; "))
	}
	return nil
}
