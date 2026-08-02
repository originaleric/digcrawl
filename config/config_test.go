package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestSocialConfigDefaultsArePublic(t *testing.T) {
	t.Parallel()

	threads := SocialPlatformConfig{}
	if got := threads.AuthModeOrDefault(); got != "public" {
		t.Fatalf("threads auth=%q", got)
	}
	if threads.BrowserFallbackOrDefault() {
		t.Fatal("threads browser fallback should default to false")
	}
	if threads.AllowsLocalSession() {
		t.Fatal("threads should not allow local session by default")
	}
	enabled := true
	instagram := SocialPlatformConfig{AuthMode: "local_session", BrowserFallback: &enabled}
	if got := instagram.AuthModeOrDefault(); got != "local_session" {
		t.Fatalf("instagram auth=%q", got)
	}
	if !instagram.BrowserFallbackOrDefault() || !instagram.AllowsLocalSession() {
		t.Fatal("explicit instagram local session should be allowed")
	}
}

func TestDefaultAllowedToolsIncludeSocialAndDomesticProviders(t *testing.T) {
	t.Parallel()

	cfg := Get()
	for _, tool := range []string{
		"feed.read",
		"threads.post.read",
		"instagram.content.read",
		"bilibili.video.read",
		"weibo.post.read",
		"kuaishou.video.read",
		"zhihu.content.read",
	} {
		if !stringSliceContains(cfg.Gateway.AllowedTools, tool) {
			t.Fatalf("gateway allowed tools missing %s", tool)
		}
		if !stringSliceContains(cfg.Collector.AllowedTools, tool) {
			t.Fatalf("collector allowed tools missing %s", tool)
		}
	}
	for _, domain := range []string{
		"threads.net",
		"www.threads.net",
		"threads.com",
		"www.threads.com",
		"instagram.com",
		"www.instagram.com",
		"bilibili.com",
		"www.bilibili.com",
		"m.bilibili.com",
		"space.bilibili.com",
		"b23.tv",
		"weibo.com",
		"www.weibo.com",
		"m.weibo.cn",
		"kuaishou.com",
		"www.kuaishou.com",
		"v.kuaishou.com",
		"zhihu.com",
		"www.zhihu.com",
		"zhuanlan.zhihu.com",
	} {
		if !stringSliceContains(cfg.LocalBrowser.AllowedDomains, domain) {
			t.Fatalf("local browser allowed domains missing %s", domain)
		}
	}
}

func TestDigCrawlSkillExampleUsesReadOnlyToolProfile(t *testing.T) {
	data, err := os.ReadFile("digcrawl-skill.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"gateway.ping", "browser.browse", "wechat.article.read", "feed.read"} {
		if !stringSliceContains(cfg.Gateway.AllowedTools, tool) {
			t.Fatalf("skill profile missing %s", tool)
		}
	}
	for _, tool := range []string{"browser.action", "browser.snapshot", "asr.transcribe", "file.read"} {
		if stringSliceContains(cfg.Gateway.AllowedTools, tool) {
			t.Fatalf("skill profile must not expose %s", tool)
		}
	}
	if cfg.Gateway.ArtifactEnabled == nil || !*cfg.Gateway.ArtifactEnabled {
		t.Fatal("skill profile should persist requested artifacts")
	}
	if cfg.Gateway.ArtifactDir == "" {
		t.Fatal("skill profile should declare an artifact directory")
	}
	if !filepath.IsAbs(cfg.Gateway.ArtifactDir) {
		t.Fatal("skill profile artifact directory should be absolute")
	}
	if cfg.Gateway.ArtifactMaxInlineBytes != 10*1024*1024 {
		t.Fatalf("skill profile artifact inline limit=%d", cfg.Gateway.ArtifactMaxInlineBytes)
	}
	if cfg.Providers.ASREnabled == nil || *cfg.Providers.ASREnabled {
		t.Fatal("skill profile should not enable ASR")
	}
	if len(cfg.Gateway.AllowedDomains) != 0 {
		t.Fatal("skill profile should leave public web access open by default")
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func TestASRConfigFFmpegPathExpandsEnv(t *testing.T) {
	t.Setenv("DIGCRAWL_FFMPEG_PATH", "/opt/homebrew/bin/ffmpeg")
	cfg := ASRConfig{FFmpegPath: "${DIGCRAWL_FFMPEG_PATH}"}
	if got := cfg.FFmpegPathOrDefault(); got != "/opt/homebrew/bin/ffmpeg" {
		t.Fatalf("FFmpegPathOrDefault=%q", got)
	}
	t.Setenv("DIGCRAWL_FFMPEG_PATH", "")
	if got := cfg.FFmpegPathOrDefault(); got != "ffmpeg" {
		t.Fatalf("empty expanded FFmpegPath should fall back to command name, got %q", got)
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
      Mode: daily
      At: "09:00"
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
	if task.ID != "reddit-ai-watch" || task.Schedule.Mode != "daily" || task.Schedule.At != "09:00" || task.Source.Options["sort"] != "new" {
		t.Fatalf("task not parsed: %+v", task)
	}
}

func TestLoadMergesLocalFeishuConfig(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "feishu.yml")
	data := []byte(`
Notifications:
  Feishu:
    Enabled: true
    DefaultChannel: feishu-default
    ConfigPath: "` + localPath + `"
    Channels:
      - ID: feishu-default
        WebhookURL: "${FEISHU_WEBHOOK_URL}"
`)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	local := []byte(`
Feishu:
  AppID: local-app
  AppSecret: local-secret
  Channels:
    - ID: feishu-default
      Mode: app
      ReceiveIDType: chat_id
      ReceiveID: oc_local
`)
	if err := os.WriteFile(localPath, local, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	feishu := cfg.Notifications.Feishu
	if feishu.AppID != "local-app" || feishu.AppSecret != "local-secret" {
		t.Fatalf("local app credentials not merged: %+v", feishu)
	}
	if len(feishu.Channels) != 1 || feishu.Channels[0].Mode != "app" || feishu.Channels[0].ReceiveID != "oc_local" {
		t.Fatalf("local channels not merged: %+v", feishu.Channels)
	}
}

func TestLoadAppliesRuntimeEnvWhenEnabled(t *testing.T) {
	t.Setenv("DIGCRAWL_RUNTIME_ENV_TEST", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
RuntimeEnv:
  Enabled: true
  Variables:
    DIGCRAWL_RUNTIME_ENV_TEST: from-config
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DIGCRAWL_RUNTIME_ENV_TEST"); got != "from-config" {
		t.Fatalf("runtime env not applied: %q", got)
	}
}

func TestLoadDoesNotApplyRuntimeEnvUnlessEnabled(t *testing.T) {
	t.Setenv("DIGCRAWL_RUNTIME_ENV_DISABLED_TEST", "before")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
RuntimeEnv:
  Enabled: false
  Variables:
    DIGCRAWL_RUNTIME_ENV_DISABLED_TEST: after
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DIGCRAWL_RUNTIME_ENV_DISABLED_TEST"); got != "before" {
		t.Fatalf("disabled runtime env should not override process env: %q", got)
	}
}

func TestLoadRejectsInvalidRuntimeEnvName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
RuntimeEnv:
  Enabled: true
  Variables:
    "BAD-NAME": value
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid runtime env name to fail")
	}
}

func TestLoadMergesLocalRuntimeEnvConfig(t *testing.T) {
	t.Setenv("DIGCRAWL_LOCAL_RUNTIME_ENV_TEST", "")
	dir := t.TempDir()
	localPath := filepath.Join(dir, "runtime-env.yml")
	configPath := filepath.Join(dir, "config.yaml")
	data := []byte(`
RuntimeEnv:
  ConfigPath: "` + localPath + `"
  Variables:
    DIGCRAWL_LOCAL_RUNTIME_ENV_TEST: from-main
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	local := []byte(`
RuntimeEnv:
  Enabled: true
  Variables:
    DIGCRAWL_LOCAL_RUNTIME_ENV_TEST: from-local
`)
	if err := os.WriteFile(localPath, local, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DIGCRAWL_LOCAL_RUNTIME_ENV_TEST"); got != "from-local" {
		t.Fatalf("local runtime env not merged: %q", got)
	}
}
