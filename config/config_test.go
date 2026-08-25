package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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

func TestSkillMediaTimeoutSecOrDefault(t *testing.T) {
	if got := (SkillMediaConfig{}).TimeoutSecOrDefault(); got != DefaultSkillMediaTimeoutSec {
		t.Fatalf("default timeout=%d want %d", got, DefaultSkillMediaTimeoutSec)
	}
	if got := (SkillMediaConfig{TimeoutSec: 45}).TimeoutSecOrDefault(); got != 45 {
		t.Fatalf("configured timeout=%d want 45", got)
	}
}

func TestSkillMediaMaxASRCandidatesOrDefault(t *testing.T) {
	if got := (SkillMediaConfig{}).MaxASRCandidatesOrDefault(); got != DefaultSkillMaxASRCandidates {
		t.Fatalf("default ASR candidates=%d want %d", got, DefaultSkillMaxASRCandidates)
	}
	if got := (SkillMediaConfig{MaxASRCandidates: 3}).MaxASRCandidatesOrDefault(); got != 3 {
		t.Fatalf("configured ASR candidates=%d want 3", got)
	}
}

func TestDefaultConfigPathsUseXDGDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/digcrawl-xdg")
	if got, want := DefaultConfigPath(), filepath.Join("/tmp/digcrawl-xdg", "digcrawl", "config.yaml"); got != want {
		t.Fatalf("DefaultConfigPath=%q want %q", got, want)
	}
	if got, want := DefaultSkillProfilePath(), filepath.Join("/tmp/digcrawl-xdg", "digcrawl", "digcrawl-skill.yaml"); got != want {
		t.Fatalf("DefaultSkillProfilePath=%q want %q", got, want)
	}
}

func TestValidateAnchorsSkillASRCacheToProfilePath(t *testing.T) {
	profileDir := t.TempDir()
	path := filepath.Join(profileDir, "digcrawl-skill.yaml")
	if err := os.WriteFile(path, []byte("Skill:\n  ASR:\n    ModelCacheDir: models\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(profileDir, "models")
	if cfg.Skill.ASR.ModelCacheDir != want {
		t.Fatalf("Skill.ASR.ModelCacheDir=%q want %q", cfg.Skill.ASR.ModelCacheDir, want)
	}
}

func TestValidateUsesProfileLocalDefaultForSkillASRCache(t *testing.T) {
	legacyCWD := t.TempDir()
	t.Chdir(legacyCWD)
	hfHome := filepath.Join(t.TempDir(), "custom-hf-home")
	t.Setenv("HF_HOME", hfHome)
	t.Setenv("HUGGINGFACE_HUB_CACHE", "")
	profileDir := t.TempDir()
	path := filepath.Join(profileDir, "digcrawl-skill.yaml")
	if err := os.WriteFile(path, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(profileDir, "asr_models")
	if cfg.Skill.ASR.ModelCacheDir != want {
		t.Fatalf("default Skill.ASR.ModelCacheDir=%q want %q", cfg.Skill.ASR.ModelCacheDir, want)
	}
	legacy := filepath.Join(legacyCWD, ".cache", "huggingface")
	wantLegacyCandidates := []string{hfHome, legacy}
	if !reflect.DeepEqual(cfg.Skill.ASR.LegacyModelCacheDirs, wantLegacyCandidates) {
		t.Fatalf("default Skill legacy cache candidates=%q want %q", cfg.Skill.ASR.LegacyModelCacheDirs, wantLegacyCandidates)
	}
}

func TestValidateDoesNotInferLegacyCacheForExplicitSkillASRCache(t *testing.T) {
	t.Chdir(t.TempDir())
	profileDir := t.TempDir()
	path := filepath.Join(profileDir, "digcrawl-skill.yaml")
	if err := os.WriteFile(path, []byte("Skill:\n  ASR:\n    ModelCacheDir: models\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skill.ASR.LegacyModelCacheDirs) != 0 {
		t.Fatalf("explicit Skill cache must not receive implicit legacy candidates: %q", cfg.Skill.ASR.LegacyModelCacheDirs)
	}
}

func TestValidatePreservesCustomLegacyHubCacheForOldSkillProfile(t *testing.T) {
	t.Chdir(t.TempDir())
	customHub := filepath.Join(t.TempDir(), "custom-hub-cache")
	t.Setenv("HF_HOME", "")
	t.Setenv("HUGGINGFACE_HUB_CACHE", customHub)
	path := filepath.Join(t.TempDir(), "digcrawl-skill.yaml")
	if err := os.WriteFile(path, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Skill.ASR.LegacyHubCacheDir != customHub {
		t.Fatalf("LegacyHubCacheDir=%q want %q", cfg.Skill.ASR.LegacyHubCacheDir, customHub)
	}
}

func TestValidateAnchorsSkillASRRunnerPathsToProfilePath(t *testing.T) {
	t.Chdir(t.TempDir())
	profileDir := t.TempDir()
	path := filepath.Join(profileDir, "digcrawl-skill.yaml")
	data := []byte("Skill:\n  ASR:\n    LocalCommand: .venv-asr/bin/python\n    LocalArgs:\n      - scripts/faster_whisper_asr.py\n      - --check\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Skill.ASR.LocalCommand, filepath.Join(profileDir, ".venv-asr", "bin", "python"); got != want {
		t.Fatalf("Skill.ASR.LocalCommand=%q want %q", got, want)
	}
	if got, want := cfg.Skill.ASR.LocalArgs[0], filepath.Join(profileDir, "scripts", "faster_whisper_asr.py"); got != want {
		t.Fatalf("Skill.ASR.LocalArgs[0]=%q want %q", got, want)
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
		"youtube.video.read",
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
		"youtube.com",
		"www.youtube.com",
		"m.youtube.com",
		"youtu.be",
		"www.youtu.be",
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
	if err := validateSkillSummaryConfig(data); err != nil {
		t.Fatalf("skill profile contains unsupported local summary configuration: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"gateway.ping", "skill.asr.enable", "browser.browse", "wechat.article.read", "feed.read"} {
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
	if !examplePathIsPortableAbsolute(cfg.Gateway.ArtifactDir) {
		t.Fatal("skill profile artifact directory should be absolute")
	}
	if cfg.Gateway.ArtifactMaxInlineBytes != 10*1024*1024 {
		t.Fatalf("skill profile artifact inline limit=%d", cfg.Gateway.ArtifactMaxInlineBytes)
	}
	if cfg.Providers.ASREnabled == nil || !*cfg.Providers.ASREnabled {
		t.Fatal("skill profile must register the local ASR setting tool")
	}
	if cfg.Skill.Media.DownloadMedia {
		t.Fatal("skill profile media download should require explicit local opt-in")
	}
	if !examplePathIsPortableAbsolute(cfg.Skill.Media.Dir) {
		t.Fatal("skill profile media directory should be absolute")
	}
	if cfg.Skill.OCR.EnabledOrDefault() || cfg.Skill.ASR.EnabledOrDefault() || cfg.Skill.Summary.EnabledOrDefault() {
		t.Fatal("skill enrichments should require explicit local configuration")
	}
	if len(cfg.Gateway.AllowedDomains) != 0 {
		t.Fatal("skill profile should leave public web access open by default")
	}
}

// The distributed example deliberately uses Unix-looking placeholders because
// users must replace them with their own local directories. Accept that
// documented placeholder on Windows while still rejecting relative values.
func examplePathIsPortableAbsolute(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, "/absolute/path/to/")
}

func TestLoadRejectsRemovedSkillSummaryFields(t *testing.T) {
	previous := defaultCfg
	t.Cleanup(func() { defaultCfg = previous })
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "provider",
			yaml: "Skill:\n  Summary:\n    Enabled: true\n    Provider: digflow_runtime\n",
			want: "Skill.Summary.Provider is no longer supported",
		},
		{
			name: "digflow",
			yaml: "Skill:\n  Summary:\n    Enabled: true\n    DigFlow:\n      Agent: legacy\n",
			want: "Skill.Summary.DigFlow is no longer supported",
		},
		{
			name: "unused metadata budget",
			yaml: "Skill:\n  Summary:\n    Enabled: true\n    InputBudget:\n      MetadataChars: 2000\n",
			want: "Skill.Summary.InputBudget.MetadataChars is unsupported",
		},
		{
			name: "disabled legacy fallback remains inert",
			yaml: "Skill:\n  Summary:\n    Enabled: false\n    Provider: digflow_runtime\n",
			want: "",
		},
		{
			name: "current local fallback is accepted",
			yaml: "Skill:\n  Summary:\n    Enabled: true\n    InputBudget:\n      TextChars: 12000\n      ASRChars: 12000\n      OCRChars: 4000\n    LLM:\n      APIKey: test-key\n      BaseURL: https://api.openai.com/v1\n      Model: gpt-4o-mini\n",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "digcrawl-skill.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if tc.want == "" && err != nil {
				t.Fatalf("Load error=%v, expected disabled legacy fallback to remain inert", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Load error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestDigCrawlSkillExampleUsesHostVisionAndCollectASROperationalSettings(t *testing.T) {
	collectData, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	skillData, err := os.ReadFile("digcrawl-skill.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var collectCfg, skillCfg Config
	if err := yaml.Unmarshal(collectData, &collectCfg); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(skillData, &skillCfg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(skillCfg.Skill.OCR, OCRConfig{}) {
		t.Fatalf("Skill profile must not configure local OCR: %+v", skillCfg.Skill.OCR)
	}
	if !reflect.DeepEqual(skillCfg.Skill.Summary, SkillSummaryConfig{}) {
		t.Fatalf("Skill profile must not configure a local summary model: %+v", skillCfg.Skill.Summary)
	}
	wantASR := collectCfg.ASR
	wantASR.Enabled = skillCfg.Skill.ASR.Enabled // Skill remains explicit opt-in.
	// The open-source Skill never places a provider key in its example profile.
	wantASR.APIKey = ""
	if !reflect.DeepEqual(skillCfg.Skill.ASR, wantASR) {
		t.Fatalf("Skill.ASR must copy the Collect ASR operational settings: skill=%+v collect=%+v", skillCfg.Skill.ASR, wantASR)
	}
}

func TestConfigExamplesUsePortableBundledASRDefault(t *testing.T) {
	for _, name := range []string{"config.yaml", "digcrawl-skill.example.yaml"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		asrCfg := cfg.ASR
		if name == "digcrawl-skill.example.yaml" {
			asrCfg = cfg.Skill.ASR
		}
		if asrCfg.LocalCommand != "" || len(asrCfg.LocalArgs) != 0 {
			t.Fatalf("%s must leave LocalCommand/LocalArgs empty for bundled fallback, got command=%q args=%q", name, asrCfg.LocalCommand, asrCfg.LocalArgs)
		}
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
		t.Fatalf("ProviderOrDefault must preserve unsupported provider for validation, got %q", got)
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
	if (VideoFrameOCRConfig{}).EnabledOrDefault() {
		t.Fatal("video frame OCR should default to disabled")
	}
	if got := (VideoFrameOCRConfig{}).IntervalSecOrDefault(); got != 5 {
		t.Fatalf("video frame interval=%d", got)
	}
	if got := (VideoFrameOCRConfig{}).MaxFramesOrDefault(); got != 8 {
		t.Fatalf("video frame max=%d", got)
	}
	if got := (VideoFrameOCRConfig{}).MaxTotalFramesOrDefault(); got != 16 {
		t.Fatalf("video frame total max=%d", got)
	}
	if got := (VideoFrameOCRConfig{}).TimeoutSecOrDefault(); got != 120 {
		t.Fatalf("video frame timeout=%d", got)
	}
	if got := (VideoFrameOCRConfig{}).FFmpegPathOrDefault(); got != "ffmpeg" {
		t.Fatalf("video frame ffmpeg=%q", got)
	}
	if !(VideoFrameOCRConfig{Enabled: &enabled}).EnabledOrDefault() {
		t.Fatal("explicit video frame OCR should be enabled")
	}
}

func TestValidateAppliesVideoFrameOCRDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("OCR:\n  Enabled: true\n  VideoFrames:\n    Enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OCR.VideoFrames.EnabledOrDefault() || cfg.OCR.VideoFrames.IntervalSec != 5 || cfg.OCR.VideoFrames.MaxFrames != 8 || cfg.OCR.VideoFrames.MaxTotalFrames != 16 || cfg.OCR.VideoFrames.TimeoutSec != 120 {
		t.Fatalf("video frame defaults=%+v", cfg.OCR.VideoFrames)
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
	if got := (ASRConfig{}).MaxTimeoutSecOrDefault(); got != 1800 {
		t.Fatalf("default MaxTimeoutSec=%d want 1800", got)
	}
	if got := (ASRConfig{MaxTimeoutSec: 2400}).MaxTimeoutSecOrDefault(); got != 2400 {
		t.Fatalf("configured MaxTimeoutSec=%d want 2400", got)
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
	if got := cfg.ASR.MaxTimeoutSecOrDefault(); got != 1800 {
		t.Fatalf("merged MaxTimeoutSec=%d want 1800", got)
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
    ConfigPath: ` + strconv.Quote(localPath) + `
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

func TestValidateDoesNotApplyRuntimeEnv(t *testing.T) {
	t.Setenv("DIGCRAWL_VALIDATE_RUNTIME_ENV_TEST", "before")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`
RuntimeEnv:
  Enabled: true
  Variables:
    DIGCRAWL_VALIDATE_RUNTIME_ENV_TEST: after
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DIGCRAWL_VALIDATE_RUNTIME_ENV_TEST"); got != "before" {
		t.Fatalf("Validate must not override process env: %q", got)
	}
}

func TestValidateDoesNotReadExternalSecretConfigs(t *testing.T) {
	dir := t.TempDir()
	externalPath := filepath.Join(dir, "external.yaml")
	if err := os.WriteFile(externalPath, []byte("[not valid YAML"), 0o600); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(dir, "profile.yaml")
	data := []byte("RuntimeEnv:\n  ConfigPath: " + strconv.Quote(externalPath) + "\nNotifications:\n  Feishu:\n    ConfigPath: " + strconv.Quote(externalPath) + "\n")
	if err := os.WriteFile(profilePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(profilePath); err != nil {
		t.Fatalf("Validate must not read external config: %v", err)
	}
	if _, err := Load(profilePath); err == nil {
		t.Fatal("Load should retain runtime external-config merging")
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
  ConfigPath: ` + strconv.Quote(localPath) + `
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
