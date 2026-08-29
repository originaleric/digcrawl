package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
type FeedReader = watchlist.FeedReader
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
	feeds      FeedReader
	summarizer summary.Provider
	notifier   notify.Notifier
	owner      string
}

type pendingFeed struct {
	feed FeedItem
	key  string
}

type itemResult struct {
	collected  int
	summarized int
	notified   int
	activity   int
	errors     []string
}

func NewService(cfg *config.Config) (*Service, error) {
	if err := validateNotificationConfig(cfg); err != nil {
		return nil, err
	}
	st, err := openWatchlistStore(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateDatabaseNotificationConfig(context.Background(), cfg, st); err != nil {
		_ = st.Close()
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
	channels, _, err := validatedFeishuChannels(cfg)
	if err != nil {
		return err
	}
	if cfg.Watchlist.SourceModeOrDefault() != "database" {
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
	}
	return nil
}

func validatedFeishuChannels(cfg *config.Config) (map[string]config.FeishuChannel, string, error) {
	feishu := cfg.Notifications.Feishu
	if !feishu.EnabledOrDefault() {
		return nil, "", fmt.Errorf("watchlist notifications are not configured; enable Notifications.Feishu or set Watchlist.DryRun=true")
	}
	channels := map[string]config.FeishuChannel{}
	defaultID := strings.TrimSpace(feishu.DefaultChannel)
	for _, ch := range feishu.Channels {
		id := strings.TrimSpace(ch.ID)
		if id == "" {
			continue
		}
		if _, exists := channels[id]; exists {
			return nil, "", fmt.Errorf("feishu notification channel %q is duplicated", id)
		}
		channels[id] = ch
		if defaultID == "" {
			defaultID = id
		}
	}
	for _, ch := range channels {
		if isFeishuAppChannel(ch) {
			if strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(feishu.AppID))) == "" || strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(feishu.AppSecret))) == "" {
				return nil, "", fmt.Errorf("feishu app channel requires AppID and AppSecret")
			}
			break
		}
	}
	if len(channels) == 0 {
		return nil, "", fmt.Errorf("feishu notifications require at least one channel")
	}
	if defaultID == "" {
		return nil, "", fmt.Errorf("feishu default channel is required")
	}
	if err := validateFeishuChannel(channels, defaultID, "default channel"); err != nil {
		return nil, "", err
	}
	return channels, defaultID, nil
}

func validateDatabaseNotificationConfig(ctx context.Context, cfg *config.Config, st *watchlist.Store) error {
	mode := cfg.Watchlist.SourceModeOrDefault()
	if !cfg.Watchlist.EnabledOrDefault() || cfg.Watchlist.DryRunOrDefault() || (mode != "database" && mode != "mixed") {
		return nil
	}
	channels, _, err := validatedFeishuChannels(cfg)
	if err != nil {
		return err
	}
	sources, err := st.ListSources(ctx, true)
	if err != nil {
		return fmt.Errorf("validate database watchlist notifications: %w", err)
	}
	for _, src := range sources {
		id := strings.TrimSpace(src.NotifyTo)
		if id == "" {
			continue
		}
		context := fmt.Sprintf("database watchlist source %q NotifyTo", strings.TrimSpace(src.ID))
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
	if isFeishuAppChannel(ch) {
		if strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(ch.ReceiveID))) == "" {
			return fmt.Errorf("feishu channel %q receive id is required", id)
		}
		return nil
	}
	if strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(ch.WebhookURL))) == "" {
		return fmt.Errorf("feishu channel %q webhook url is required", id)
	}
	return nil
}

func isFeishuAppChannel(ch config.FeishuChannel) bool {
	mode := strings.ToLower(strings.TrimSpace(ch.Mode))
	return mode == "app" || mode == "bot" || mode == "openapi"
}

func (s *Service) RunOnce(ctx context.Context) ([]RunSummary, error) {
	rawItems, err := s.configuredItems(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]WatchItem, 0, len(rawItems))
	var firstErr error
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
		items = append(items, item)
	}
	summaries, runErr := runBounded(s.cfg.Watchlist.Scheduler.MaxConcurrentRunsOrDefault(), items, func(item WatchItem) (RunSummary, error) {
		return s.RunItem(ctx, item)
	})
	if firstErr == nil {
		firstErr = runErr
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
	var firstErr error
	type scheduledItem struct {
		item WatchItem
		def  TaskDefinition
	}
	var dueItems []scheduledItem
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
		dueItems = append(dueItems, scheduledItem{item: item, def: def})
	}
	summaries, runErr := runBounded(s.cfg.Watchlist.Scheduler.MaxConcurrentRunsOrDefault(), dueItems, func(due scheduledItem) (RunSummary, error) {
		return s.runScheduledTask(ctx, due.item, due.def)
	})
	if firstErr == nil {
		firstErr = runErr
	}
	if err := s.store.DisableMissingTaskDefinitions(ctx, "watchlist.collect", activeTaskIDs, now); err != nil && firstErr == nil {
		firstErr = err
	}
	return summaries, firstErr
}

// runBounded runs Watchlist items with a fixed worker bound while returning
// summaries in input order. A bound of one preserves the previous behavior.
func runBounded[T any](limit int, items []T, run func(T) (RunSummary, error)) ([]RunSummary, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > len(items) {
		limit = len(items)
	}
	type result struct {
		summary RunSummary
		err     error
	}
	results := make([]result, len(items))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range limit {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index].summary, results[index].err = run(items[index])
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	summaries := make([]RunSummary, len(items))
	var firstErr error
	for index, result := range results {
		summaries[index] = result.summary
		if firstErr == nil && result.err != nil {
			firstErr = result.err
		}
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
	contentActivity := retried
	feeds, err := s.feeds.ListLatest(ctx, item)
	if err != nil {
		status = "failed"
		errText = err.Error()
		sum.Errors = append(sum.Errors, err.Error())
		if isLoginRequiredError(err) {
			notified, notifyErr := s.notifyLoginRequired(ctx, item, time.Now())
			if notifyErr != nil {
				sum.Errors = append(sum.Errors, notifyErr.Error())
			} else if notified {
				sum.Notified++
			}
		}
		return sum, err
	}
	limit := s.cfg.Watchlist.MaxItemsPerRunOrDefault()
	if limit > 0 && len(feeds) > limit {
		feeds = feeds[:limit]
	}
	sum.Discovered = len(feeds)
	var pending []pendingFeed
	seenContentKeys := make(map[string]struct{}, len(feeds))
	for _, feed := range feeds {
		key := watchlist.ContentKey(feed)
		if _, seen := seenContentKeys[key]; seen {
			continue
		}
		isNew, err := s.store.UpsertDiscovered(ctx, item.ID, feed, baseline)
		if err != nil {
			sum.Errors = append(sum.Errors, err.Error())
			continue
		}
		seenContentKeys[key] = struct{}{}
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
		pending = append(pending, pendingFeed{feed: feed, key: key})
	}
	results := runPendingFeeds(s.cfg.Watchlist.Collect.MaxConcurrentItemsOrDefault(), pending, func(pending pendingFeed) itemResult {
		return s.processPendingFeed(ctx, item, pending.feed, pending.key)
	})
	for _, result := range results {
		sum.Collected += result.collected
		sum.Summarized += result.summarized
		sum.Notified += result.notified
		contentActivity += result.activity
		sum.Errors = append(sum.Errors, result.errors...)
	}
	if !baseline && sum.NewItems == 0 {
		if alreadyNotified, err := s.store.HasNotifiedContentOnDate(ctx, item.ID, time.Now()); err != nil {
			sum.Errors = append(sum.Errors, err.Error())
		} else if alreadyNotified {
			sum.NoUpdateSuppressed = true
			sum.NoUpdateSuppressedReason = "source already notified real content today; no-update notification suppressed"
		} else if alreadySent, err := s.store.HasNotifiedDelivery(ctx, item.ID, noUpdateDeliveryKey(time.Now())); err != nil {
			sum.Errors = append(sum.Errors, err.Error())
		} else if alreadySent {
			sum.NoUpdateSuppressed = true
			sum.NoUpdateSuppressedReason = "source already sent a no-update notification today"
		} else if shouldNotifyNoUpdateForRun(item.Options, sum.Discovered, sum.Errors, contentActivity) {
			notified, err := s.notifyNoUpdate(ctx, item, time.Now())
			if err != nil {
				sum.Errors = append(sum.Errors, err.Error())
			} else if notified {
				sum.Notified++
			}
		} else if reason := noUpdateSuppressedReasonForRun(item.Options, sum.Discovered, sum.Errors, contentActivity); reason != "" {
			sum.NoUpdateSuppressed = true
			sum.NoUpdateSuppressedReason = reason
		}
	}
	if len(sum.Errors) > 0 {
		status = "partial"
		errText = strings.Join(sum.Errors, "; ")
	}
	return sum, nil
}

func runPendingFeeds[T any](limit int, feeds []T, run func(T) itemResult) []itemResult {
	if len(feeds) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > len(feeds) {
		limit = len(feeds)
	}
	results := make([]itemResult, len(feeds))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range limit {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = run(feeds[index])
			}
		}()
	}
	for index := range feeds {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return results
}

func (s *Service) processPendingFeed(ctx context.Context, item WatchItem, feed FeedItem, key string) itemResult {
	result := itemResult{}
	collected, err := s.collectFeedItem(ctx, feed)
	if err != nil {
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusCollectFailed, err.Error(), "")
		result.errors = append(result.errors, err.Error())
		return result
	}
	result.collected++
	result.activity++
	if err := s.store.UpdateItemCollectedMetadata(ctx, item.ID, key, collected.Title, collected.PublishedAt, collected.ContentType); err != nil {
		result.errors = append(result.errors, err.Error())
	}
	_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusCollected, "", "")
	req := summary.Request{WatchID: item.ID, Platform: item.Platform, SourceName: item.Name, RunTime: time.Now().Format(time.RFC3339), Items: []summary.Item{{
		ItemID: key, URL: feed.URL, Title: firstNonEmpty(collected.Title, feed.Title),
		PublishedAt: firstNonEmpty(collected.PublishedAt, feedPublishedAtText(feed.PublishedAt)),
		Text:        collected.Text, ASRText: collected.ASRText, OCRText: collected.OCRText, Metadata: collected.Metadata,
	}}}
	res, err := s.summarizer.Summarize(ctx, req)
	if err != nil {
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusSummaryFailed, "", err.Error())
		result.errors = append(result.errors, err.Error())
		return result
	}
	if err := validateSummaryResultForCollectedItem(collected, res); err != nil {
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusSummaryFailed, "", err.Error())
		result.errors = append(result.errors, err.Error())
		return result
	}
	result.summarized++
	_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusSummarized, "", res.Error)
	msg := watchlist.RenderNotification(res)
	notifyResult := s.notifier.Send(ctx, item.NotifyTo, msg)
	if notifyResult.Status == "notified" || notifyResult.Status == StatusNotified {
		result.notified++
		_ = s.store.UpdateItemStatus(ctx, item.ID, key, StatusNotified, "", "")
		_ = s.store.UpsertDeliveryWithProvider(ctx, item.ID, key, notifyResult.ChannelID, StatusNotified, "", false, msg.Title, msg.Text, res.Provider)
		return result
	}
	status := StatusNotifyFailed
	if !notifyResult.Retryable {
		status = StatusNotifyFinalFail
	}
	_ = s.store.UpdateItemStatus(ctx, item.ID, key, status, "", "")
	_ = s.store.UpsertDeliveryWithProvider(ctx, item.ID, key, notifyResult.ChannelID, StatusNotifyFailed, notifyResult.Error, notifyResult.Retryable, msg.Title, msg.Text, res.Provider)
	result.errors = append(result.errors, notifyResult.Error)
	return result
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
			"watch_id":                    sum.WatchID,
			"watch_run_id":                sum.RunID,
			"discovered":                  sum.Discovered,
			"new_items":                   sum.NewItems,
			"collected":                   sum.Collected,
			"summarized":                  sum.Summarized,
			"notified":                    sum.Notified,
			"skipped_locked":              sum.SkippedLocked,
			"no_update_suppressed":        sum.NoUpdateSuppressed,
			"no_update_suppressed_reason": sum.NoUpdateSuppressedReason,
			"errors":                      sum.Errors,
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
		if stale, reason := isStaleVideoPendingDelivery(d); stale {
			_ = s.store.UpsertDeliveryWithProvider(ctx, d.WatchID, d.ContentKey, d.ChannelID, StatusNotifyFailed, reason, false, d.Title, d.Text, d.MessageProvider)
			_ = s.store.UpdateItemStatus(ctx, d.WatchID, d.ContentKey, StatusSummaryFailed, "", reason)
			continue
		}
		res := s.notifier.Send(ctx, d.ChannelID, notify.Message{Title: d.Title, Text: d.Text})
		if res.Status == StatusNotified || res.Status == "notified" {
			notified++
			_ = s.store.UpdateItemStatus(ctx, d.WatchID, d.ContentKey, StatusNotified, "", "")
			_ = s.store.UpsertDeliveryWithProvider(ctx, d.WatchID, d.ContentKey, d.ChannelID, StatusNotified, "", false, d.Title, d.Text, d.MessageProvider)
			continue
		}
		status := StatusNotifyFailed
		if !res.Retryable {
			status = StatusNotifyFinalFail
		}
		_ = s.store.UpdateItemStatus(ctx, d.WatchID, d.ContentKey, status, "", "")
		_ = s.store.UpsertDeliveryWithProvider(ctx, d.WatchID, d.ContentKey, d.ChannelID, StatusNotifyFailed, res.Error, res.Retryable, d.Title, d.Text, d.MessageProvider)
	}
	return notified, nil
}

func (s *Service) notifyNoUpdate(ctx context.Context, item WatchItem, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now()
	}
	key := noUpdateDeliveryKey(now)
	alreadyRecorded, err := s.store.HasDelivery(ctx, item.ID, key)
	if err != nil {
		return false, err
	}
	if alreadyRecorded {
		return false, nil
	}
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = strings.TrimSpace(item.ID)
	}
	date := now.In(time.Local).Format("2006-01-02")
	title := fmt.Sprintf("%s 今日无更新", name)
	text := fmt.Sprintf("%s\n\n%s 今日无更新。", title, date)
	res := s.notifier.Send(ctx, item.NotifyTo, notify.Message{Title: title, Text: text})
	if res.Status == "notified" || res.Status == StatusNotified {
		if err := s.store.UpsertDelivery(ctx, item.ID, key, res.ChannelID, StatusNotified, "", false, title, text); err != nil {
			return false, err
		}
		return true, nil
	}
	status := StatusNotifyFailed
	if !res.Retryable {
		status = StatusNotifyFinalFail
	}
	if err := s.store.UpsertDelivery(ctx, item.ID, key, res.ChannelID, status, res.Error, res.Retryable, title, text); err != nil {
		return false, err
	}
	if strings.TrimSpace(res.Error) == "" {
		return false, fmt.Errorf("notify no-update message failed")
	}
	return false, fmt.Errorf("notify no-update message failed: %s", res.Error)
}

func (s *Service) notifyLoginRequired(ctx context.Context, item WatchItem, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now()
	}
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	if platform == "" {
		platform = "目标平台"
	}
	key := loginRequiredDeliveryKey(platform, now)
	alreadyRecorded, err := s.store.HasDelivery(ctx, item.ID, key)
	if err != nil {
		return false, err
	}
	if alreadyRecorded {
		return false, nil
	}
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = strings.TrimSpace(item.ID)
	}
	title := fmt.Sprintf("%s 需要重新登录", name)
	text := fmt.Sprintf("%s\n\n来源「%s」的登录状态已失效，无法判断是否有更新。请在本机终端运行：\n\ndigcrawl browser login --platform %s\n\n完成登录后，Watchlist 会在下次轮询继续抓取。", title, name, platform)
	res := s.notifier.Send(ctx, item.NotifyTo, notify.Message{Title: title, Text: text})
	if res.Status == "notified" || res.Status == StatusNotified {
		if err := s.store.UpsertDelivery(ctx, item.ID, key, res.ChannelID, StatusNotified, "", false, title, text); err != nil {
			return false, err
		}
		return true, nil
	}
	status := StatusNotifyFailed
	if !res.Retryable {
		status = StatusNotifyFinalFail
	}
	if err := s.store.UpsertDelivery(ctx, item.ID, key, res.ChannelID, status, res.Error, res.Retryable, title, text); err != nil {
		return false, err
	}
	if strings.TrimSpace(res.Error) == "" {
		return false, fmt.Errorf("notify login-required message failed")
	}
	return false, fmt.Errorf("notify login-required message failed: %s", res.Error)
}

func noUpdateDeliveryKey(now time.Time) string {
	return "__no_update__:" + now.In(time.Local).Format("2006-01-02")
}

func loginRequiredDeliveryKey(platform string, now time.Time) string {
	return "__login_required__:" + strings.ToLower(strings.TrimSpace(platform)) + ":" + now.In(time.Local).Format("2006-01-02")
}

func isLoginRequiredError(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "LOGIN_REQUIRED")
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
	scheduleMode := strings.ToLower(strings.TrimSpace(optionString(raw.Options, "_task_schedule_mode")))
	if scheduleMode == "" {
		scheduleMode = "interval"
	}
	scheduleAt := optionString(raw.Options, "_task_schedule_at")
	return WatchItem{
		ID: strings.TrimSpace(raw.ID), Platform: strings.ToLower(strings.TrimSpace(raw.Platform)),
		Type: strings.ToLower(strings.TrimSpace(raw.Type)), URL: strings.TrimSpace(raw.URL),
		Name: strings.TrimSpace(raw.Name), Interval: d, ScheduleMode: scheduleMode, ScheduleAt: scheduleAt,
		NotifyTo: strings.TrimSpace(raw.NotifyTo), Enabled: raw.EnabledOrDefault(), Options: raw.Options,
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
		options := cloneOptions(src.Options)
		mode := strings.ToLower(strings.TrimSpace(src.ScheduleMode))
		if mode == "" {
			mode = "interval"
		}
		options["_task_schedule_mode"] = mode
		if strings.TrimSpace(src.ScheduleAt) != "" {
			options["_task_schedule_at"] = strings.TrimSpace(src.ScheduleAt)
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
			Options:  options,
		})
	}
	return items, nil
}

func cloneOptions(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+2)
	for k, v := range in {
		out[k] = v
	}
	return out
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
	case "", "interval", "daily":
	default:
		options["_task_schedule_error"] = fmt.Sprintf("unsupported mode %q; supported modes: interval, daily", task.Schedule.Mode)
	}
	if mode == "" {
		mode = "interval"
	}
	options["_task_schedule_mode"] = mode
	if strings.TrimSpace(task.Schedule.At) != "" {
		options["_task_schedule_at"] = strings.TrimSpace(task.Schedule.At)
	}
	enabled := task.EnabledOrDefault()
	interval := strings.TrimSpace(task.Schedule.Every)
	if mode == "daily" && interval == "" {
		interval = "24h"
	}
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
	result, err := s.collector.CollectURLWithOptions(ctx, feed.URL, []string{"md", "json"}, s.cfg.Collect.OutputDir, collect.CollectURLOptions{
		UseCookieDomain: feedCookieDomain(feed),
	})
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
	asrText := extractMetadataText(out, "asr_text")
	text, err := summaryTextForCollectedItem(feed, out, asrText, result.Downloaded)
	if err != nil {
		return CollectedItem{Feed: feed}, err
	}
	return CollectedItem{
		Feed: feed, Title: str(out["title"]), PublishedAt: str(out["published_at"]),
		ContentType: str(out["content_type"]), Text: text, ASRText: asrText,
		OCRText: extractOCRText(out), Metadata: extractMetadata(out),
	}, nil
}

func feedCookieDomain(feed FeedItem) string {
	if feed.Raw == nil {
		return ""
	}
	return strings.TrimSpace(str(feed.Raw["use_cookie_domain"]))
}

func summaryTextForCollectedItem(feed FeedItem, out map[string]any, asrText string, downloaded []map[string]any) (string, error) {
	if isVideoCollectedItem(feed, out) {
		if strings.TrimSpace(asrText) == "" {
			return "", videoTranscriptUnavailableError(out, downloaded)
		}
		return "", nil
	}
	return firstNonEmpty(str(out["text"]), str(out["markdown"])), nil
}

func videoTranscriptUnavailableError(out map[string]any, downloaded []map[string]any) error {
	reason := firstNonEmpty(extractMetadataText(out, "asr_error"), videoAssetFailureReason(downloaded))
	if reason == "" {
		reason = "no downloaded video/audio asset was available for ASR"
	}
	return fmt.Errorf("video transcript unavailable: %s", reason)
}

func videoAssetFailureReason(downloaded []map[string]any) string {
	var reasons []string
	sawVideoOrAudio := false
	for _, item := range downloaded {
		typ := strings.ToLower(strings.TrimSpace(str(item["type"])))
		if typ != "video" && typ != "audio" {
			continue
		}
		sawVideoOrAudio = true
		if errText := str(item["error"]); errText != "" {
			reasons = append(reasons, fmt.Sprintf("%s download failed: %s", typ, errText))
		}
	}
	if len(reasons) > 0 {
		return strings.Join(reasons, "; ")
	}
	if sawVideoOrAudio {
		return "downloaded video/audio asset did not produce ASR text"
	}
	return ""
}

func validateSummaryResultForCollectedItem(collected CollectedItem, res *summary.Result) error {
	if !isVideoFeedItem(collected.Feed) {
		return nil
	}
	if res == nil {
		return fmt.Errorf("video summary requires agent provider; got empty result")
	}
	provider := strings.ToLower(strings.TrimSpace(res.Provider))
	if provider == "" || provider == "extractive" {
		if strings.TrimSpace(res.Error) != "" {
			return fmt.Errorf("video summary requires agent provider; got %q: %s", provider, strings.TrimSpace(res.Error))
		}
		return fmt.Errorf("video summary requires agent provider; got %q", provider)
	}
	if len(res.Items) == 0 {
		return fmt.Errorf("video summary agent returned no item summary")
	}
	itemSummary := strings.TrimSpace(res.Items[0].Summary)
	if itemSummary == "" {
		return fmt.Errorf("video summary agent returned empty item summary")
	}
	if utf8.RuneCountInString(itemSummary) > 240 {
		return fmt.Errorf("video summary agent returned overlong item summary")
	}
	if compactForCompare(itemSummary) == compactForCompare(collected.ASRText) {
		return fmt.Errorf("video summary agent copied ASR transcript verbatim")
	}
	return nil
}

func feedPublishedAtText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func isVideoFeedItem(feed FeedItem) bool {
	return isVideoContentType(feed.ContentType)
}

func isVideoContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "video", "toutiao_video", "douyin_video":
		return true
	default:
		return false
	}
}

func isAgentSummaryProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "digflow_runtime", "digflow_http", "llm_direct":
		return true
	default:
		return false
	}
}

func isStaleVideoPendingDelivery(d watchlist.PendingDelivery) (bool, string) {
	if !isVideoContentType(d.ContentType) {
		return false, ""
	}
	if isAgentSummaryProvider(d.MessageProvider) {
		return false, ""
	}
	return true, fmt.Sprintf("stale video delivery requires regenerated agent summary; message_provider=%q", strings.TrimSpace(d.MessageProvider))
}

func isVideoCollectedItem(feed FeedItem, out map[string]any) bool {
	for _, v := range []string{feed.ContentType, str(out["content_type"])} {
		if isVideoContentType(v) {
			return true
		}
	}
	return false
}

func compactForCompare(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), "")
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

func shouldNotifyNoUpdate(opts map[string]any, discovered int) bool {
	if !notifyNoUpdateEnabled(opts) {
		return false
	}
	return noUpdateDiscoveryComplete(opts, discovered)
}

func shouldNotifyNoUpdateForRun(opts map[string]any, discovered int, errors []string, contentActivity int) bool {
	if !shouldNotifyNoUpdate(opts, discovered) {
		return false
	}
	if contentActivity > 0 {
		return false
	}
	return noUpdateErrorSuppressedReason(opts, errors) == ""
}

func noUpdateSuppressedReason(opts map[string]any, discovered int) string {
	if !notifyNoUpdateEnabled(opts) {
		return ""
	}
	minDiscovered := noUpdateMinDiscovered(opts)
	if minDiscovered > 0 && discovered < minDiscovered {
		return fmt.Sprintf("discovery incomplete: discovered %d below no-update threshold %d", discovered, minDiscovered)
	}
	return ""
}

func noUpdateSuppressedReasonForRun(opts map[string]any, discovered int, errors []string, contentActivity int) string {
	if !notifyNoUpdateEnabled(opts) {
		return ""
	}
	if contentActivity > 0 {
		return fmt.Sprintf("run has %d content activity event(s); no-update notification suppressed", contentActivity)
	}
	if reason := noUpdateErrorSuppressedReason(opts, errors); reason != "" {
		return reason
	}
	return noUpdateSuppressedReason(opts, discovered)
}

func noUpdateErrorSuppressedReason(opts map[string]any, errors []string) string {
	if !notifyNoUpdateEnabled(opts) || len(errors) == 0 {
		return ""
	}
	return fmt.Sprintf("run has %d error(s); no-update notification suppressed", len(errors))
}

func notifyNoUpdateEnabled(opts map[string]any) bool {
	for _, key := range []string{"notify_no_update", "NotifyNoUpdate", "notifyOnNoUpdate", "notify_on_no_update"} {
		if optionBool(opts, key) {
			return true
		}
	}
	return false
}

func noUpdateDiscoveryComplete(opts map[string]any, discovered int) bool {
	minDiscovered := noUpdateMinDiscovered(opts)
	return minDiscovered <= 0 || discovered >= minDiscovered
}

func noUpdateMinDiscovered(opts map[string]any) int {
	minDiscovered := firstPositiveOptionInt(opts,
		"min_discovered_for_no_update",
		"no_update_min_discovered",
		"noUpdateMinDiscovered",
	)
	if minDiscovered <= 0 {
		minDiscovered = firstPositiveOptionInt(opts, "limit", "Limit")
	}
	return minDiscovered
}

func firstPositiveOptionInt(opts map[string]any, keys ...string) int {
	for _, key := range keys {
		if n := optionInt(opts, key); n > 0 {
			return n
		}
	}
	return 0
}

func optionInt(opts map[string]any, key string) int {
	if opts == nil {
		return 0
	}
	v, ok := opts[key]
	if !ok {
		return 0
	}
	switch value := v.(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case uint:
		return int(value)
	case uint8:
		return int(value)
	case uint16:
		return int(value)
	case uint32:
		return int(value)
	case uint64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return n
	}
}

func optionBool(opts map[string]any, key string) bool {
	if opts == nil {
		return false
	}
	v, ok := opts[key]
	if !ok {
		return false
	}
	switch value := v.(type) {
	case bool:
		return value
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(value))
		return b
	default:
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
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
