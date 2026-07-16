package watchlist

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/notify"
	"github.com/originaleric/digcrawl/internal/summary"
)

type Service struct {
	cfg        *config.Config
	store      *Store
	collector  *collect.Service
	feeds      FeedRegistry
	summarizer summary.Provider
	notifier   notify.Notifier
	owner      string
}

func NewService(cfg *config.Config) (*Service, error) {
	if err := validateNotificationConfig(cfg); err != nil {
		return nil, err
	}
	if driver := cfg.Watchlist.PersistenceDriverOrDefault(); driver != "sqlite" {
		return nil, fmt.Errorf("unsupported watchlist persistence driver %q; only sqlite is supported", driver)
	}
	st, err := OpenStore(cfg.Watchlist.StateDBPathOrDefault(cfg.Collect.DBPathOrDefault()))
	if err != nil {
		return nil, err
	}
	collector, err := collect.NewService(cfg)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	fallback := summary.ExtractiveProvider{}
	b := cfg.Summary.InputBudgetOrDefault()
	budget := summary.InputBudget{
		TextChars: b.TextChars, ASRChars: b.ASRChars, OCRChars: b.OCRChars,
		MetadataChars: b.MetadataChars, MaxInputChars: cfg.Summary.MaxInputCharsOrDefault(),
	}
	var summarizer summary.Provider = fallback
	if cfg.Summary.EnabledOrDefault() {
		switch cfg.Summary.ProviderOrDefault() {
		case "digflow_runtime":
			if cfg.Summary.DigFlow.RuntimeModeOrDefault() == "http" {
				summarizer = summary.DigFlowProvider{
					BaseURL: cfg.Summary.DigFlow.BaseURL, Endpoint: cfg.Summary.DigFlow.Endpoint,
					Agent: cfg.Summary.DigFlow.Agent, AuthToken: cfg.Summary.DigFlow.AuthToken,
					Timeout: time.Duration(cfg.Summary.TimeoutSecOrDefault()) * time.Second,
					Budget:  budget, Fallback: fallback,
				}
			} else {
				summarizer = &summary.EmbeddedDigFlowProvider{
					RootDir: cfg.Summary.DigFlow.RootDir, ConfigPath: cfg.Summary.DigFlow.ConfigPath,
					GormConfigPath: cfg.Summary.DigFlow.GormConfigPath, EinoConfigPath: cfg.Summary.DigFlow.EinoConfigPath,
					ManifestPath: cfg.Summary.DigFlow.ManifestPath, GlobalToolsPath: cfg.Summary.DigFlow.GlobalToolsPath,
					GlobalSkillsDir: cfg.Summary.DigFlow.GlobalSkillsDir, Agent: cfg.Summary.DigFlow.Agent,
					DisableCheckpoint: cfg.Summary.DigFlow.DisableCheckpointOrDefault(),
					Timeout:           time.Duration(cfg.Summary.TimeoutSecOrDefault()) * time.Second,
					Budget:            budget, Fallback: fallback,
				}
			}
		case "digflow_http":
			summarizer = summary.DigFlowProvider{
				BaseURL: cfg.Summary.DigFlow.BaseURL, Endpoint: cfg.Summary.DigFlow.Endpoint,
				Agent: cfg.Summary.DigFlow.Agent, AuthToken: cfg.Summary.DigFlow.AuthToken,
				Timeout: time.Duration(cfg.Summary.TimeoutSecOrDefault()) * time.Second,
				Budget:  budget, Fallback: fallback,
			}
		case "llm_direct":
			summarizer = summary.LLMProvider{
				APIKey: cfg.Summary.LLM.APIKey, BaseURL: cfg.Summary.LLM.BaseURL, Model: cfg.Summary.LLM.Model,
				Timeout: time.Duration(cfg.Summary.TimeoutSecOrDefault()) * time.Second,
				Budget:  budget, Fallback: fallback,
			}
		default:
			summarizer = fallback
		}
	}
	notifier, err := newNotifier(cfg)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "digcrawl"
	}
	return &Service{
		cfg: cfg, store: st, collector: collector, feeds: FeedRegistry{},
		summarizer: summarizer, notifier: notifier,
		owner: fmt.Sprintf("%s-%d", host, time.Now().UnixNano()),
	}, nil
}

func (s *Service) Close() error {
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func newNotifier(cfg *config.Config) (notify.Notifier, error) {
	if cfg.Watchlist.DryRunOrDefault() {
		return notify.NoopNotifier{}, nil
	}
	if cfg.Notifications.Feishu.EnabledOrDefault() {
		return notify.NewFeishu(cfg.Notifications.Feishu), nil
	}
	if cfg.Watchlist.EnabledOrDefault() {
		return nil, fmt.Errorf("watchlist notifications are not configured; enable Notifications.Feishu or set Watchlist.DryRun=true")
	}
	return notify.NoopNotifier{}, nil
}

func validateNotificationConfig(cfg *config.Config) error {
	if !cfg.Watchlist.EnabledOrDefault() || cfg.Watchlist.DryRunOrDefault() {
		return nil
	}
	feishu := cfg.Notifications.Feishu
	if !feishu.EnabledOrDefault() {
		return fmt.Errorf("watchlist notifications are not configured; enable Notifications.Feishu or set Watchlist.DryRun=true")
	}
	channels := map[string]config.FeishuChannel{}
	defaultID := strings.TrimSpace(feishu.DefaultChannel)
	for _, ch := range feishu.Channels {
		id := strings.TrimSpace(ch.ID)
		if id == "" {
			continue
		}
		if _, exists := channels[id]; exists {
			return fmt.Errorf("feishu notification channel %q is duplicated", id)
		}
		channels[id] = ch
		if defaultID == "" {
			defaultID = id
		}
	}
	if len(channels) == 0 {
		return fmt.Errorf("feishu notifications require at least one channel")
	}
	if defaultID == "" {
		return fmt.Errorf("feishu default channel is required")
	}
	if err := validateFeishuChannel(channels, defaultID, "default channel"); err != nil {
		return err
	}
	for _, item := range configuredWatchlistItems(cfg) {
		if !item.EnabledOrDefault() {
			continue
		}
		id := strings.TrimSpace(item.NotifyTo)
		if id == "" {
			continue
		}
		context := fmt.Sprintf("watchlist item %q NotifyTo", strings.TrimSpace(item.ID))
		if err := validateFeishuChannel(channels, id, context); err != nil {
			return err
		}
	}
	return nil
}

func validateFeishuChannel(channels map[string]config.FeishuChannel, id, context string) error {
	ch, ok := channels[id]
	if !ok {
		return fmt.Errorf("feishu %s references missing channel %q", context, id)
	}
	if strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(ch.WebhookURL))) == "" {
		return fmt.Errorf("feishu channel %q webhook url is required", id)
	}
	return nil
}

func (s *Service) RunOnce(ctx context.Context) ([]RunSummary, error) {
	var summaries []RunSummary
	var firstErr error
	for _, raw := range configuredWatchlistItems(s.cfg) {
		item, err := s.normalizeItem(raw)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !item.Enabled {
			continue
		}
		sum, err := s.RunItem(ctx, item)
		summaries = append(summaries, sum)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return summaries, firstErr
}

func (s *Service) RunDue(ctx context.Context, now time.Time) ([]RunSummary, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var summaries []RunSummary
	var firstErr error
	activeTaskIDs := []string{}
	for _, raw := range configuredWatchlistItems(s.cfg) {
		item, err := s.normalizeItem(raw)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		def := taskDefinition(item)
		activeTaskIDs = append(activeTaskIDs, def.ID)
		if err := s.store.UpsertTaskDefinition(ctx, def, now); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !item.Enabled {
			continue
		}
		due, err := s.store.IsTaskDue(ctx, def.ID, now)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !due {
			continue
		}
		sum, err := s.runScheduledTask(ctx, item, def)
		summaries = append(summaries, sum)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.store.DisableMissingTaskDefinitions(ctx, "watchlist.collect", activeTaskIDs, now); err != nil && firstErr == nil {
		firstErr = err
	}
	return summaries, firstErr
}

func (s *Service) RunItem(ctx context.Context, item WatchItem) (RunSummary, error) {
	sum := RunSummary{WatchID: item.ID}
	lockTTL, _ := time.ParseDuration(s.cfg.Watchlist.LockTTLOrDefault())
	locked, err := s.store.AcquireLock(ctx, item.ID, s.owner, lockTTL)
	if err != nil {
		return sum, err
	}
	if !locked {
		sum.SkippedLocked = true
		return sum, nil
	}
	defer s.store.ReleaseLock(context.Background(), item.ID, s.owner)
	runID, err := s.store.StartRun(ctx, item.ID, s.owner)
	if err != nil {
		return sum, err
	}
	sum.RunID = runID
	status := "success"
	errText := ""
	defer func() {
		_ = s.store.FinishRun(context.Background(), runID, sum, status, errText)
	}()

	hadItems, err := s.store.HasAnyItem(ctx, item.ID)
	if err != nil {
		status = "failed"
		errText = err.Error()
		return sum, err
	}
	baseline := !hadItems && !s.cfg.Watchlist.NotifyOnFirstRunOrDefault()
	retried, err := s.retryPendingDeliveries(ctx, item)
	if err != nil {
		sum.Errors = append(sum.Errors, err.Error())
	} else {
		sum.Notified += retried
	}
	feeds, err := s.feeds.ListLatest(ctx, item)
	if err != nil {
		status = "failed"
		errText = err.Error()
		sum.Errors = append(sum.Errors, err.Error())
		return sum, err
	}
	limit := s.cfg.Watchlist.MaxItemsPerRunOrDefault()
	if limit > 0 && len(feeds) > limit {
		feeds = feeds[:limit]
	}
	sum.Discovered = len(feeds)
	for _, feed := range feeds {
		key := contentKey(feed)
		isNew, err := s.store.UpsertDiscovered(ctx, item.ID, feed, baseline)
		if err != nil {
			sum.Errors = append(sum.Errors, err.Error())
			continue
		}
		if baseline {
			continue
		}
		if !isNew {
			st, err := s.store.ItemStatus(ctx, item.ID, key)
			if err != nil {
				sum.Errors = append(sum.Errors, err.Error())
				continue
			}
			if !retryableItemStatus(st) {
				continue
			}
		} else {
			sum.NewItems++
		}
		collected, err := s.collectFeedItem(ctx, feed)
		if err != nil {
			_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusCollectFailed, err.Error(), "")
			sum.Errors = append(sum.Errors, err.Error())
			continue
		}
		sum.Collected++
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusCollected, "", "")
		req := summary.Request{
			WatchID: item.ID, Platform: item.Platform, SourceName: item.Name, RunTime: time.Now().Format(time.RFC3339),
			Items: []summary.Item{{
				ItemID: key, URL: feed.URL, Title: firstNonEmpty(collected.Title, feed.Title),
				PublishedAt: feed.PublishedAt.Format(time.RFC3339),
				Text:        collected.Text, ASRText: collected.ASRText, OCRText: collected.OCRText, Metadata: collected.Metadata,
			}},
		}
		res, err := s.summarizer.Summarize(ctx, req)
		if err != nil {
			_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusSummaryFailed, "", err.Error())
			sum.Errors = append(sum.Errors, err.Error())
			continue
		}
		collected.Summary = res
		sum.Summarized++
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusSummarized, "", res.Error)
		msg := RenderNotification(res)
		notifyResult := s.notifier.Send(ctx, item.NotifyTo, msg)
		if notifyResult.Status == "notified" || notifyResult.Status == StatusNotified {
			sum.Notified++
			_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusNotified, "", "")
			_ = s.store.UpsertDelivery(ctx, item.ID, key, notifyResult.ChannelID, StatusNotified, "", false, msg.Title, msg.Text)
			continue
		}
		status := StatusNotifyFailed
		if !notifyResult.Retryable {
			status = StatusNotifyFinalFail
		}
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, status, "", "")
		_ = s.store.UpsertDelivery(ctx, item.ID, key, notifyResult.ChannelID, StatusNotifyFailed, notifyResult.Error, notifyResult.Retryable, msg.Title, msg.Text)
		sum.Errors = append(sum.Errors, notifyResult.Error)
	}
	if len(sum.Errors) > 0 {
		status = "partial"
		errText = strings.Join(sum.Errors, "; ")
	}
	return sum, nil
}

func (s *Service) runScheduledTask(ctx context.Context, item WatchItem, def TaskDefinition) (RunSummary, error) {
	sum := RunSummary{WatchID: item.ID}
	lockTTL, _ := time.ParseDuration(s.cfg.Watchlist.LockTTLOrDefault())
	locked, err := s.store.AcquireTaskLock(ctx, def.ID, s.owner, lockTTL)
	if err != nil {
		return sum, err
	}
	if !locked {
		sum.SkippedLocked = true
		return sum, s.store.InsertTaskEvent(ctx, def.ID, "", "run_skipped", StatusSkippedLocked, "task lock is held", "")
	}
	defer s.store.ReleaseTaskLock(context.Background(), def.ID, s.owner)
	taskRunID, err := s.store.StartTaskRun(ctx, def, s.owner)
	if err != nil {
		return sum, err
	}
	status := "success"
	errText := ""
	defer func() {
		metadata := map[string]any{
			"watch_id":       sum.WatchID,
			"watch_run_id":   sum.RunID,
			"discovered":     sum.Discovered,
			"new_items":      sum.NewItems,
			"collected":      sum.Collected,
			"summarized":     sum.Summarized,
			"notified":       sum.Notified,
			"skipped_locked": sum.SkippedLocked,
			"errors":         sum.Errors,
		}
		data, _ := json.Marshal(metadata)
		_ = s.store.FinishTaskRun(context.Background(), taskRunID, def, status, errText, string(data), time.Now())
	}()
	sum, err = s.RunItem(ctx, item)
	if sum.SkippedLocked {
		status = "skipped_locked"
		errText = "watchlist lock is held"
		return sum, err
	}
	if err != nil {
		status = "failed"
		errText = err.Error()
		return sum, err
	}
	if len(sum.Errors) > 0 {
		status = "partial"
		errText = strings.Join(sum.Errors, "; ")
	}
	return sum, nil
}

func (s *Service) retryPendingDeliveries(ctx context.Context, item WatchItem) (int, error) {
	pending, err := s.store.ListPendingDeliveries(ctx, item.ID, s.cfg.Watchlist.MaxItemsPerRunOrDefault())
	if err != nil {
		return 0, err
	}
	notified := 0
	for _, d := range pending {
		if strings.TrimSpace(d.Text) == "" {
			_ = s.store.UpsertDelivery(ctx, d.WatchID, d.ContentKey, d.ChannelID, StatusNotifyFailed, "missing delivery message", false, d.Title, d.Text)
			_ = s.store.UpdateItemStatus(ctx, d.WatchID, d.ContentKey, StatusNotifyFinalFail, "", "")
			continue
		}
		res := s.notifier.Send(ctx, d.ChannelID, notify.Message{Title: d.Title, Text: d.Text})
		if res.Status == StatusNotified || res.Status == "notified" {
			notified++
			_ = s.store.UpdateItemStatus(ctx, d.WatchID, d.ContentKey, StatusNotified, "", "")
			_ = s.store.UpsertDelivery(ctx, d.WatchID, d.ContentKey, d.ChannelID, StatusNotified, "", false, d.Title, d.Text)
			continue
		}
		status := StatusNotifyFailed
		if !res.Retryable {
			status = StatusNotifyFinalFail
		}
		_ = s.store.UpdateItemStatus(ctx, d.WatchID, d.ContentKey, status, "", "")
		_ = s.store.UpsertDelivery(ctx, d.WatchID, d.ContentKey, d.ChannelID, StatusNotifyFailed, res.Error, res.Retryable, d.Title, d.Text)
	}
	return notified, nil
}

func (s *Service) normalizeItem(raw config.WatchlistItem) (WatchItem, error) {
	if strings.TrimSpace(raw.ID) == "" {
		return WatchItem{}, fmt.Errorf("watchlist item id is required")
	}
	if errText := optionString(raw.Options, "_task_config_error"); errText != "" {
		return WatchItem{}, fmt.Errorf("watchlist item %s invalid task config: %s", raw.ID, errText)
	}
	if errText := optionString(raw.Options, "_task_schedule_error"); errText != "" {
		return WatchItem{}, fmt.Errorf("watchlist item %s invalid schedule: %s", raw.ID, errText)
	}
	interval := s.cfg.Watchlist.DefaultIntervalOrDefault()
	if strings.TrimSpace(raw.Interval) != "" {
		interval = raw.Interval
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		return WatchItem{}, fmt.Errorf("watchlist item %s invalid interval: %w", raw.ID, err)
	}
	return WatchItem{
		ID: strings.TrimSpace(raw.ID), Platform: strings.ToLower(strings.TrimSpace(raw.Platform)),
		Type: strings.ToLower(strings.TrimSpace(raw.Type)), URL: strings.TrimSpace(raw.URL),
		Name: strings.TrimSpace(raw.Name), Interval: d, NotifyTo: strings.TrimSpace(raw.NotifyTo), Enabled: raw.EnabledOrDefault(),
		Options: raw.Options,
	}, nil
}

func ConfiguredItemCount(cfg *config.Config) int {
	return len(configuredWatchlistItems(cfg))
}

func configuredWatchlistItems(cfg *config.Config) []config.WatchlistItem {
	if cfg == nil {
		return nil
	}
	items := make([]config.WatchlistItem, 0, len(cfg.Watchlist.Items)+len(cfg.Tasks))
	items = append(items, cfg.Watchlist.Items...)
	for _, task := range cfg.Tasks {
		items = append(items, watchlistItemFromTask(task))
	}
	return items
}

func watchlistItemFromTask(task config.TaskConfig) config.WatchlistItem {
	typ := strings.ToLower(strings.TrimSpace(task.Type))
	if typ == "" {
		typ = "watchlist.collect"
	}
	options := map[string]any{}
	for k, v := range task.Source.Options {
		options[k] = v
	}
	for k, v := range task.Options {
		options[k] = v
	}
	switch typ {
	case "watchlist.collect", "feed.digest":
	default:
		options["_task_config_error"] = fmt.Sprintf("unsupported task type %q", task.Type)
	}
	mode := strings.ToLower(strings.TrimSpace(task.Schedule.Mode))
	switch mode {
	case "", "interval":
	default:
		options["_task_schedule_error"] = fmt.Sprintf("unsupported mode %q; only interval is supported", task.Schedule.Mode)
	}
	enabled := task.EnabledOrDefault()
	interval := strings.TrimSpace(task.Schedule.Every)
	return config.WatchlistItem{
		ID:       strings.TrimSpace(task.ID),
		Platform: strings.TrimSpace(task.Source.Platform),
		Type:     strings.TrimSpace(task.Source.Kind),
		URL:      strings.TrimSpace(task.Source.URL),
		Name:     strings.TrimSpace(task.Name),
		Interval: interval,
		NotifyTo: strings.TrimSpace(task.Notify.Channel),
		Enabled:  &enabled,
		Options:  options,
	}
}

func taskDefinition(item WatchItem) TaskDefinition {
	mode := "interval"
	def := TaskDefinition{
		ID:             item.ID,
		Type:           "watchlist.collect",
		Enabled:        item.Enabled,
		ScheduleMode:   mode,
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

func (s *Service) collectFeedItem(ctx context.Context, feed FeedItem) (CollectedItem, error) {
	result, err := s.collector.CollectURL(ctx, feed.URL, []string{"md", "json"}, s.cfg.Collect.OutputDir)
	if err != nil {
		return CollectedItem{Feed: feed}, err
	}
	out := map[string]any{}
	if result.Paths.JSON == "" {
		return CollectedItem{Feed: feed}, fmt.Errorf("collect did not produce json output for %s", feed.URL)
	}
	data, err := os.ReadFile(result.Paths.JSON)
	if err != nil {
		return CollectedItem{Feed: feed}, fmt.Errorf("read collect json: %w", err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return CollectedItem{Feed: feed}, fmt.Errorf("decode collect json: %w", err)
	}
	return CollectedItem{
		Feed: feed, Title: str(out["title"]), Text: firstNonEmpty(str(out["text"]), str(out["markdown"])),
		ASRText: extractMetadataText(out, "asr_text"), OCRText: extractOCRText(out),
		Metadata: extractMetadata(out),
	}, nil
}

func retryableItemStatus(status string) bool {
	switch status {
	case StatusDiscovered, StatusCollected, StatusCollectFailed, StatusSummaryFailed, StatusNotifyFailed, StatusNotifyPending, StatusSummarized:
		return true
	default:
		return false
	}
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func extractMetadata(out map[string]any) map[string]any {
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	if engagement, ok := out["engagement"].(map[string]any); ok {
		for k, v := range engagement {
			meta[k] = v
		}
	}
	return meta
}

func extractMetadataText(out map[string]any, key string) string {
	if v := str(out[key]); v != "" {
		return v
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	return str(meta[key])
}

func extractOCRText(out map[string]any) string {
	meta, _ := out["platform_metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	if s := str(meta["ocr_text"]); s != "" {
		return s
	}
	if arr, ok := meta["ocr_results"].([]any); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if text := str(m["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
