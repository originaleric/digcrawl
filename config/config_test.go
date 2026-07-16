package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectPersistModeHelpers(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false

	cases := []struct {
		mode       string
		indexEn    *bool
		wantIndex  bool
		wantSQLite bool
	}{
		{"", &enabled, true, false},
		{"files", &enabled, false, false},
		{"index", &enabled, true, false},
		{"sqlite", &enabled, false, true},
		{"both", &enabled, true, true},
		{"index", &disabled, false, false},
		{"both", &disabled, false, true},
	}
	for _, tc := range cases {
		c := CollectConfig{PersistMode: tc.mode, IndexEnabled: tc.indexEn}
		if c.PersistModeOrDefault() == "index" && tc.mode == "" {
			// ok
		}
		if c.UseIndexJSON() != tc.wantIndex {
			t.Fatalf("mode=%q indexEn=%v UseIndexJSON=%v want %v", tc.mode, tc.indexEn, c.UseIndexJSON(), tc.wantIndex)
		}
		if c.UseSQLite() != tc.wantSQLite {
			t.Fatalf("mode=%q UseSQLite=%v want %v", tc.mode, c.UseSQLite(), tc.wantSQLite)
		}
	}
	if got := (CollectConfig{}).DBPathOrDefault(); got != "storage/digcrawl/collect.db" {
		t.Fatalf("DBPathOrDefault=%q", got)
	}
}

func TestWatchlistPersistenceDefaults(t *testing.T) {
	t.Parallel()

	empty := WatchlistConfig{}
	if got := empty.PersistenceDriverOrDefault(); got != "sqlite" {
		t.Fatalf("PersistenceDriverOrDefault=%q", got)
	}
	if got := empty.StateDBPathOrDefault("storage/custom/collect.db"); got != "storage/custom/collect.db" {
		t.Fatalf("StateDBPathOrDefault collect fallback=%q", got)
	}
	if got := (WatchlistConfig{DBPath: "legacy.db"}).StateDBPathOrDefault("collect.db"); got != "legacy.db" {
		t.Fatalf("StateDBPathOrDefault legacy=%q", got)
	}
	cfg := WatchlistConfig{
		Persistence: WatchlistPersistenceConfig{Driver: "SQLite", SQLitePath: "watchlist.db"},
		DBPath:      "legacy.db",
	}
	if got := cfg.PersistenceDriverOrDefault(); got != "sqlite" {
		t.Fatalf("normalized driver=%q", got)
	}
	if got := cfg.StateDBPathOrDefault("collect.db"); got != "watchlist.db" {
		t.Fatalf("StateDBPathOrDefault persistence path=%q", got)
	}
}

func TestXAPIConfigHelpers(t *testing.T) {
	t.Parallel()

	if !(XAPIConfig{}).EnabledOrDefault() {
		t.Fatal("zero XAPIConfig should default to enabled")
	}
	if !(XAPIConfig{}).OEmbedEnabledOrDefault() {
		t.Fatal("zero XAPIConfig should default to oEmbed enabled")
	}
	disabled := false
	if (XAPIConfig{APIEnabled: &disabled}).EnabledOrDefault() {
		t.Fatal("explicit disabled API should be disabled")
	}
	if (XAPIConfig{OEmbedEnabled: &disabled}).OEmbedEnabledOrDefault() {
		t.Fatal("explicit disabled oEmbed should be disabled")
	}
	if got := (XAPIConfig{}).AuthModeOrDefault(); got != "app_only" {
		t.Fatalf("AuthModeOrDefault=%q", got)
	}
	if got := (XAPIConfig{BearerToken: "token"}).AuthModeOrDefault(); got != "bearer" {
		t.Fatalf("AuthModeOrDefault with bearer=%q", got)
	}
	if got := (XAPIConfig{}).APIBaseURLOrDefault(); got != "https://api.twitter.com" {
		t.Fatalf("APIBaseURLOrDefault=%q", got)
	}
	if got := (XAPIConfig{}).TokenURLOrDefault(); got != "https://api.twitter.com/oauth2/token" {
		t.Fatalf("TokenURLOrDefault=%q", got)
	}
	if got := (XAPIConfig{}).OEmbedURLOrDefault(); got != "https://publish.twitter.com/oembed" {
		t.Fatalf("OEmbedURLOrDefault=%q", got)
	}
	if got := (XAPIConfig{}).APITimeoutSecOrDefault(); got != 8 {
		t.Fatalf("APITimeoutSecOrDefault=%d", got)
	}
	if got := (XAPIConfig{}).APIMaxRetriesOrDefault(); got != 1 {
		t.Fatalf("APIMaxRetriesOrDefault=%d", got)
	}
}

func TestOCRConfigHelpers(t *testing.T) {
	t.Parallel()

	if (OCRConfig{}).EnabledOrDefault() {
		t.Fatal("zero OCRConfig should default to disabled")
	}
	enabled := true
	if !(OCRConfig{Enabled: &enabled}).EnabledOrDefault() {
		t.Fatal("explicit enabled OCR should be enabled")
	}
	if got := (OCRConfig{}).ProviderOrDefault(); got != "openai_compatible" {
		t.Fatalf("ProviderOrDefault=%q", got)
	}
	if got := (OCRConfig{Provider: "stub"}).ProviderOrDefault(); got != "stub" {
		t.Fatalf("ProviderOrDefault stub=%q", got)
	}
	if got := (OCRConfig{Provider: "zhipu_ocr"}).ProviderOrDefault(); got != "zhipu_ocr" {
		t.Fatalf("ProviderOrDefault zhipu_ocr=%q", got)
	}
	if got := (OCRConfig{Provider: "glm-ocr"}).ProviderOrDefault(); got != "zhipu_ocr" {
		t.Fatalf("ProviderOrDefault glm-ocr=%q", got)
	}
	if got := (OCRConfig{}).BaseURLOrDefault(); got != "https://api.openai.com/v1" {
		t.Fatalf("BaseURLOrDefault=%q", got)
	}
	if got := (OCRConfig{}).ModelOrDefault(); got != "gpt-4o-mini" {
		t.Fatalf("ModelOrDefault=%q", got)
	}
	if got := (OCRConfig{}).TimeoutSecOrDefault(); got != 30 {
		t.Fatalf("TimeoutSecOrDefault=%d", got)
	}
	if got := (OCRConfig{}).MaxImagesOrDefault(); got != 8 {
		t.Fatalf("MaxImagesOrDefault=%d", got)
	}
	if got := (OCRConfig{}).MaxImageBytesOrDefault(); got != 5*1024*1024 {
		t.Fatalf("MaxImageBytesOrDefault=%d", got)
	}
	if !(OCRConfig{}).AppendToMarkdownOrDefault() {
		t.Fatal("AppendToMarkdown should default to true")
	}
	if (OCRConfig{}).AppendToTextOrDefault() {
		t.Fatal("AppendToText should default to false")
	}
}

func TestASRConfigProviderAwareModelDefault(t *testing.T) {
	t.Parallel()

	if got := (ASRConfig{}).ProviderOrDefault(); got != "api" {
		t.Fatalf("ProviderOrDefault=%q", got)
	}
	if got := (ASRConfig{}).ModelOrDefault(); got != "whisper-1" {
		t.Fatalf("api ModelOrDefault=%q", got)
	}
	if got := (ASRConfig{Provider: "local"}).ModelOrDefault(); got != "small" {
		t.Fatalf("local ModelOrDefault=%q", got)
	}
	if got := (ASRConfig{}).ResponseFormatOrDefault(); got != "verbose_json" {
		t.Fatalf("ResponseFormatOrDefault=%q", got)
	}
	if got := (ASRConfig{ResponseFormat: "TEXT"}).ResponseFormatOrDefault(); got != "text" {
		t.Fatalf("normalized ResponseFormatOrDefault=%q", got)
	}
	if !SupportedASRResponseFormat("vtt") {
		t.Fatal("expected vtt response format to be supported")
	}
	if SupportedASRResponseFormat("xml") {
		t.Fatal("unexpected xml response format support")
	}

	cfg := Config{
		ASR: ASRConfig{
			Provider: "local",
		},
	}
	mergeDefaults(&cfg)
	if cfg.ASR.Model != "" {
		t.Fatalf("mergeDefaults should not materialize provider-specific ASR model, got %q", cfg.ASR.Model)
	}
	if got := cfg.ASR.ModelOrDefault(); got != "small" {
		t.Fatalf("merged local ModelOrDefault=%q", got)
	}
}

func TestLoadParsesScheduledTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
Watchlist:
  Enabled: true
  Persistence:
    Driver: sqlite
    SQLitePath: storage/digcrawl/watchlist-test.db
  Scheduler:
    TickInterval: 2m
    Jitter: 10s
Tasks:
  - ID: reddit-ai-watch
    Name: Reddit AI
    Type: watchlist.collect
    Schedule:
      Mode: interval
      Every: 6h
    Source:
      Platform: reddit
      Kind: reddit_subreddit
      URL: https://www.reddit.com/r/artificial/new/
      Options:
        sort: new
        limit: 25
    Notify:
      Channel: feishu-default
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Watchlist.Scheduler.TickIntervalOrDefault(); got != "2m" {
		t.Fatalf("TickInterval=%q", got)
	}
	if got := cfg.Watchlist.Scheduler.JitterOrDefault(); got != "10s" {
		t.Fatalf("Jitter=%q", got)
	}
	if got := cfg.Watchlist.PersistenceDriverOrDefault(); got != "sqlite" {
		t.Fatalf("Watchlist persistence driver=%q", got)
	}
	if got := cfg.Watchlist.StateDBPathOrDefault(cfg.Collect.DBPathOrDefault()); got != "storage/digcrawl/watchlist-test.db" {
		t.Fatalf("Watchlist state db=%q", got)
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("tasks len=%d", len(cfg.Tasks))
	}
	task := cfg.Tasks[0]
	if task.ID != "reddit-ai-watch" || task.Schedule.Every != "6h" || task.Source.Options["sort"] != "new" {
		t.Fatalf("task not parsed: %+v", task)
	}
}
