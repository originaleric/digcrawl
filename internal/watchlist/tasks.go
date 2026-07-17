package watchlist

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
)

func TaskDefinitionForItem(item WatchItem) TaskDefinition {
	def := TaskDefinition{
		ID:             item.ID,
		Type:           "watchlist.collect",
		Enabled:        item.Enabled,
		ScheduleMode:   "interval",
		ScheduleEvery:  item.Interval,
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
