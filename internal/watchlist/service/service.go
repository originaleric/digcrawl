package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/notify"
	"github.com/originaleric/digcrawl/internal/summary"
	"github.com/originaleric/digcrawl/internal/watchlist"
	"gopkg.in/yaml.v3"
)

type Store = watchlist.Store
type WatchItem = watchlist.WatchItem
type FeedItem = watchlist.FeedItem
type RunSummary = watchlist.RunSummary
type TaskDefinition = watchlist.TaskDefinition
type CollectedItem = watchlist.CollectedItem

const (
	StatusDiscovered      = watchlist.StatusDiscovered
	StatusCollected       = watchlist.StatusCollected
	StatusSummarized      = watchlist.StatusSummarized
	StatusNotifyPending   = watchlist.StatusNotifyPending
	StatusNotified        = watchlist.StatusNotified
	StatusCollectFailed   = watchlist.StatusCollectFailed
	StatusSummaryFailed   = watchlist.StatusSummaryFailed
	StatusNotifyFailed    = watchlist.StatusNotifyFailed
	StatusNotifyFinalFail = watchlist.StatusNotifyFinalFail
	StatusSkippedLocked   = watchlist.StatusSkippedLocked
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
	st, err := openWatchlistStore(cfg)
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

func NewDiscoverService(cfg *config.Config) (*Service, error) {
	var st *watchlist.Store
	var err error
	if cfg == nil {
		cfg = &config.Config{}
	}
	switch cfg.Watchlist.SourceModeOrDefault() {
	case "database", "mixed":
		st, err = openWatchlistStore(cfg)
		if err != nil {
			return nil, err
		}
	}
	return &Service{cfg: cfg, store: st, feeds: FeedRegistry{}, owner: "digcrawl-discover"}, nil
}

func openWatchlistStore(cfg *config.Config) (*watchlist.Store, error) {
	driver := cfg.Watchlist.PersistenceDriverOrDefault()
	opts := watchlist.StoreOptions{Driver: driver}
	switch driver {
	case "sqlite", "sqlite3":
		opts.SQLitePath = cfg.Watchlist.StateDBPathOrDefault(cfg.Collect.DBPathOrDefault())
	case "mysql":
		dsn, err := watchlistMySQLDSN(cfg)
		if err != nil {
			return nil, err
		}
		opts.MySQLDSN = dsn
	default:
		return nil, fmt.Errorf("unsupported watchlist persistence driver %q", driver)
	}
	return watchlist.OpenStoreWithOptions(opts)
}

func OpenConfiguredStore(cfg *config.Config) (*watchlist.Store, error) {
	return openWatchlistStore(cfg)
}

func watchlistMySQLDSN(cfg *config.Config) (string, error) {
	if dsn := strings.TrimSpace(cfg.Watchlist.Persistence.MySQLDSN); dsn != "" {
		return os.ExpandEnv(dsn), nil
	}
	path := strings.TrimSpace(cfg.Watchlist.Persistence.GormConfigPath)
	if path == "" {
		path = "config/gorm.yml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("watchlist mysql dsn is empty and gorm config %q cannot be read: %w", path, err)
	}
	var gc struct {
		Gorm struct {
			Mysql struct {
				Write struct {
					Host     string `yaml:"Host"`
					DataBase string `yaml:"DataBase"`
					Port     int    `yaml:"Port"`
					User     string `yaml:"User"`
					Pass     string `yaml:"Pass"`
					Charset  string `yaml:"Charset"`
				} `yaml:"Write"`
			} `yaml:"Mysql"`
		} `yaml:"Gorm"`
	}
	if err := yaml.Unmarshal(data, &gc); err != nil {
		return "", fmt.Errorf("decode gorm config %q: %w", path, err)
	}
	w := gc.Gorm.Mysql.Write
	host := strings.TrimSpace(os.ExpandEnv(w.Host))
	user := strings.TrimSpace(os.ExpandEnv(w.User))
	pass := os.ExpandEnv(w.Pass)
	db := strings.TrimSpace(os.ExpandEnv(w.DataBase))
	if host == "" || user == "" || db == "" {
		return "", fmt.Errorf("gorm mysql write config %q must include host, user and database", path)
	}
	port := w.Port
	if port == 0 {
		port = 3306
	}
	charset := strings.TrimSpace(w.Charset)
	if charset == "" {
		charset = "utf8mb4"
	}
	mysqlCfg := mysql.Config{
		User:                 user,
		Passwd:               pass,
		Net:                  "tcp",
		Addr:                 host + ":" + strconv.Itoa(port),
		DBName:               db,
		Params:               map[string]string{"charset": charset},
		ParseTime:            true,
		Loc:                  time.Local,
		AllowNativePasswords: true,
	}
	return mysqlCfg.FormatDSN(), nil
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
	rawItems, err := s.configuredItems(ctx)
	if err != nil {
		return nil, err
	}
	for _, raw := range rawItems {
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

func (s *Service) RunConfiguredItem(ctx context.Context, id string) (RunSummary, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return RunSummary{}, fmt.Errorf("watchlist item id is required")
	}
	rawItems, err := s.configuredItems(ctx)
	if err != nil {
		return RunSummary{}, err
	}
	for _, raw := range rawItems {
		if strings.TrimSpace(raw.ID) != id {
			continue
		}
		item, err := s.normalizeItem(raw)
		if err != nil {
			return RunSummary{WatchID: id}, err
		}
		if !item.Enabled {
			return RunSummary{WatchID: id}, fmt.Errorf("watchlist item %q is disabled", id)
		}
		return s.RunItem(ctx, item)
	}
	return RunSummary{WatchID: id}, fmt.Errorf("watchlist item %q not found", id)
}

func (s *Service) DiscoverConfiguredItem(ctx context.Context, id string) ([]FeedItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("watchlist item id is required")
	}
	rawItems, err := s.configuredItems(ctx)
	if err != nil {
		return nil, err
	}
	for _, raw := range rawItems {
		if strings.TrimSpace(raw.ID) != id {
			continue
		}
		item, err := s.normalizeItem(raw)
		if err != nil {
			return nil, err
		}
		if !item.Enabled {
			return nil, fmt.Errorf("watchlist item %q is disabled", id)
		}
		feeds, err := s.feeds.ListLatest(ctx, item)
		if err != nil {
			return nil, err
		}
		limit := s.cfg.Watchlist.MaxItemsPerRunOrDefault()
		if limit > 0 && len(feeds) > limit {
			feeds = feeds[:limit]
		}
		return feeds, nil
	}
	return nil, fmt.Errorf("watchlist item %q not found", id)
}

func (s *Service) RunDue(ctx context.Context, now time.Time) ([]RunSummary, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var summaries []RunSummary
	var firstErr error
	activeTaskIDs := []string{}
	rawItems, err := s.configuredItems(ctx)
	if err != nil {
		return nil, err
	}
	for _, raw := range rawItems {
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
		key := watchlist.ContentKey(feed)
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
		msg := watchlist.RenderNotification(res)
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

func (s *Service) ConfiguredItemCount(ctx context.Context) (int, error) {
	items, err := s.configuredItems(ctx)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (s *Service) configuredItems(ctx context.Context) ([]config.WatchlistItem, error) {
	mode := s.cfg.Watchlist.SourceModeOrDefault()
	switch mode {
	case "yaml":
		return configuredWatchlistItems(s.cfg), nil
	case "database":
		return s.databaseWatchlistItems(ctx)
	case "mixed":
		items := configuredWatchlistItems(s.cfg)
		dbItems, err := s.databaseWatchlistItems(ctx)
		if err != nil {
			return nil, err
		}
		merged := make(map[string]int, len(items)+len(dbItems))
		out := make([]config.WatchlistItem, 0, len(items)+len(dbItems))
		for _, item := range items {
			key := strings.TrimSpace(item.ID)
			if key == "" {
				out = append(out, item)
				continue
			}
			merged[key] = len(out)
			out = append(out, item)
		}
		for _, item := range dbItems {
			key := strings.TrimSpace(item.ID)
			if idx, ok := merged[key]; ok && key != "" {
				out[idx] = item
				continue
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported watchlist source mode %q; expected yaml, database or mixed", mode)
	}
}

func (s *Service) databaseWatchlistItems(ctx context.Context) ([]config.WatchlistItem, error) {
	sources, err := s.store.ListSources(ctx, false)
	if err != nil {
		return nil, err
	}
	items := make([]config.WatchlistItem, 0, len(sources))
	for _, src := range sources {
		enabled := src.Enabled
		interval := ""
		if src.Interval > 0 {
			interval = src.Interval.String()
		}
		items = append(items, config.WatchlistItem{
			ID:       src.ID,
			Platform: src.Platform,
			Type:     src.Type,
			URL:      src.URL,
			Name:     src.Name,
			Interval: interval,
			NotifyTo: src.NotifyTo,
			Enabled:  &enabled,
			Options:  src.Options,
		})
	}
	return items, nil
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
	return watchlist.TaskDefinitionForItem(item)
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func optionString(opts map[string]any, key string) string {
	if opts == nil {
		return ""
	}
	v, ok := opts[key]
	if !ok {
		return ""
	}
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
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
