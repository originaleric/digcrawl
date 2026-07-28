package watchlist

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func TaskDefinitionForItem(item WatchItem) TaskDefinition {
	mode := strings.ToLower(strings.TrimSpace(item.ScheduleMode))
	if mode == "" {
		mode = "interval"
	}
	def := TaskDefinition{
		ID:             item.ID,
		Type:           "watchlist.collect",
		Enabled:        item.Enabled,
		ScheduleMode:   mode,
		ScheduleEvery:  item.Interval,
		ScheduleAt:     strings.TrimSpace(item.ScheduleAt),
		SourcePlatform: item.Platform,
		SourceKind:     item.Type,
		SourceURL:      item.URL,
		NotifyChannel:  item.NotifyTo,
	}
	payload := map[string]any{
		"id":              def.ID,
		"type":            def.Type,
		"enabled":         def.Enabled,
		"schedule_mode":   def.ScheduleMode,
		"schedule_every":  def.ScheduleEvery.String(),
		"schedule_at":     def.ScheduleAt,
		"source_platform": def.SourcePlatform,
		"source_kind":     def.SourceKind,
		"source_url":      def.SourceURL,
		"notify_channel":  def.NotifyChannel,
		"options":         item.Options,
	}
	data, _ := json.Marshal(payload)
	sum := sha1.Sum(data)
	def.ConfigHash = hex.EncodeToString(sum[:])
	return def
}

func (def TaskDefinition) InitialNextRunAt(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	if strings.EqualFold(def.ScheduleMode, "daily") {
		return nextDailyRunAfter(now, def.ScheduleAt)
	}
	return now.UTC()
}

func (def TaskDefinition) NextRunAfter(finishedAt time.Time) time.Time {
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	if strings.EqualFold(def.ScheduleMode, "daily") {
		return nextDailyRunAfter(finishedAt, def.ScheduleAt)
	}
	if def.ScheduleEvery <= 0 {
		return finishedAt.UTC()
	}
	return finishedAt.UTC().Add(def.ScheduleEvery)
}

func nextDailyRunAfter(after time.Time, at string) time.Time {
	hour, minute, err := parseDailyAt(at)
	if err != nil {
		hour, minute = 9, 0
	}
	local := after.In(time.Local)
	next := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, time.Local)
	if !next.After(local) {
		next = next.Add(24 * time.Hour)
	}
	return next.UTC()
}

func ValidateSchedule(def TaskDefinition) error {
	switch strings.ToLower(strings.TrimSpace(def.ScheduleMode)) {
	case "", "interval":
		if def.ScheduleEvery <= 0 {
			return fmt.Errorf("interval schedule requires positive duration")
		}
	case "daily":
		if _, _, err := parseDailyAt(def.ScheduleAt); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported schedule mode %q", def.ScheduleMode)
	}
	return nil
}

func parseDailyAt(at string) (int, int, error) {
	at = strings.TrimSpace(at)
	if at == "" {
		return 9, 0, nil
	}
	t, err := time.Parse("15:04", at)
	if err != nil {
		return 0, 0, fmt.Errorf("daily schedule At must use HH:MM, got %q", at)
	}
	return t.Hour(), t.Minute(), nil
}
