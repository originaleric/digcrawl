package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds DigCrawl runtime settings.
type Config struct {
	// SourcePath records the local profile that produced this in-memory
	// configuration. It is runtime metadata and is never written to YAML.
	SourcePath    string              `yaml:"-" json:"-"`
	Gateway       GatewayConfig       `yaml:"Gateway" json:"Gateway"`
	Collector     CollectorConfig     `yaml:"Collector" json:"Collector"`
	Collect       CollectConfig       `yaml:"Collect" json:"Collect"`
	LocalBrowser  LocalBrowserConfig  `yaml:"LocalBrowser" json:"LocalBrowser"`
	RuntimeEnv    RuntimeEnvConfig    `yaml:"RuntimeEnv" json:"RuntimeEnv,omitempty"`
	Skill         SkillConfig         `yaml:"Skill" json:"Skill,omitempty"`
	Platform      PlatformConfig      `yaml:"Platform" json:"Platform"`
	OCR           OCRConfig           `yaml:"OCR" json:"OCR"`
	ASR           ASRConfig           `yaml:"ASR" json:"ASR"`
	Watchlist     WatchlistConfig     `yaml:"Watchlist" json:"Watchlist"`
	Tasks         []TaskConfig        `yaml:"Tasks" json:"Tasks,omitempty"`
	Summary       SummaryConfig       `yaml:"Summary" json:"Summary"`
	Notifications NotificationsConfig `yaml:"Notifications" json:"Notifications"`
	Providers     ProvidersConfig     `yaml:"Providers" json:"Providers"`
	Social        SocialConfig        `yaml:"Social" json:"Social,omitempty"`
}

// SkillConfig controls the independently deployable agent Skill enhancement pipeline.
// It intentionally does not reuse Collect configuration or services.
type SkillConfig struct {
	Media   SkillMediaConfig   `yaml:"Media" json:"Media,omitempty"`
	OCR     OCRConfig          `yaml:"OCR" json:"OCR,omitempty"`
	ASR     ASRConfig          `yaml:"ASR" json:"ASR,omitempty"`
	Summary SkillSummaryConfig `yaml:"Summary" json:"Summary,omitempty"`
}

// SkillMediaConfig controls media acquisition for explicit Skill OCR/ASR requests.
type SkillMediaConfig struct {
	DownloadMedia    bool   `yaml:"DownloadMedia" json:"DownloadMedia,omitempty"`
	Dir              string `yaml:"Dir" json:"Dir,omitempty"`
	MaxBytes         int64  `yaml:"MaxBytes" json:"MaxBytes,omitempty"`
	TimeoutSec       int    `yaml:"TimeoutSec" json:"TimeoutSec,omitempty"`
	MaxASRCandidates int    `yaml:"MaxASRCandidates" json:"MaxASRCandidates,omitempty"`
}

const (
	DefaultSkillMediaTimeoutSec  = 120
	DefaultSkillMaxASRCandidates = 2
)

// TimeoutSecOrDefault returns the download budget for explicit Skill media.
func (c SkillMediaConfig) TimeoutSecOrDefault() int {
	if c.TimeoutSec > 0 {
		return c.TimeoutSec
	}
	return DefaultSkillMediaTimeoutSec
}

// MaxASRCandidatesOrDefault bounds the primary media input plus ASR fallbacks.
func (c SkillMediaConfig) MaxASRCandidatesOrDefault() int {
	if c.MaxASRCandidates > 0 {
		return c.MaxASRCandidates
	}
	return DefaultSkillMaxASRCandidates
}

// SkillSummaryConfig is the optional local OpenAI-compatible fallback for a
// host Agent's own summary. It deliberately excludes Collect's DigFlow paths.
type SkillSummaryConfig struct {
	Enabled       *bool              `yaml:"Enabled" json:"Enabled,omitempty"`
	TimeoutSec    int                `yaml:"TimeoutSec" json:"TimeoutSec,omitempty"`
	MaxInputChars int                `yaml:"MaxInputChars" json:"MaxInputChars,omitempty"`
	InputBudget   SummaryInputBudget `yaml:"InputBudget" json:"InputBudget,omitempty"`
	LLM           LLMSummaryConfig   `yaml:"LLM" json:"LLM,omitempty"`
}

func (c SkillSummaryConfig) EnabledOrDefault() bool {
	return c.Enabled != nil && *c.Enabled
}

func (c SkillSummaryConfig) TimeoutSecOrDefault() int {
	if c.TimeoutSec > 0 {
		return c.TimeoutSec
	}
	return 60
}

func (c SkillSummaryConfig) MaxInputCharsOrDefault() int {
	if c.MaxInputChars > 0 {
		return c.MaxInputChars
	}
	return 30000
}

func (c SkillSummaryConfig) InputBudgetOrDefault() SummaryInputBudget {
	b := c.InputBudget
	if b.TextChars <= 0 {
		b.TextChars = 12000
	}
	if b.ASRChars <= 0 {
		b.ASRChars = 12000
	}
	if b.OCRChars <= 0 {
		b.OCRChars = 4000
	}
	if b.MetadataChars <= 0 {
		b.MetadataChars = 2000
	}
	return b
}

// CollectConfig local content collect and export settings.
type CollectConfig struct {
	OutputDir          string         `yaml:"OutputDir" json:"OutputDir,omitempty"`
	InboxDir           string         `yaml:"InboxDir" json:"InboxDir,omitempty"`
	DefaultFormats     []string       `yaml:"DefaultFormats" json:"DefaultFormats,omitempty"`
	IncludeMedia       *bool          `yaml:"IncludeMedia" json:"IncludeMedia,omitempty"`
	DownloadMedia      *bool          `yaml:"DownloadMedia" json:"DownloadMedia,omitempty"`
	PersistMode        string         `yaml:"PersistMode" json:"PersistMode,omitempty"` // files | index | sqlite | both
	DBPath             string         `yaml:"DBPath" json:"DBPath,omitempty"`
	IndexEnabled       *bool          `yaml:"IndexEnabled" json:"IndexEnabled,omitempty"`
	MaxMediaBytes      int64          `yaml:"MaxMediaBytes" json:"MaxMediaBytes,omitempty"`
	MediaTimeoutSec    int            `yaml:"MediaTimeoutSec" json:"MediaTimeoutSec,omitempty"`
	InboxPollInterval  int            `yaml:"InboxPollIntervalSec" json:"InboxPollIntervalSec,omitempty"`
	RateLimitMs        int            `yaml:"RateLimitMs" json:"RateLimitMs,omitempty"`
	PlatformRateLimits map[string]int `yaml:"PlatformRateLimits" json:"PlatformRateLimits,omitempty"`
}

// PersistModeOrDefault returns files | index | sqlite | both (default index).
func (c CollectConfig) PersistModeOrDefault() string {
	mode := strings.ToLower(strings.TrimSpace(c.PersistMode))
	switch mode {
	case "files", "index", "sqlite", "both":
		return mode
	default:
		return "index"
	}
}

// UseIndexJSON reports whether index.json should be maintained.
func (c CollectConfig) UseIndexJSON() bool {
	switch c.PersistModeOrDefault() {
	case "index", "both":
		if c.IndexEnabled != nil && !*c.IndexEnabled {
			return false
		}
		return true
	default:
		return false
	}
}

// UseSQLite reports whether collect.db should be maintained.
func (c CollectConfig) UseSQLite() bool {
	switch c.PersistModeOrDefault() {
	case "sqlite", "both":
		return true
	default:
		return false
	}
}

// DBPathOrDefault returns the SQLite database path.
func (c CollectConfig) DBPathOrDefault() string {
	if strings.TrimSpace(c.DBPath) != "" {
		return c.DBPath
	}
	return "storage/digcrawl/collect.db"
}

// LocalBrowserConfig local browser used by platform tools and collect.
type LocalBrowserConfig struct {
	Enabled                *bool    `yaml:"Enabled" json:"Enabled,omitempty"`
	MaxConcurrency         int      `yaml:"MaxConcurrency" json:"MaxConcurrency,omitempty"`
	TotalTimeoutSec        int      `yaml:"TotalTimeoutSec" json:"TotalTimeoutSec,omitempty"`
	NavigateTimeoutSec     int      `yaml:"NavigateTimeoutSec" json:"NavigateTimeoutSec,omitempty"`
	WaitSelectorTimeoutSec int      `yaml:"WaitSelectorTimeoutSec" json:"WaitSelectorTimeoutSec,omitempty"`
	AllowedDomains         []string `yaml:"AllowedDomains" json:"AllowedDomains,omitempty"`
	ChromePath             string   `yaml:"ChromePath" json:"ChromePath,omitempty"`
	Headless               *bool    `yaml:"Headless" json:"Headless,omitempty"`
	CookieStoreDir         string   `yaml:"CookieStoreDir" json:"CookieStoreDir,omitempty"`
	CDPMode                *bool    `yaml:"CDPMode" json:"CDPMode,omitempty"`
	CDPPort                int      `yaml:"CDPPort" json:"CDPPort,omitempty"`
	CDPConnectExisting     *bool    `yaml:"CDPConnectExisting" json:"CDPConnectExisting,omitempty"`
	UserDataDir            string   `yaml:"UserDataDir" json:"UserDataDir,omitempty"`
	ProxyURL               string   `yaml:"ProxyURL" json:"ProxyURL,omitempty"`
	ExtraArgs              []string `yaml:"ExtraArgs" json:"ExtraArgs,omitempty"`
}

// RuntimeEnvConfig controls explicit, config-file based runtime variable
// injection for embedded components that still consume process environment.
type RuntimeEnvConfig struct {
	Enabled    *bool             `yaml:"Enabled" json:"Enabled,omitempty"`
	ConfigPath string            `yaml:"ConfigPath" json:"ConfigPath,omitempty"`
	Variables  map[string]string `yaml:"Variables" json:"Variables,omitempty"`
}

func (c RuntimeEnvConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

// PlatformConfig platform read/retry/health settings.
type PlatformConfig struct {
	BrowseRetryMaxAttempts int               `yaml:"BrowseRetryMaxAttempts" json:"BrowseRetryMaxAttempts,omitempty"`
	BrowseRetryDelayMs     int               `yaml:"BrowseRetryDelayMs" json:"BrowseRetryDelayMs,omitempty"`
	ReadMetricsEnabled     *bool             `yaml:"ReadMetricsEnabled" json:"ReadMetricsEnabled,omitempty"`
	HealthProbes           map[string]string `yaml:"HealthProbes" json:"HealthProbes,omitempty"`
	X                      XAPIConfig        `yaml:"X" json:"X,omitempty"`
	Reddit                 RedditConfig      `yaml:"Reddit" json:"Reddit,omitempty"`
}

// SocialConfig controls non-official social platform crawling defaults.
type SocialConfig struct {
	Threads   SocialPlatformConfig `yaml:"Threads" json:"Threads,omitempty"`
	Instagram SocialPlatformConfig `yaml:"Instagram" json:"Instagram,omitempty"`
}

type SocialPlatformConfig struct {
	AuthMode          string `yaml:"AuthMode" json:"AuthMode,omitempty"` // public | local_session
	BrowserFallback   *bool  `yaml:"BrowserFallback" json:"BrowserFallback,omitempty"`
	RequestTimeoutSec int    `yaml:"RequestTimeoutSec" json:"RequestTimeoutSec,omitempty"`
	RateLimitMs       int    `yaml:"RateLimitMs" json:"RateLimitMs,omitempty"`
}

func (c SocialPlatformConfig) AuthModeOrDefault() string {
	switch strings.ToLower(strings.TrimSpace(c.AuthMode)) {
	case "local_session":
		return "local_session"
	default:
		return "public"
	}
}

func (c SocialPlatformConfig) BrowserFallbackOrDefault() bool {
	if c.BrowserFallback == nil {
		return false
	}
	return *c.BrowserFallback
}

func (c SocialPlatformConfig) AllowsLocalSession() bool {
	return c.AuthModeOrDefault() == "local_session"
}

// OCRConfig controls optional image and local-video-frame OCR enrichment.
type OCRConfig struct {
	Enabled          *bool               `yaml:"Enabled" json:"Enabled,omitempty"`
	Provider         string              `yaml:"Provider" json:"Provider,omitempty"` // openai_compatible | stub
	APIKey           string              `yaml:"APIKey" json:"APIKey,omitempty"`
	BaseURL          string              `yaml:"BaseURL" json:"BaseURL,omitempty"`
	Model            string              `yaml:"Model" json:"Model,omitempty"`
	Prompt           string              `yaml:"Prompt" json:"Prompt,omitempty"`
	TimeoutSec       int                 `yaml:"TimeoutSec" json:"TimeoutSec,omitempty"`
	MaxImages        int                 `yaml:"MaxImages" json:"MaxImages,omitempty"`
	MaxImageBytes    int64               `yaml:"MaxImageBytes" json:"MaxImageBytes,omitempty"`
	MaxTokens        int                 `yaml:"MaxTokens" json:"MaxTokens,omitempty"`
	AppendToMarkdown *bool               `yaml:"AppendToMarkdown" json:"AppendToMarkdown,omitempty"`
	AppendToText     *bool               `yaml:"AppendToText" json:"AppendToText,omitempty"`
	VideoFrames      VideoFrameOCRConfig `yaml:"VideoFrames" json:"VideoFrames,omitempty"`
}

// VideoFrameOCRConfig controls the explicit, bounded extraction of local video
// frames for OCR. It is disabled by default because a remote OCR provider may
// receive the extracted frames.
type VideoFrameOCRConfig struct {
	Enabled     *bool  `yaml:"Enabled" json:"Enabled,omitempty"`
	IntervalSec int    `yaml:"IntervalSec" json:"IntervalSec,omitempty"`
	MaxFrames   int    `yaml:"MaxFrames" json:"MaxFrames,omitempty"`
	FFmpegPath  string `yaml:"FFmpegPath" json:"FFmpegPath,omitempty"`
	TempDir     string `yaml:"TempDir" json:"TempDir,omitempty"`
}

// EnabledOrDefault reports whether local video frame OCR is explicitly enabled.
func (c VideoFrameOCRConfig) EnabledOrDefault() bool {
	return c.Enabled != nil && *c.Enabled
}

// IntervalSecOrDefault returns the spacing between extracted video frames.
func (c VideoFrameOCRConfig) IntervalSecOrDefault() int {
	if c.IntervalSec > 0 {
		return c.IntervalSec
	}
	return 5
}

// MaxFramesOrDefault bounds the number of frames sent to the OCR provider for
// one video. OCR.MaxImages remains the final per-content OCR ceiling.
func (c VideoFrameOCRConfig) MaxFramesOrDefault() int {
	if c.MaxFrames > 0 {
		return c.MaxFrames
	}
	return 8
}

// FFmpegPathOrDefault returns the executable used to extract video frames.
func (c VideoFrameOCRConfig) FFmpegPathOrDefault() string {
	if strings.TrimSpace(c.FFmpegPath) != "" {
		return strings.TrimSpace(c.FFmpegPath)
	}
	return "ffmpeg"
}

// EnabledOrDefault reports whether OCR enrichment is enabled.
func (c OCRConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

// ProviderOrDefault returns the configured OCR provider.
func (c OCRConfig) ProviderOrDefault() string {
	p := strings.ToLower(strings.TrimSpace(c.Provider))
	if p == "" {
		return "openai_compatible"
	}
	return p
}

// BaseURLOrDefault returns the OCR provider base URL.
func (c OCRConfig) BaseURLOrDefault() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	}
	return "https://api.openai.com/v1"
}

// ModelOrDefault returns the OCR model name.
func (c OCRConfig) ModelOrDefault() string {
	if strings.TrimSpace(c.Model) != "" {
		return strings.TrimSpace(c.Model)
	}
	return "gpt-4o-mini"
}

// PromptOrDefault returns the OCR extraction prompt.
func (c OCRConfig) PromptOrDefault() string {
	if strings.TrimSpace(c.Prompt) != "" {
		return strings.TrimSpace(c.Prompt)
	}
	return "Extract all readable text from this image. Return only the OCR text. Preserve line breaks when useful."
}

// TimeoutSecOrDefault returns the OCR request timeout.
func (c OCRConfig) TimeoutSecOrDefault() int {
	if c.TimeoutSec > 0 {
		return c.TimeoutSec
	}
	return 30
}

// MaxImagesOrDefault returns the maximum images OCR will process per content item.
func (c OCRConfig) MaxImagesOrDefault() int {
	if c.MaxImages > 0 {
		return c.MaxImages
	}
	return 8
}

// MaxImageBytesOrDefault returns the maximum local image payload size.
func (c OCRConfig) MaxImageBytesOrDefault() int64 {
	if c.MaxImageBytes > 0 {
		return c.MaxImageBytes
	}
	return 5 * 1024 * 1024
}

// MaxTokensOrDefault returns the OCR response token budget.
func (c OCRConfig) MaxTokensOrDefault() int {
	if c.MaxTokens > 0 {
		return c.MaxTokens
	}
	return 1200
}

// AppendToMarkdownOrDefault reports whether OCR text is appended to Markdown.
func (c OCRConfig) AppendToMarkdownOrDefault() bool {
	if c.AppendToMarkdown == nil {
		return true
	}
	return *c.AppendToMarkdown
}

// AppendToTextOrDefault reports whether OCR text is appended to plain text.
func (c OCRConfig) AppendToTextOrDefault() bool {
	if c.AppendToText == nil {
		return false
	}
	return *c.AppendToText
}

// ASRConfig controls optional audio transcription.
type ASRConfig struct {
	Enabled        *bool  `yaml:"Enabled" json:"Enabled,omitempty"`
	Provider       string `yaml:"Provider" json:"Provider,omitempty"` // api | local | stub
	APIKey         string `yaml:"APIKey" json:"APIKey,omitempty"`
	BaseURL        string `yaml:"BaseURL" json:"BaseURL,omitempty"`
	Model          string `yaml:"Model" json:"Model,omitempty"`
	Language       string `yaml:"Language" json:"Language,omitempty"`
	Prompt         string `yaml:"Prompt" json:"Prompt,omitempty"`
	ResponseFormat string `yaml:"ResponseFormat" json:"ResponseFormat,omitempty"`
	TimeoutSec     int    `yaml:"TimeoutSec" json:"TimeoutSec,omitempty"`
	// MaxTimeoutSec is the hard upper bound for one ASR operation. It gives
	// Runtime a finite envelope while TimeoutSec remains the local ASR floor.
	MaxTimeoutSec int    `yaml:"MaxTimeoutSec" json:"MaxTimeoutSec,omitempty"`
	FFmpegPath    string `yaml:"FFmpegPath" json:"FFmpegPath,omitempty"`
	TempDir       string `yaml:"TempDir" json:"TempDir,omitempty"`
	ModelCacheDir string `yaml:"ModelCacheDir" json:"ModelCacheDir,omitempty"`
	// LegacyModelCacheDirs is runtime-only compatibility metadata populated for
	// old Skill profiles that predate ModelCacheDir. It is deliberately not
	// persisted: users retain one canonical ModelCacheDir while the ASR runtime
	// can safely adopt an existing old cache without moving or re-downloading it.
	LegacyModelCacheDirs []string `yaml:"-" json:"-"`
	// LegacyHubCacheDir preserves the old HUGGINGFACE_HUB_CACHE contract when
	// it points to a custom directory that is not the conventional HF_HOME/hub.
	LegacyHubCacheDir string   `yaml:"-" json:"-"`
	AudioSampleRate   int      `yaml:"AudioSampleRate" json:"AudioSampleRate,omitempty"`
	MaxAudioBytes     int64    `yaml:"MaxAudioBytes" json:"MaxAudioBytes,omitempty"`
	LocalCommand      string   `yaml:"LocalCommand" json:"LocalCommand,omitempty"`
	LocalArgs         []string `yaml:"LocalArgs" json:"LocalArgs,omitempty"`
}

// EnabledOrDefault reports whether ASR tooling is enabled.
func (c ASRConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

// ProviderOrDefault returns the configured ASR provider.
func (c ASRConfig) ProviderOrDefault() string {
	switch strings.ToLower(strings.TrimSpace(c.Provider)) {
	case "api", "openai_compatible", "openai-compatible":
		return "api"
	case "local", "faster_whisper", "faster-whisper", "whisper":
		return "local"
	case "stub":
		return "stub"
	default:
		return "api"
	}
}

// BaseURLOrDefault returns the API ASR base URL.
func (c ASRConfig) BaseURLOrDefault() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	}
	return "https://api.openai.com/v1"
}

// ModelOrDefault returns the ASR model name.
func (c ASRConfig) ModelOrDefault() string {
	if strings.TrimSpace(c.Model) != "" {
		return strings.TrimSpace(c.Model)
	}
	if c.ProviderOrDefault() == "local" {
		return "small"
	}
	return "whisper-1"
}

// ResponseFormatOrDefault returns the API ASR response format.
func (c ASRConfig) ResponseFormatOrDefault() string {
	if strings.TrimSpace(c.ResponseFormat) != "" {
		return strings.ToLower(strings.TrimSpace(c.ResponseFormat))
	}
	return "verbose_json"
}

// SupportedASRResponseFormat reports whether format is supported by the API provider parser.
func SupportedASRResponseFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "verbose_json", "json", "text", "srt", "vtt":
		return true
	default:
		return false
	}
}

// TimeoutSecOrDefault returns ASR timeout seconds.
func (c ASRConfig) TimeoutSecOrDefault() int {
	if c.TimeoutSec > 0 {
		return c.TimeoutSec
	}
	return 300
}

// MaxTimeoutSecOrDefault returns the hard upper bound for one ASR operation.
// It deliberately has a larger default than TimeoutSec so local media can use
// its duration-derived budget without being silently cut off by Runtime.
func (c ASRConfig) MaxTimeoutSecOrDefault() int {
	if c.MaxTimeoutSec > 0 {
		return c.MaxTimeoutSec
	}
	return 1800
}

// FFmpegPathOrDefault returns ffmpeg executable path.
func (c ASRConfig) FFmpegPathOrDefault() string {
	if strings.TrimSpace(c.FFmpegPath) != "" {
		if expanded := strings.TrimSpace(os.ExpandEnv(strings.TrimSpace(c.FFmpegPath))); expanded != "" {
			return expanded
		}
	}
	return "ffmpeg"
}

// ModelCacheDirOrDefault returns the persistent cache used by local ASR
// models. Keeping it outside transient directories makes doctor checks and
// production runs observe the same model state.
func (c ASRConfig) ModelCacheDirOrDefault() string {
	if path := strings.TrimSpace(os.ExpandEnv(c.ModelCacheDir)); path != "" {
		return path
	}
	return filepath.Join("storage", "digcrawl", "asr_models")
}

// AudioSampleRateOrDefault returns extracted audio sample rate.
func (c ASRConfig) AudioSampleRateOrDefault() int {
	if c.AudioSampleRate > 0 {
		return c.AudioSampleRate
	}
	return 16000
}

// MaxAudioBytesOrDefault returns the maximum audio payload size.
func (c ASRConfig) MaxAudioBytesOrDefault() int64 {
	if c.MaxAudioBytes > 0 {
		return c.MaxAudioBytes
	}
	return 25 * 1024 * 1024
}

// LocalCommandOrDefault returns local ASR command.
func (c ASRConfig) LocalCommandOrDefault() string {
	if strings.TrimSpace(c.LocalCommand) != "" {
		return strings.TrimSpace(c.LocalCommand)
	}
	return "faster-whisper"
}

// WatchlistConfig controls periodic source monitoring.
type WatchlistConfig struct {
	Enabled          *bool                      `yaml:"Enabled" json:"Enabled,omitempty"`
	DryRun           *bool                      `yaml:"DryRun" json:"DryRun,omitempty"`
	DefaultInterval  string                     `yaml:"DefaultInterval" json:"DefaultInterval,omitempty"`
	Scheduler        SchedulerConfig            `yaml:"Scheduler" json:"Scheduler,omitempty"`
	Source           WatchlistSourceConfig      `yaml:"Source" json:"Source,omitempty"`
	NotifyOnFirstRun *bool                      `yaml:"NotifyOnFirstRun" json:"NotifyOnFirstRun,omitempty"`
	MaxItemsPerRun   int                        `yaml:"MaxItemsPerRun" json:"MaxItemsPerRun,omitempty"`
	LockTTL          string                     `yaml:"LockTTL" json:"LockTTL,omitempty"`
	Persistence      WatchlistPersistenceConfig `yaml:"Persistence" json:"Persistence,omitempty"`
	DBPath           string                     `yaml:"DBPath" json:"DBPath,omitempty"` // Deprecated: use Persistence.SQLitePath.
	Items            []WatchlistItem            `yaml:"Items" json:"Items,omitempty"`
}

// WatchlistPersistenceConfig controls local watchlist runtime persistence.
type WatchlistPersistenceConfig struct {
	Driver         string `yaml:"Driver" json:"Driver,omitempty"`
	SQLitePath     string `yaml:"SQLitePath" json:"SQLitePath,omitempty"`
	MySQLDSN       string `yaml:"MySQLDSN" json:"MySQLDSN,omitempty"`
	GormConfigPath string `yaml:"GormConfigPath" json:"GormConfigPath,omitempty"`
}

func (c WatchlistPersistenceConfig) DriverOrDefault() string {
	driver := strings.ToLower(strings.TrimSpace(c.Driver))
	if driver == "" {
		return "sqlite"
	}
	return driver
}

// WatchlistSourceConfig controls where watchlist source definitions are loaded from.
type WatchlistSourceConfig struct {
	Mode string `yaml:"Mode" json:"Mode,omitempty"` // yaml | database | mixed
}

func (c WatchlistSourceConfig) ModeOrDefault() string {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		return "yaml"
	}
	return mode
}

// SchedulerConfig controls the in-process periodic scheduler.
type SchedulerConfig struct {
	Enabled      *bool  `yaml:"Enabled" json:"Enabled,omitempty"`
	TickInterval string `yaml:"TickInterval" json:"TickInterval,omitempty"`
	Jitter       string `yaml:"Jitter" json:"Jitter,omitempty"`
}

func (c SchedulerConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

func (c SchedulerConfig) TickIntervalOrDefault() string {
	if strings.TrimSpace(c.TickInterval) != "" {
		return strings.TrimSpace(c.TickInterval)
	}
	return "5m"
}

func (c SchedulerConfig) JitterOrDefault() string {
	return strings.TrimSpace(c.Jitter)
}

// WatchlistItem is one monitored source.
type WatchlistItem struct {
	ID       string         `yaml:"ID" json:"ID,omitempty"`
	Platform string         `yaml:"Platform" json:"Platform,omitempty"`
	Type     string         `yaml:"Type" json:"Type,omitempty"`
	URL      string         `yaml:"URL" json:"URL,omitempty"`
	Name     string         `yaml:"Name" json:"Name,omitempty"`
	Interval string         `yaml:"Interval" json:"Interval,omitempty"`
	NotifyTo string         `yaml:"NotifyTo" json:"NotifyTo,omitempty"`
	Enabled  *bool          `yaml:"Enabled" json:"Enabled,omitempty"`
	Options  map[string]any `yaml:"Options" json:"Options,omitempty"`
}

func (c WatchlistConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

func (c WatchlistConfig) DryRunOrDefault() bool {
	if c.DryRun == nil {
		return false
	}
	return *c.DryRun
}

func (c WatchlistConfig) DefaultIntervalOrDefault() string {
	if strings.TrimSpace(c.DefaultInterval) != "" {
		return strings.TrimSpace(c.DefaultInterval)
	}
	return "6h"
}

func (c WatchlistConfig) LockTTLOrDefault() string {
	if strings.TrimSpace(c.LockTTL) != "" {
		return strings.TrimSpace(c.LockTTL)
	}
	return "30m"
}

func (c WatchlistConfig) MaxItemsPerRunOrDefault() int {
	if c.MaxItemsPerRun > 0 {
		return c.MaxItemsPerRun
	}
	return 20
}

func (c WatchlistConfig) NotifyOnFirstRunOrDefault() bool {
	if c.NotifyOnFirstRun == nil {
		return false
	}
	return *c.NotifyOnFirstRun
}

func (c WatchlistConfig) DBPathOrDefault(collectDB string) string {
	return c.StateDBPathOrDefault(collectDB)
}

func (c WatchlistConfig) PersistenceDriverOrDefault() string {
	return c.Persistence.DriverOrDefault()
}

func (c WatchlistConfig) SourceModeOrDefault() string {
	return c.Source.ModeOrDefault()
}

func (c WatchlistConfig) StateDBPathOrDefault(collectDB string) string {
	if strings.TrimSpace(c.Persistence.SQLitePath) != "" {
		return strings.TrimSpace(c.Persistence.SQLitePath)
	}
	if strings.TrimSpace(c.DBPath) != "" {
		return strings.TrimSpace(c.DBPath)
	}
	if strings.TrimSpace(collectDB) != "" {
		return strings.TrimSpace(collectDB)
	}
	return "storage/digcrawl/collect.db"
}

func (i WatchlistItem) EnabledOrDefault() bool {
	if i.Enabled == nil {
		return true
	}
	return *i.Enabled
}

// TaskConfig describes a scheduled task. The first supported task type is
// watchlist.collect; the shape is intentionally generic for future task kinds.
type TaskConfig struct {
	ID       string             `yaml:"ID" json:"ID,omitempty"`
	Name     string             `yaml:"Name" json:"Name,omitempty"`
	Type     string             `yaml:"Type" json:"Type,omitempty"`
	Enabled  *bool              `yaml:"Enabled" json:"Enabled,omitempty"`
	Schedule TaskScheduleConfig `yaml:"Schedule" json:"Schedule,omitempty"`
	Source   TaskSourceConfig   `yaml:"Source" json:"Source,omitempty"`
	Notify   TaskNotifyConfig   `yaml:"Notify" json:"Notify,omitempty"`
	Options  map[string]any     `yaml:"Options" json:"Options,omitempty"`
}

type TaskScheduleConfig struct {
	Mode  string `yaml:"Mode" json:"Mode,omitempty"`
	Every string `yaml:"Every" json:"Every,omitempty"`
	At    string `yaml:"At" json:"At,omitempty"`
}

type TaskSourceConfig struct {
	Platform string         `yaml:"Platform" json:"Platform,omitempty"`
	Kind     string         `yaml:"Kind" json:"Kind,omitempty"`
	URL      string         `yaml:"URL" json:"URL,omitempty"`
	Options  map[string]any `yaml:"Options" json:"Options,omitempty"`
}

type TaskNotifyConfig struct {
	Channel string `yaml:"Channel" json:"Channel,omitempty"`
}

func (t TaskConfig) EnabledOrDefault() bool {
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}

// SummaryConfig controls watchlist summary generation.
type SummaryConfig struct {
	Enabled             *bool                `yaml:"Enabled" json:"Enabled,omitempty"`
	Provider            string               `yaml:"Provider" json:"Provider,omitempty"`
	TimeoutSec          int                  `yaml:"TimeoutSec" json:"TimeoutSec,omitempty"`
	MaxInputChars       int                  `yaml:"MaxInputChars" json:"MaxInputChars,omitempty"`
	ImportanceThreshold string               `yaml:"ImportanceThreshold" json:"ImportanceThreshold,omitempty"`
	FallbackProvider    string               `yaml:"FallbackProvider" json:"FallbackProvider,omitempty"`
	Mode                string               `yaml:"Mode" json:"Mode,omitempty"`
	InputBudget         SummaryInputBudget   `yaml:"InputBudget" json:"InputBudget,omitempty"`
	DigFlow             DigFlowSummaryConfig `yaml:"DigFlow" json:"DigFlow,omitempty"`
	LLM                 LLMSummaryConfig     `yaml:"LLM" json:"LLM,omitempty"`
}

type SummaryInputBudget struct {
	TextChars     int `yaml:"TextChars" json:"TextChars,omitempty"`
	ASRChars      int `yaml:"ASRChars" json:"ASRChars,omitempty"`
	OCRChars      int `yaml:"OCRChars" json:"OCRChars,omitempty"`
	MetadataChars int `yaml:"MetadataChars" json:"MetadataChars,omitempty"`
}

type DigFlowSummaryConfig struct {
	RuntimeMode       string `yaml:"RuntimeMode" json:"RuntimeMode,omitempty"`
	RootDir           string `yaml:"RootDir" json:"RootDir,omitempty"`
	ConfigPath        string `yaml:"ConfigPath" json:"ConfigPath,omitempty"`
	GormConfigPath    string `yaml:"GormConfigPath" json:"GormConfigPath,omitempty"`
	EinoConfigPath    string `yaml:"EinoConfigPath" json:"EinoConfigPath,omitempty"`
	ManifestPath      string `yaml:"ManifestPath" json:"ManifestPath,omitempty"`
	GlobalToolsPath   string `yaml:"GlobalToolsPath" json:"GlobalToolsPath,omitempty"`
	GlobalSkillsDir   string `yaml:"GlobalSkillsDir" json:"GlobalSkillsDir,omitempty"`
	DisableCheckpoint *bool  `yaml:"DisableCheckpoint" json:"DisableCheckpoint,omitempty"`
	BaseURL           string `yaml:"BaseURL" json:"BaseURL,omitempty"`
	Endpoint          string `yaml:"Endpoint" json:"Endpoint,omitempty"`
	Agent             string `yaml:"Agent" json:"Agent,omitempty"`
	AuthToken         string `yaml:"AuthToken" json:"AuthToken,omitempty"`
}

type LLMSummaryConfig struct {
	APIKey  string `yaml:"APIKey" json:"APIKey,omitempty"`
	BaseURL string `yaml:"BaseURL" json:"BaseURL,omitempty"`
	Model   string `yaml:"Model" json:"Model,omitempty"`
}

func (c SummaryConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

func (c SummaryConfig) ProviderOrDefault() string {
	p := strings.ToLower(strings.TrimSpace(c.Provider))
	switch p {
	case "digflow_runtime", "digflow_http", "llm_direct", "extractive":
		return p
	default:
		return "extractive"
	}
}

func (c SummaryConfig) TimeoutSecOrDefault() int {
	if c.TimeoutSec > 0 {
		return c.TimeoutSec
	}
	return 60
}

func (c SummaryConfig) MaxInputCharsOrDefault() int {
	if c.MaxInputChars > 0 {
		return c.MaxInputChars
	}
	return 30000
}

func (c SummaryConfig) FallbackProviderOrDefault() string {
	if strings.TrimSpace(c.FallbackProvider) != "" {
		return strings.ToLower(strings.TrimSpace(c.FallbackProvider))
	}
	return "extractive"
}

func (c SummaryConfig) InputBudgetOrDefault() SummaryInputBudget {
	b := c.InputBudget
	if b.TextChars <= 0 {
		b.TextChars = 12000
	}
	if b.ASRChars <= 0 {
		b.ASRChars = 12000
	}
	if b.OCRChars <= 0 {
		b.OCRChars = 4000
	}
	if b.MetadataChars <= 0 {
		b.MetadataChars = 2000
	}
	return b
}

func (c DigFlowSummaryConfig) RuntimeModeOrDefault() string {
	mode := strings.ToLower(strings.TrimSpace(c.RuntimeMode))
	switch mode {
	case "embedded", "runtime", "local":
		return "embedded"
	case "http", "remote":
		return "http"
	default:
		return "embedded"
	}
}

func (c DigFlowSummaryConfig) DisableCheckpointOrDefault() bool {
	if c.DisableCheckpoint == nil {
		return true
	}
	return *c.DisableCheckpoint
}

// NotificationsConfig controls outbound notification providers.
type NotificationsConfig struct {
	Feishu FeishuNotificationConfig `yaml:"Feishu" json:"Feishu,omitempty"`
}

type FeishuNotificationConfig struct {
	Enabled        *bool           `yaml:"Enabled" json:"Enabled,omitempty"`
	DefaultChannel string          `yaml:"DefaultChannel" json:"DefaultChannel,omitempty"`
	ConfigPath     string          `yaml:"ConfigPath" json:"ConfigPath,omitempty"`
	APIBaseURL     string          `yaml:"APIBaseURL" json:"APIBaseURL,omitempty"`
	AppID          string          `yaml:"AppID" json:"AppID,omitempty"`
	AppSecret      string          `yaml:"AppSecret" json:"AppSecret,omitempty"`
	Channels       []FeishuChannel `yaml:"Channels" json:"Channels,omitempty"`
}

type FeishuChannel struct {
	ID              string `yaml:"ID" json:"ID,omitempty"`
	Mode            string `yaml:"Mode" json:"Mode,omitempty"` // webhook | app
	WebhookURL      string `yaml:"WebhookURL" json:"WebhookURL,omitempty"`
	Secret          string `yaml:"Secret" json:"Secret,omitempty"`
	ReceiveIDType   string `yaml:"ReceiveIDType" json:"ReceiveIDType,omitempty"`
	ReceiveID       string `yaml:"ReceiveID" json:"ReceiveID,omitempty"`
	TimeoutSec      int    `yaml:"TimeoutSec" json:"TimeoutSec,omitempty"`
	MaxMessageChars int    `yaml:"MaxMessageChars" json:"MaxMessageChars,omitempty"`
}

func (c FeishuNotificationConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

func (c FeishuNotificationConfig) APIBaseURLOrDefault() string {
	if strings.TrimSpace(c.APIBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	}
	return "https://open.feishu.cn"
}

func (c FeishuNotificationConfig) hasValues() bool {
	return c.Enabled != nil ||
		strings.TrimSpace(c.DefaultChannel) != "" ||
		strings.TrimSpace(c.ConfigPath) != "" ||
		strings.TrimSpace(c.APIBaseURL) != "" ||
		strings.TrimSpace(c.AppID) != "" ||
		strings.TrimSpace(c.AppSecret) != "" ||
		len(c.Channels) > 0
}

// XAPIConfig controls X/Twitter API based post reads.
type XAPIConfig struct {
	APIEnabled        *bool  `yaml:"APIEnabled" json:"APIEnabled,omitempty"`
	OEmbedEnabled     *bool  `yaml:"OEmbedEnabled" json:"OEmbedEnabled,omitempty"`
	AuthMode          string `yaml:"AuthMode" json:"AuthMode,omitempty"` // app_only | bearer
	APIBaseURL        string `yaml:"APIBaseURL" json:"APIBaseURL,omitempty"`
	TokenURL          string `yaml:"TokenURL" json:"TokenURL,omitempty"`
	OEmbedURL         string `yaml:"OEmbedURL" json:"OEmbedURL,omitempty"`
	ConsumerKey       string `yaml:"ConsumerKey" json:"ConsumerKey,omitempty"`
	ConsumerKeySecret string `yaml:"ConsumerKeySecret" json:"ConsumerKeySecret,omitempty"`
	BearerToken       string `yaml:"BearerToken" json:"BearerToken,omitempty"`
	APITimeoutSec     int    `yaml:"APITimeoutSec" json:"APITimeoutSec,omitempty"`
	APIMaxRetries     int    `yaml:"APIMaxRetries" json:"APIMaxRetries,omitempty"`
}

// OEmbedEnabledOrDefault reports whether X oEmbed fallback is enabled.
func (c XAPIConfig) OEmbedEnabledOrDefault() bool {
	if c.OEmbedEnabled == nil {
		return true
	}
	return *c.OEmbedEnabled
}

// EnabledOrDefault reports whether X API reads are enabled.
func (c XAPIConfig) EnabledOrDefault() bool {
	if c.APIEnabled == nil {
		return true
	}
	return *c.APIEnabled
}

// AuthModeOrDefault returns the configured X API auth mode.
func (c XAPIConfig) AuthModeOrDefault() string {
	mode := strings.ToLower(strings.TrimSpace(c.AuthMode))
	if mode == "bearer" || mode == "app_only" {
		return mode
	}
	if strings.TrimSpace(c.BearerToken) != "" {
		return "bearer"
	}
	return "app_only"
}

// APIBaseURLOrDefault returns the X API base URL.
func (c XAPIConfig) APIBaseURLOrDefault() string {
	if strings.TrimSpace(c.APIBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	}
	return "https://api.twitter.com"
}

// TokenURLOrDefault returns the OAuth2 app-only token endpoint.
func (c XAPIConfig) TokenURLOrDefault() string {
	if strings.TrimSpace(c.TokenURL) != "" {
		return strings.TrimSpace(c.TokenURL)
	}
	return "https://api.twitter.com/oauth2/token"
}

// OEmbedURLOrDefault returns the public X/Twitter oEmbed endpoint.
func (c XAPIConfig) OEmbedURLOrDefault() string {
	if strings.TrimSpace(c.OEmbedURL) != "" {
		return strings.TrimSpace(c.OEmbedURL)
	}
	return "https://publish.twitter.com/oembed"
}

// APITimeoutSecOrDefault returns the X API timeout in seconds.
func (c XAPIConfig) APITimeoutSecOrDefault() int {
	if c.APITimeoutSec > 0 {
		return c.APITimeoutSec
	}
	return 8
}

// APIMaxRetriesOrDefault returns transient retry attempts for X API requests.
func (c XAPIConfig) APIMaxRetriesOrDefault() int {
	if c.APIMaxRetries > 0 {
		return c.APIMaxRetries
	}
	return 1
}

// RedditConfig controls Reddit API and browser fallback reads.
type RedditConfig struct {
	APIEnabled      *bool                       `yaml:"APIEnabled" json:"APIEnabled,omitempty"`
	ClientID        string                      `yaml:"ClientID" json:"ClientID,omitempty"`
	ClientSecret    string                      `yaml:"ClientSecret" json:"ClientSecret,omitempty"`
	RefreshToken    string                      `yaml:"RefreshToken" json:"RefreshToken,omitempty"`
	UserAgent       string                      `yaml:"UserAgent" json:"UserAgent,omitempty"`
	OAuthMode       string                      `yaml:"OAuthMode" json:"OAuthMode,omitempty"` // client_credentials | refresh_token
	APIBaseURL      string                      `yaml:"APIBaseURL" json:"APIBaseURL,omitempty"`
	TokenURL        string                      `yaml:"TokenURL" json:"TokenURL,omitempty"`
	APITimeoutSec   int                         `yaml:"APITimeoutSec" json:"APITimeoutSec,omitempty"`
	APIMaxRetries   int                         `yaml:"APIMaxRetries" json:"APIMaxRetries,omitempty"`
	RateLimit       RedditRateLimitConfig       `yaml:"RateLimit" json:"RateLimit,omitempty"`
	Comments        RedditCommentsConfig        `yaml:"Comments" json:"Comments,omitempty"`
	BrowserFallback RedditBrowserFallbackConfig `yaml:"BrowserFallback" json:"BrowserFallback,omitempty"`
}

type RedditRateLimitConfig struct {
	RequestsPerMinute int   `yaml:"RequestsPerMinute" json:"RequestsPerMinute,omitempty"`
	HeaderDriven      *bool `yaml:"HeaderDriven" json:"HeaderDriven,omitempty"`
	BackoffOn429      *bool `yaml:"BackoffOn429" json:"BackoffOn429,omitempty"`
	MinRetryAfterSec  int   `yaml:"MinRetryAfterSec" json:"MinRetryAfterSec,omitempty"`
}

type RedditCommentsConfig struct {
	DefaultLimit    int    `yaml:"DefaultLimit" json:"DefaultLimit,omitempty"`
	DefaultDepth    int    `yaml:"DefaultDepth" json:"DefaultDepth,omitempty"`
	DefaultSort     string `yaml:"DefaultSort" json:"DefaultSort,omitempty"`
	ExpandMore      *bool  `yaml:"ExpandMore" json:"ExpandMore,omitempty"`
	ExpandMoreLimit int    `yaml:"ExpandMoreLimit" json:"ExpandMoreLimit,omitempty"`
}

type RedditBrowserFallbackConfig struct {
	Enabled           *bool `yaml:"Enabled" json:"Enabled,omitempty"`
	AllowWithoutOAuth *bool `yaml:"AllowWithoutOAuth" json:"AllowWithoutOAuth,omitempty"`
}

func (c RedditConfig) EnabledOrDefault() bool {
	if c.APIEnabled == nil {
		return true
	}
	return *c.APIEnabled
}

func (c RedditConfig) OAuthModeOrDefault() string {
	mode := strings.ToLower(strings.TrimSpace(c.OAuthMode))
	switch mode {
	case "refresh_token", "client_credentials":
		return mode
	default:
		if strings.TrimSpace(c.RefreshToken) != "" {
			return "refresh_token"
		}
		return "client_credentials"
	}
}

func (c RedditConfig) APIBaseURLOrDefault() string {
	if strings.TrimSpace(c.APIBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	}
	return "https://oauth.reddit.com"
}

func (c RedditConfig) TokenURLOrDefault() string {
	if strings.TrimSpace(c.TokenURL) != "" {
		return strings.TrimSpace(c.TokenURL)
	}
	return "https://www.reddit.com/api/v1/access_token"
}

func (c RedditConfig) UserAgentOrDefault() string {
	if strings.TrimSpace(c.UserAgent) != "" {
		return strings.TrimSpace(c.UserAgent)
	}
	return "digcrawl/1.0"
}

func (c RedditConfig) APITimeoutSecOrDefault() int {
	if c.APITimeoutSec > 0 {
		return c.APITimeoutSec
	}
	return 10
}

func (c RedditConfig) APIMaxRetriesOrDefault() int {
	if c.APIMaxRetries > 0 {
		return c.APIMaxRetries
	}
	return 1
}

func (c RedditCommentsConfig) LimitOrDefault() int {
	if c.DefaultLimit > 0 {
		return c.DefaultLimit
	}
	return 50
}

func (c RedditCommentsConfig) DepthOrDefault() int {
	if c.DefaultDepth > 0 {
		return c.DefaultDepth
	}
	return 3
}

func (c RedditCommentsConfig) SortOrDefault() string {
	sort := strings.ToLower(strings.TrimSpace(c.DefaultSort))
	switch sort {
	case "confidence", "top", "new", "controversial", "old", "qa":
		return sort
	default:
		return "confidence"
	}
}

func (c RedditCommentsConfig) ExpandMoreOrDefault() bool {
	if c.ExpandMore == nil {
		return false
	}
	return *c.ExpandMore
}

func (c RedditBrowserFallbackConfig) EnabledOrDefault() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

func (c RedditBrowserFallbackConfig) AllowWithoutOAuthOrDefault() bool {
	if c.AllowWithoutOAuth == nil {
		return false
	}
	return *c.AllowWithoutOAuth
}

// GatewayConfig HTTP Tool Gateway settings.
type GatewayConfig struct {
	Enabled                *bool    `yaml:"Enabled" json:"Enabled,omitempty"`
	ListenAddr             string   `yaml:"ListenAddr" json:"ListenAddr,omitempty"`
	InstanceID             string   `yaml:"InstanceID" json:"InstanceID,omitempty"`
	AuthToken              string   `yaml:"AuthToken" json:"AuthToken,omitempty"`
	AllowedTools           []string `yaml:"AllowedTools" json:"AllowedTools,omitempty"`
	AllowedReadPaths       []string `yaml:"AllowedReadPaths" json:"AllowedReadPaths,omitempty"`
	AllowedDomains         []string `yaml:"AllowedDomains" json:"AllowedDomains,omitempty"`
	ArtifactEnabled        *bool    `yaml:"ArtifactEnabled" json:"ArtifactEnabled,omitempty"`
	ArtifactDir            string   `yaml:"ArtifactDir" json:"ArtifactDir,omitempty"`
	ArtifactTTLMinutes     int      `yaml:"ArtifactTTLMinutes" json:"ArtifactTTLMinutes,omitempty"`
	ArtifactMaxInlineBytes int64    `yaml:"ArtifactMaxInlineBytes" json:"ArtifactMaxInlineBytes,omitempty"`
	RequireApproval        *bool    `yaml:"RequireApproval" json:"RequireApproval,omitempty"`
}

// CollectorConfig WebSocket reverse connector settings.
type CollectorConfig struct {
	ServerURL            string   `yaml:"ServerURL" json:"ServerURL,omitempty"`
	WSPath               string   `yaml:"WSPath" json:"WSPath,omitempty"`
	Token                string   `yaml:"Token" json:"Token,omitempty"`
	InstanceID           string   `yaml:"InstanceID" json:"InstanceID,omitempty"`
	HeartbeatIntervalSec int      `yaml:"HeartbeatIntervalSec" json:"HeartbeatIntervalSec,omitempty"`
	PullIntervalSec      int      `yaml:"PullIntervalSec" json:"PullIntervalSec,omitempty"`
	PullBatchSize        int      `yaml:"PullBatchSize" json:"PullBatchSize,omitempty"`
	ReconnectDelaySec    int      `yaml:"ReconnectDelaySec" json:"ReconnectDelaySec,omitempty"`
	MaxConcurrentCalls   int      `yaml:"MaxConcurrentCalls" json:"MaxConcurrentCalls,omitempty"`
	RateLimitCooldownMs  int      `yaml:"RateLimitCooldownMs" json:"RateLimitCooldownMs,omitempty"`
	CallQueueSize        int      `yaml:"CallQueueSize" json:"CallQueueSize,omitempty"`
	CallQueueWaitMs      int      `yaml:"CallQueueWaitMs" json:"CallQueueWaitMs,omitempty"`
	RetryMaxAttempts     int      `yaml:"RetryMaxAttempts" json:"RetryMaxAttempts,omitempty"`
	RetryDelayMs         int      `yaml:"RetryDelayMs" json:"RetryDelayMs,omitempty"`
	AllowedTools         []string `yaml:"AllowedTools" json:"AllowedTools,omitempty"`
}

// ProvidersConfig toggles built-in tool providers.
type ProvidersConfig struct {
	ExampleEnabled  *bool `yaml:"ExampleEnabled" json:"ExampleEnabled,omitempty"`
	PlatformEnabled *bool `yaml:"PlatformEnabled" json:"PlatformEnabled,omitempty"`
	BrowserEnabled  *bool `yaml:"BrowserEnabled" json:"BrowserEnabled,omitempty"`
	ASREnabled      *bool `yaml:"ASREnabled" json:"ASREnabled,omitempty"`
	StubEnabled     *bool `yaml:"StubEnabled" json:"StubEnabled,omitempty"`
}

var (
	defaultCfg Config
	once       sync.Once
	skillMu    sync.RWMutex
)

func init() {
	once.Do(func() {
		enabled := true
		disabled := false
		defaultCfg = Config{
			Gateway: GatewayConfig{
				ListenAddr: "127.0.0.1:8787",
				InstanceID: "digcrawl-local",
				AllowedTools: []string{
					"gateway.ping",
					"browser.browse",
					"browser.snapshot",
					"wechat.article.read",
					"xiaohongshu.note.read",
					"douyin.video.read",
					"toutiao.article.read",
					"toutiao.video.read",
					"feed.read",
					"bilibili.video.read",
					"weibo.post.read",
					"kuaishou.video.read",
					"zhihu.content.read",
					"x.post.read",
					"reddit.post.read",
					"reddit.subreddit.read",
					"reddit.user.read",
					"threads.post.read",
					"instagram.content.read",
					"asr.transcribe",
				},
				ArtifactDir:            "storage/digcrawl/artifacts",
				ArtifactTTLMinutes:     60,
				ArtifactMaxInlineBytes: 10 * 1024 * 1024,
				ArtifactEnabled:        &enabled,
				RequireApproval:        &enabled,
			},
			Collector: CollectorConfig{
				WSPath:               "/digcrawl/v1/collector/ws",
				HeartbeatIntervalSec: 30,
				PullIntervalSec:      0,
				ReconnectDelaySec:    5,
				PullBatchSize:        1,
				MaxConcurrentCalls:   1,
				RateLimitCooldownMs:  1000,
				CallQueueSize:        32,
				CallQueueWaitMs:      30000,
				RetryMaxAttempts:     2,
				RetryDelayMs:         1500,
				AllowedTools: []string{
					"gateway.ping",
					"browser.browse",
					"browser.snapshot",
					"wechat.article.read",
					"xiaohongshu.note.read",
					"douyin.video.read",
					"toutiao.article.read",
					"toutiao.video.read",
					"feed.read",
					"bilibili.video.read",
					"weibo.post.read",
					"kuaishou.video.read",
					"zhihu.content.read",
					"x.post.read",
					"reddit.post.read",
					"reddit.subreddit.read",
					"reddit.user.read",
					"threads.post.read",
					"instagram.content.read",
					"asr.transcribe",
				},
			},
			Collect: CollectConfig{
				OutputDir:         "collect-output",
				InboxDir:          "collect-inbox",
				DefaultFormats:    []string{"md", "json"},
				IncludeMedia:      &enabled,
				DownloadMedia:     &enabled,
				PersistMode:       "index",
				DBPath:            "storage/digcrawl/collect.db",
				IndexEnabled:      &enabled,
				MaxMediaBytes:     10 * 1024 * 1024,
				MediaTimeoutSec:   30,
				InboxPollInterval: 5,
				RateLimitMs:       1500,
				PlatformRateLimits: map[string]int{
					"xiaohongshu": 2000,
					"douyin":      2500,
					"toutiao":     2000,
					"reddit":      2500,
				},
			},
			LocalBrowser: LocalBrowserConfig{
				Enabled:                &enabled,
				MaxConcurrency:         2,
				TotalTimeoutSec:        90,
				NavigateTimeoutSec:     45,
				WaitSelectorTimeoutSec: 25,
				AllowedDomains: []string{
					"mp.weixin.qq.com", "weixin.qq.com",
					"xiaohongshu.com", "www.xiaohongshu.com", "edith.xiaohongshu.com", "xhslink.com",
					"douyin.com", "www.douyin.com", "iesdouyin.com", "v.douyin.com",
					"toutiao.com", "www.toutiao.com", "m.toutiao.com",
					"bilibili.com", "www.bilibili.com", "m.bilibili.com", "space.bilibili.com", "b23.tv",
					"weibo.com", "www.weibo.com", "m.weibo.cn",
					"kuaishou.com", "www.kuaishou.com", "v.kuaishou.com",
					"zhihu.com", "www.zhihu.com", "zhuanlan.zhihu.com",
					"x.com", "twitter.com", "www.twitter.com",
					"reddit.com", "www.reddit.com", "old.reddit.com", "new.reddit.com", "redd.it",
					"threads.net", "www.threads.net", "threads.com", "www.threads.com",
					"instagram.com", "www.instagram.com",
				},
				Headless:       &enabled,
				CookieStoreDir: "storage/digcrawl/browser_cookies",
			},
			Platform: PlatformConfig{
				BrowseRetryMaxAttempts: 2,
				BrowseRetryDelayMs:     800,
				ReadMetricsEnabled:     &enabled,
				HealthProbes:           map[string]string{},
				X: XAPIConfig{
					APIEnabled:        &enabled,
					OEmbedEnabled:     &enabled,
					AuthMode:          "app_only",
					APIBaseURL:        "https://api.twitter.com",
					TokenURL:          "https://api.twitter.com/oauth2/token",
					OEmbedURL:         "https://publish.twitter.com/oembed",
					ConsumerKey:       "r3lDvHngF1lLm9nbKABcpieHt",
					ConsumerKeySecret: "DdQgNA7LVBByIAlQTMniEwH4r42dFIfCtpOTxPyWjhTIZe21WI",
					APITimeoutSec:     8,
					APIMaxRetries:     1,
				},
				Reddit: RedditConfig{
					APIEnabled:    &enabled,
					OAuthMode:     "client_credentials",
					APIBaseURL:    "https://oauth.reddit.com",
					TokenURL:      "https://www.reddit.com/api/v1/access_token",
					UserAgent:     "digcrawl/1.0",
					APITimeoutSec: 10,
					APIMaxRetries: 1,
					RateLimit: RedditRateLimitConfig{
						RequestsPerMinute: 60,
						HeaderDriven:      &enabled,
						BackoffOn429:      &enabled,
						MinRetryAfterSec:  30,
					},
					Comments: RedditCommentsConfig{
						DefaultLimit:    50,
						DefaultDepth:    3,
						DefaultSort:     "confidence",
						ExpandMore:      nil,
						ExpandMoreLimit: 0,
					},
					BrowserFallback: RedditBrowserFallbackConfig{
						Enabled:           &enabled,
						AllowWithoutOAuth: nil,
					},
				},
			},
			Social: SocialConfig{
				Threads: SocialPlatformConfig{
					AuthMode:          "public",
					BrowserFallback:   &disabled,
					RequestTimeoutSec: 30,
				},
				Instagram: SocialPlatformConfig{
					AuthMode:          "public",
					BrowserFallback:   &disabled,
					RequestTimeoutSec: 30,
					RateLimitMs:       5000,
				},
			},
			OCR: OCRConfig{
				Provider:         "openai_compatible",
				BaseURL:          "https://api.openai.com/v1",
				Model:            "gpt-4o-mini",
				TimeoutSec:       30,
				MaxImages:        8,
				MaxImageBytes:    5 * 1024 * 1024,
				MaxTokens:        1200,
				AppendToMarkdown: &enabled,
				VideoFrames: VideoFrameOCRConfig{
					IntervalSec: 5,
					MaxFrames:   8,
				},
			},
			ASR: ASRConfig{
				Provider:        "api",
				BaseURL:         "https://api.openai.com/v1",
				ResponseFormat:  "verbose_json",
				TimeoutSec:      300,
				MaxTimeoutSec:   1800,
				FFmpegPath:      "ffmpeg",
				ModelCacheDir:   filepath.Join("storage", "digcrawl", "asr_models"),
				AudioSampleRate: 16000,
				MaxAudioBytes:   25 * 1024 * 1024,
				LocalCommand:    "faster-whisper",
			},
			Watchlist: WatchlistConfig{
				DefaultInterval:  "6h",
				Scheduler:        SchedulerConfig{TickInterval: "5m"},
				Source:           WatchlistSourceConfig{Mode: "yaml"},
				MaxItemsPerRun:   20,
				LockTTL:          "30m",
				Persistence:      WatchlistPersistenceConfig{Driver: "sqlite", SQLitePath: "storage/digcrawl/collect.db"},
				DBPath:           "storage/digcrawl/collect.db",
				NotifyOnFirstRun: nil,
			},
			Summary: SummaryConfig{
				Provider:            "extractive",
				TimeoutSec:          60,
				MaxInputChars:       30000,
				ImportanceThreshold: "medium",
				FallbackProvider:    "extractive",
				Mode:                "per_item_and_digest",
				InputBudget: SummaryInputBudget{
					TextChars:     12000,
					ASRChars:      12000,
					OCRChars:      4000,
					MetadataChars: 2000,
				},
				DigFlow: DigFlowSummaryConfig{
					RuntimeMode:       "embedded",
					RootDir:           ".",
					ConfigPath:        "config/digflow.yml",
					GormConfigPath:    "config/digflow-gorm.yml",
					EinoConfigPath:    "config/eino.yml",
					ManifestPath:      "agents/watchlist/agents.yml",
					GlobalToolsPath:   "config/global_tools.yml",
					GlobalSkillsDir:   "skills",
					DisableCheckpoint: &enabled,
					BaseURL:           "http://127.0.0.1:20201",
					Endpoint:          "/api/v1/invoke",
					Agent:             "watchlist_summary_agent",
				},
				LLM: LLMSummaryConfig{
					BaseURL: "https://api.openai.com/v1",
					Model:   "gpt-4o-mini",
				},
			},
			Providers: ProvidersConfig{
				ExampleEnabled:  &enabled,
				PlatformEnabled: &enabled,
				BrowserEnabled:  &enabled,
				ASREnabled:      &enabled,
			},
		}
	})
}

// Get returns the default configuration (after Load or defaults).
func Get() *Config {
	return &defaultCfg
}

// Skill returns the active Skill configuration as a stable snapshot. The MCP
// opt-in can update this section while other stdio workers read content, so
// callers in the Skill pipeline must not retain a mutable global pointer.
func Skill() SkillConfig {
	skillMu.RLock()
	defer skillMu.RUnlock()
	return defaultCfg.Skill
}

// SetSkill replaces the active Skill configuration. It is used by the local
// Skill opt-in and by tests that exercise Skill-owned enhancement behavior.
func SetSkill(cfg SkillConfig) {
	skillMu.Lock()
	defer skillMu.Unlock()
	defaultCfg.Skill = cfg
}

// Load reads YAML from path, applies explicit runtime environment injection,
// and replaces the process-wide default config.
func Load(path string) (*Config, error) {
	return load(path, true)
}

// Validate reads and normalizes a configuration file without changing process
// environment or the process-wide active configuration. It is the safe read
// path used by `digcrawl config validate`.
func Validate(path string) (*Config, error) {
	return load(path, false)
}

func load(path string, applyRuntimeEnv bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := validateSkillSummaryConfig(data); err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	mergeDefaults(&cfg)
	cfg.SourcePath = normalizedConfigPath(path)
	normalizeSkillASRPaths(&cfg)
	if applyRuntimeEnv {
		if err := mergeLocalFeishuConfig(&cfg); err != nil {
			return nil, err
		}
		if err := mergeLocalRuntimeEnvConfig(&cfg); err != nil {
			return nil, err
		}
		if err := applyRuntimeEnvConfig(cfg.RuntimeEnv); err != nil {
			return nil, err
		}
		skillMu.Lock()
		defaultCfg = cfg
		skillMu.Unlock()
		return &defaultCfg, nil
	}
	return &cfg, nil
}

// normalizeSkillASRPaths makes a Skill profile self-contained: persistent
// model state and explicitly configured local runner paths are anchored to the
// profile that selected them, never to the process working directory. Root ASR
// configuration keeps its existing project-relative semantics.
func normalizeSkillASRPaths(cfg *Config) {
	if cfg == nil || cfg.SourcePath == "" {
		return
	}
	profileDir := filepath.Dir(cfg.SourcePath)
	cacheDir := strings.TrimSpace(os.ExpandEnv(cfg.Skill.ASR.ModelCacheDir))
	if cacheDir == "" {
		cacheDir = filepath.Join(profileDir, "asr_models")
		cfg.Skill.ASR.LegacyModelCacheDirs, cfg.Skill.ASR.LegacyHubCacheDir = legacySkillASRCacheDirs()
	}
	if !filepath.IsAbs(cacheDir) {
		cacheDir = filepath.Join(profileDir, cacheDir)
	}
	cfg.Skill.ASR.ModelCacheDir = filepath.Clean(cacheDir)
	cfg.Skill.ASR.LocalCommand = resolveSkillASRPath(profileDir, cfg.Skill.ASR.LocalCommand)
	if len(cfg.Skill.ASR.LocalArgs) > 0 {
		args := append([]string(nil), cfg.Skill.ASR.LocalArgs...)
		args[0] = resolveSkillASRPath(profileDir, args[0])
		cfg.Skill.ASR.LocalArgs = args
	}
}

// legacySkillASRCacheDirs records the only implicit cache location used by
// pre-ModelCacheDir Skill profiles. The active runtime persists an adoption
// marker after the first successful discovery, so future service starts do not
// depend on cwd again. Explicit ModelCacheDir values are always authoritative
// and never receive implicit migration candidates.
func legacySkillASRCacheDirs() ([]string, string) {
	var candidates []string
	// Before ModelCacheDir existed, the local runner respected Hugging Face's
	// standard HF_HOME override. Preserve that higher-priority legacy contract
	// before considering DigCrawl's former cwd-relative fallback.
	legacyHubCacheDir := ""
	if hubCache := strings.TrimSpace(os.ExpandEnv(os.Getenv("HUGGINGFACE_HUB_CACHE"))); hubCache != "" {
		hubCache = absoluteCleanPath(hubCache)
		if filepath.Base(hubCache) == "hub" {
			candidates = append(candidates, filepath.Dir(hubCache))
		} else {
			legacyHubCacheDir = hubCache
		}
	}
	if hfHome := strings.TrimSpace(os.ExpandEnv(os.Getenv("HF_HOME"))); hfHome != "" {
		candidates = append(candidates, absoluteCleanPath(hfHome))
	}
	wd, err := os.Getwd()
	if err == nil && strings.TrimSpace(wd) != "" {
		candidates = append(candidates, absoluteCleanPath(filepath.Join(wd, ".cache", "huggingface")))
	}
	return uniqueCleanPaths(candidates), legacyHubCacheDir
}

func uniqueCleanPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = absoluteCleanPath(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func absoluteCleanPath(path string) string {
	path = filepath.Clean(path)
	if absolute, err := filepath.Abs(path); err == nil {
		return filepath.Clean(absolute)
	}
	return path
}

func resolveSkillASRPath(profileDir, value string) string {
	value = strings.TrimSpace(os.ExpandEnv(value))
	if value == "" || filepath.IsAbs(value) || !looksLikeLocalPath(value) {
		return value
	}
	return filepath.Clean(filepath.Join(profileDir, value))
}

func looksLikeLocalPath(value string) bool {
	return strings.ContainsRune(value, filepath.Separator) || strings.Contains(value, "/") || strings.Contains(value, "\\")
}

// validateSkillSummaryConfig rejects fields that belonged to the former
// DigFlow-backed Skill summary. Keep this narrow: other configuration remains
// permissive, while an enabled local Summary must not silently change meaning.
func validateSkillSummaryConfig(data []byte) error {
	var raw struct {
		Skill struct {
			Summary map[string]any `yaml:"Summary"`
		} `yaml:"Skill"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	enabled, _ := raw.Skill.Summary["Enabled"].(bool)
	if !enabled {
		return nil
	}
	for _, field := range []string{"Provider", "FallbackProvider", "Mode", "ImportanceThreshold", "DigFlow"} {
		if _, ok := raw.Skill.Summary[field]; ok {
			return fmt.Errorf("Skill.Summary.%s is no longer supported; remove it and configure Skill.Summary.LLM (APIKey, BaseURL, Model) for the optional local fallback", field)
		}
	}
	budget, _ := raw.Skill.Summary["InputBudget"].(map[string]any)
	if _, ok := budget["MetadataChars"]; ok {
		return fmt.Errorf("Skill.Summary.InputBudget.MetadataChars is unsupported because the local summary does not send platform metadata")
	}
	return nil
}

func applyRuntimeEnvConfig(cfg RuntimeEnvConfig) error {
	if !cfg.EnabledOrDefault() {
		return nil
	}
	for k, v := range cfg.Variables {
		name := strings.TrimSpace(k)
		if !isValidEnvName(name) {
			return fmt.Errorf("RuntimeEnv variable %q is invalid", k)
		}
		if strings.IndexByte(v, 0) >= 0 {
			return fmt.Errorf("RuntimeEnv variable %q contains NUL byte", name)
		}
		if err := os.Setenv(name, v); err != nil {
			return fmt.Errorf("set RuntimeEnv variable %q: %w", name, err)
		}
	}
	return nil
}

func isValidEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' {
				continue
			}
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func mergeLocalRuntimeEnvConfig(cfg *Config) error {
	path := strings.TrimSpace(cfg.RuntimeEnv.ConfigPath)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var local struct {
		RuntimeEnv RuntimeEnvConfig  `yaml:"RuntimeEnv"`
		Enabled    *bool             `yaml:"Enabled"`
		Variables  map[string]string `yaml:"Variables"`
	}
	if err := yaml.Unmarshal(data, &local); err != nil {
		return err
	}
	override := local.RuntimeEnv
	if override.Enabled == nil && len(override.Variables) == 0 && (local.Enabled != nil || len(local.Variables) > 0) {
		override.Enabled = local.Enabled
		override.Variables = local.Variables
	}
	mergeRuntimeEnvConfig(&cfg.RuntimeEnv, override)
	return nil
}

func mergeRuntimeEnvConfig(dst *RuntimeEnvConfig, src RuntimeEnvConfig) {
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
	}
	if strings.TrimSpace(src.ConfigPath) != "" {
		dst.ConfigPath = src.ConfigPath
	}
	if len(src.Variables) > 0 {
		if dst.Variables == nil {
			dst.Variables = map[string]string{}
		}
		for k, v := range src.Variables {
			dst.Variables[k] = v
		}
	}
}

func mergeLocalFeishuConfig(cfg *Config) error {
	path := strings.TrimSpace(cfg.Notifications.Feishu.ConfigPath)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var local struct {
		Feishu        FeishuNotificationConfig `yaml:"Feishu"`
		Notifications NotificationsConfig      `yaml:"Notifications"`
	}
	if err := yaml.Unmarshal(data, &local); err != nil {
		return err
	}
	override := local.Feishu
	if !override.hasValues() {
		override = local.Notifications.Feishu
	}
	mergeFeishuNotificationConfig(&cfg.Notifications.Feishu, override)
	return nil
}

func mergeFeishuNotificationConfig(dst *FeishuNotificationConfig, src FeishuNotificationConfig) {
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
	}
	if strings.TrimSpace(src.DefaultChannel) != "" {
		dst.DefaultChannel = src.DefaultChannel
	}
	if strings.TrimSpace(src.ConfigPath) != "" {
		dst.ConfigPath = src.ConfigPath
	}
	if strings.TrimSpace(src.APIBaseURL) != "" {
		dst.APIBaseURL = src.APIBaseURL
	}
	if strings.TrimSpace(src.AppID) != "" {
		dst.AppID = src.AppID
	}
	if strings.TrimSpace(src.AppSecret) != "" {
		dst.AppSecret = src.AppSecret
	}
	if len(src.Channels) > 0 {
		dst.Channels = src.Channels
	}
}

func mergeDefaults(cfg *Config) {
	if cfg.Gateway.ListenAddr == "" {
		cfg.Gateway.ListenAddr = defaultCfg.Gateway.ListenAddr
	}
	if cfg.Gateway.InstanceID == "" {
		cfg.Gateway.InstanceID = defaultCfg.Gateway.InstanceID
	}
	if cfg.Gateway.ArtifactDir == "" {
		cfg.Gateway.ArtifactDir = defaultCfg.Gateway.ArtifactDir
	}
	if cfg.Gateway.ArtifactTTLMinutes == 0 {
		cfg.Gateway.ArtifactTTLMinutes = defaultCfg.Gateway.ArtifactTTLMinutes
	}
	if cfg.Gateway.ArtifactMaxInlineBytes <= 0 {
		cfg.Gateway.ArtifactMaxInlineBytes = defaultCfg.Gateway.ArtifactMaxInlineBytes
	}
	if cfg.Gateway.RequireApproval == nil {
		cfg.Gateway.RequireApproval = defaultCfg.Gateway.RequireApproval
	}
	if len(cfg.Gateway.AllowedTools) == 0 {
		cfg.Gateway.AllowedTools = defaultCfg.Gateway.AllowedTools
	}
	if cfg.Collector.WSPath == "" {
		cfg.Collector.WSPath = defaultCfg.Collector.WSPath
	}
	if cfg.Collector.HeartbeatIntervalSec == 0 {
		cfg.Collector.HeartbeatIntervalSec = defaultCfg.Collector.HeartbeatIntervalSec
	}
	if cfg.Collector.ReconnectDelaySec == 0 {
		cfg.Collector.ReconnectDelaySec = defaultCfg.Collector.ReconnectDelaySec
	}
	if cfg.Collector.PullBatchSize == 0 {
		cfg.Collector.PullBatchSize = defaultCfg.Collector.PullBatchSize
	}
	if cfg.Collector.MaxConcurrentCalls == 0 {
		cfg.Collector.MaxConcurrentCalls = defaultCfg.Collector.MaxConcurrentCalls
	}
	if cfg.Collector.RateLimitCooldownMs == 0 {
		cfg.Collector.RateLimitCooldownMs = defaultCfg.Collector.RateLimitCooldownMs
	}
	if cfg.Collector.CallQueueSize == 0 {
		cfg.Collector.CallQueueSize = defaultCfg.Collector.CallQueueSize
	}
	if cfg.Collector.CallQueueWaitMs == 0 {
		cfg.Collector.CallQueueWaitMs = defaultCfg.Collector.CallQueueWaitMs
	}
	if cfg.Collector.RetryMaxAttempts == 0 {
		cfg.Collector.RetryMaxAttempts = defaultCfg.Collector.RetryMaxAttempts
	}
	if cfg.Collector.RetryDelayMs == 0 {
		cfg.Collector.RetryDelayMs = defaultCfg.Collector.RetryDelayMs
	}
	if len(cfg.Collector.AllowedTools) == 0 {
		cfg.Collector.AllowedTools = defaultCfg.Collector.AllowedTools
	}
	if cfg.Providers.ExampleEnabled == nil {
		cfg.Providers.ExampleEnabled = defaultCfg.Providers.ExampleEnabled
	}
	if cfg.Providers.PlatformEnabled == nil {
		cfg.Providers.PlatformEnabled = defaultCfg.Providers.PlatformEnabled
	}
	if cfg.Providers.BrowserEnabled == nil {
		cfg.Providers.BrowserEnabled = defaultCfg.Providers.BrowserEnabled
	}
	if cfg.Providers.ASREnabled == nil {
		cfg.Providers.ASREnabled = defaultCfg.Providers.ASREnabled
	}
	if cfg.Providers.StubEnabled == nil {
		cfg.Providers.StubEnabled = defaultCfg.Providers.StubEnabled
	}
	if cfg.Collect.OutputDir == "" {
		cfg.Collect.OutputDir = defaultCfg.Collect.OutputDir
	}
	if len(cfg.Collect.DefaultFormats) == 0 {
		cfg.Collect.DefaultFormats = defaultCfg.Collect.DefaultFormats
	}
	if cfg.Collect.IncludeMedia == nil {
		cfg.Collect.IncludeMedia = defaultCfg.Collect.IncludeMedia
	}
	if cfg.Collect.InboxDir == "" {
		cfg.Collect.InboxDir = defaultCfg.Collect.InboxDir
	}
	if cfg.Collect.DownloadMedia == nil {
		cfg.Collect.DownloadMedia = defaultCfg.Collect.DownloadMedia
	}
	if cfg.Collect.IndexEnabled == nil {
		cfg.Collect.IndexEnabled = defaultCfg.Collect.IndexEnabled
	}
	if cfg.Collect.MaxMediaBytes == 0 {
		cfg.Collect.MaxMediaBytes = defaultCfg.Collect.MaxMediaBytes
	}
	if cfg.Collect.MediaTimeoutSec == 0 {
		cfg.Collect.MediaTimeoutSec = defaultCfg.Collect.MediaTimeoutSec
	}
	if cfg.Collect.InboxPollInterval == 0 {
		cfg.Collect.InboxPollInterval = defaultCfg.Collect.InboxPollInterval
	}
	if cfg.Collect.RateLimitMs == 0 {
		cfg.Collect.RateLimitMs = defaultCfg.Collect.RateLimitMs
	}
	if cfg.Collect.DBPath == "" {
		cfg.Collect.DBPath = defaultCfg.Collect.DBPath
	}
	if cfg.Collect.PlatformRateLimits == nil {
		cfg.Collect.PlatformRateLimits = defaultCfg.Collect.PlatformRateLimits
	}
	if cfg.LocalBrowser.MaxConcurrency == 0 {
		cfg.LocalBrowser.MaxConcurrency = defaultCfg.LocalBrowser.MaxConcurrency
	}
	if cfg.LocalBrowser.TotalTimeoutSec == 0 {
		cfg.LocalBrowser.TotalTimeoutSec = defaultCfg.LocalBrowser.TotalTimeoutSec
	}
	if cfg.LocalBrowser.NavigateTimeoutSec == 0 {
		cfg.LocalBrowser.NavigateTimeoutSec = defaultCfg.LocalBrowser.NavigateTimeoutSec
	}
	if cfg.LocalBrowser.WaitSelectorTimeoutSec == 0 {
		cfg.LocalBrowser.WaitSelectorTimeoutSec = defaultCfg.LocalBrowser.WaitSelectorTimeoutSec
	}
	if cfg.LocalBrowser.CookieStoreDir == "" {
		cfg.LocalBrowser.CookieStoreDir = defaultCfg.LocalBrowser.CookieStoreDir
	}
	if cfg.LocalBrowser.Enabled == nil {
		cfg.LocalBrowser.Enabled = defaultCfg.LocalBrowser.Enabled
	}
	if cfg.LocalBrowser.Headless == nil {
		cfg.LocalBrowser.Headless = defaultCfg.LocalBrowser.Headless
	}
	if cfg.LocalBrowser.CDPMode == nil {
		cfg.LocalBrowser.CDPMode = defaultCfg.LocalBrowser.CDPMode
	}
	if cfg.LocalBrowser.CDPPort == 0 {
		cfg.LocalBrowser.CDPPort = defaultCfg.LocalBrowser.CDPPort
	}
	if cfg.LocalBrowser.CDPConnectExisting == nil {
		cfg.LocalBrowser.CDPConnectExisting = defaultCfg.LocalBrowser.CDPConnectExisting
	}
	if len(cfg.LocalBrowser.AllowedDomains) == 0 {
		cfg.LocalBrowser.AllowedDomains = defaultCfg.LocalBrowser.AllowedDomains
	}
	if cfg.Platform.BrowseRetryMaxAttempts == 0 {
		cfg.Platform.BrowseRetryMaxAttempts = defaultCfg.Platform.BrowseRetryMaxAttempts
	}
	if cfg.Platform.BrowseRetryDelayMs == 0 {
		cfg.Platform.BrowseRetryDelayMs = defaultCfg.Platform.BrowseRetryDelayMs
	}
	if cfg.Platform.ReadMetricsEnabled == nil {
		cfg.Platform.ReadMetricsEnabled = defaultCfg.Platform.ReadMetricsEnabled
	}
	if cfg.Platform.HealthProbes == nil {
		cfg.Platform.HealthProbes = defaultCfg.Platform.HealthProbes
	}
	if cfg.Platform.X.APIEnabled == nil {
		cfg.Platform.X.APIEnabled = defaultCfg.Platform.X.APIEnabled
	}
	if cfg.Platform.X.OEmbedEnabled == nil {
		cfg.Platform.X.OEmbedEnabled = defaultCfg.Platform.X.OEmbedEnabled
	}
	if cfg.Platform.X.AuthMode == "" {
		cfg.Platform.X.AuthMode = defaultCfg.Platform.X.AuthMode
	}
	if cfg.Platform.X.APIBaseURL == "" {
		cfg.Platform.X.APIBaseURL = defaultCfg.Platform.X.APIBaseURL
	}
	if cfg.Platform.X.TokenURL == "" {
		cfg.Platform.X.TokenURL = defaultCfg.Platform.X.TokenURL
	}
	if cfg.Platform.X.OEmbedURL == "" {
		cfg.Platform.X.OEmbedURL = defaultCfg.Platform.X.OEmbedURL
	}
	if cfg.Platform.X.ConsumerKey == "" {
		cfg.Platform.X.ConsumerKey = defaultCfg.Platform.X.ConsumerKey
	}
	if cfg.Platform.X.ConsumerKeySecret == "" {
		cfg.Platform.X.ConsumerKeySecret = defaultCfg.Platform.X.ConsumerKeySecret
	}
	if cfg.Platform.X.APITimeoutSec == 0 {
		cfg.Platform.X.APITimeoutSec = defaultCfg.Platform.X.APITimeoutSec
	}
	if cfg.Platform.X.APIMaxRetries == 0 {
		cfg.Platform.X.APIMaxRetries = defaultCfg.Platform.X.APIMaxRetries
	}
	if cfg.Platform.Reddit.APIEnabled == nil {
		cfg.Platform.Reddit.APIEnabled = defaultCfg.Platform.Reddit.APIEnabled
	}
	if cfg.Platform.Reddit.OAuthMode == "" {
		cfg.Platform.Reddit.OAuthMode = defaultCfg.Platform.Reddit.OAuthMode
	}
	if cfg.Platform.Reddit.APIBaseURL == "" {
		cfg.Platform.Reddit.APIBaseURL = defaultCfg.Platform.Reddit.APIBaseURL
	}
	if cfg.Platform.Reddit.TokenURL == "" {
		cfg.Platform.Reddit.TokenURL = defaultCfg.Platform.Reddit.TokenURL
	}
	if cfg.Platform.Reddit.UserAgent == "" {
		cfg.Platform.Reddit.UserAgent = defaultCfg.Platform.Reddit.UserAgent
	}
	if cfg.Platform.Reddit.APITimeoutSec == 0 {
		cfg.Platform.Reddit.APITimeoutSec = defaultCfg.Platform.Reddit.APITimeoutSec
	}
	if cfg.Platform.Reddit.APIMaxRetries == 0 {
		cfg.Platform.Reddit.APIMaxRetries = defaultCfg.Platform.Reddit.APIMaxRetries
	}
	if cfg.Platform.Reddit.RateLimit.RequestsPerMinute == 0 {
		cfg.Platform.Reddit.RateLimit.RequestsPerMinute = defaultCfg.Platform.Reddit.RateLimit.RequestsPerMinute
	}
	if cfg.Platform.Reddit.RateLimit.HeaderDriven == nil {
		cfg.Platform.Reddit.RateLimit.HeaderDriven = defaultCfg.Platform.Reddit.RateLimit.HeaderDriven
	}
	if cfg.Platform.Reddit.RateLimit.BackoffOn429 == nil {
		cfg.Platform.Reddit.RateLimit.BackoffOn429 = defaultCfg.Platform.Reddit.RateLimit.BackoffOn429
	}
	if cfg.Platform.Reddit.RateLimit.MinRetryAfterSec == 0 {
		cfg.Platform.Reddit.RateLimit.MinRetryAfterSec = defaultCfg.Platform.Reddit.RateLimit.MinRetryAfterSec
	}
	if cfg.Platform.Reddit.Comments.DefaultLimit == 0 {
		cfg.Platform.Reddit.Comments.DefaultLimit = defaultCfg.Platform.Reddit.Comments.DefaultLimit
	}
	if cfg.Platform.Reddit.Comments.DefaultDepth == 0 {
		cfg.Platform.Reddit.Comments.DefaultDepth = defaultCfg.Platform.Reddit.Comments.DefaultDepth
	}
	if cfg.Platform.Reddit.Comments.DefaultSort == "" {
		cfg.Platform.Reddit.Comments.DefaultSort = defaultCfg.Platform.Reddit.Comments.DefaultSort
	}
	if cfg.Platform.Reddit.Comments.ExpandMore == nil {
		cfg.Platform.Reddit.Comments.ExpandMore = defaultCfg.Platform.Reddit.Comments.ExpandMore
	}
	if cfg.Platform.Reddit.BrowserFallback.Enabled == nil {
		cfg.Platform.Reddit.BrowserFallback.Enabled = defaultCfg.Platform.Reddit.BrowserFallback.Enabled
	}
	if cfg.Platform.Reddit.BrowserFallback.AllowWithoutOAuth == nil {
		cfg.Platform.Reddit.BrowserFallback.AllowWithoutOAuth = defaultCfg.Platform.Reddit.BrowserFallback.AllowWithoutOAuth
	}
	if cfg.Social.Threads.AuthMode == "" {
		cfg.Social.Threads.AuthMode = defaultCfg.Social.Threads.AuthMode
	}
	if cfg.Social.Threads.BrowserFallback == nil {
		cfg.Social.Threads.BrowserFallback = defaultCfg.Social.Threads.BrowserFallback
	}
	if cfg.Social.Threads.RequestTimeoutSec == 0 {
		cfg.Social.Threads.RequestTimeoutSec = defaultCfg.Social.Threads.RequestTimeoutSec
	}
	if cfg.Social.Threads.RateLimitMs == 0 {
		cfg.Social.Threads.RateLimitMs = defaultCfg.Social.Threads.RateLimitMs
	}
	if cfg.Social.Instagram.AuthMode == "" {
		cfg.Social.Instagram.AuthMode = defaultCfg.Social.Instagram.AuthMode
	}
	if cfg.Social.Instagram.BrowserFallback == nil {
		cfg.Social.Instagram.BrowserFallback = defaultCfg.Social.Instagram.BrowserFallback
	}
	if cfg.Social.Instagram.RequestTimeoutSec == 0 {
		cfg.Social.Instagram.RequestTimeoutSec = defaultCfg.Social.Instagram.RequestTimeoutSec
	}
	if cfg.Social.Instagram.RateLimitMs == 0 {
		cfg.Social.Instagram.RateLimitMs = defaultCfg.Social.Instagram.RateLimitMs
	}
	if cfg.OCR.Enabled == nil {
		cfg.OCR.Enabled = defaultCfg.OCR.Enabled
	}
	if cfg.OCR.Provider == "" {
		cfg.OCR.Provider = defaultCfg.OCR.Provider
	}
	if cfg.OCR.BaseURL == "" {
		cfg.OCR.BaseURL = defaultCfg.OCR.BaseURL
	}
	if cfg.OCR.Model == "" {
		cfg.OCR.Model = defaultCfg.OCR.Model
	}
	if cfg.OCR.Prompt == "" {
		cfg.OCR.Prompt = defaultCfg.OCR.Prompt
	}
	if cfg.OCR.TimeoutSec == 0 {
		cfg.OCR.TimeoutSec = defaultCfg.OCR.TimeoutSec
	}
	if cfg.OCR.MaxImages == 0 {
		cfg.OCR.MaxImages = defaultCfg.OCR.MaxImages
	}
	if cfg.OCR.MaxImageBytes == 0 {
		cfg.OCR.MaxImageBytes = defaultCfg.OCR.MaxImageBytes
	}
	if cfg.OCR.MaxTokens == 0 {
		cfg.OCR.MaxTokens = defaultCfg.OCR.MaxTokens
	}
	if cfg.OCR.AppendToMarkdown == nil {
		cfg.OCR.AppendToMarkdown = defaultCfg.OCR.AppendToMarkdown
	}
	if cfg.OCR.AppendToText == nil {
		cfg.OCR.AppendToText = defaultCfg.OCR.AppendToText
	}
	if cfg.OCR.VideoFrames.IntervalSec == 0 {
		cfg.OCR.VideoFrames.IntervalSec = defaultCfg.OCR.VideoFrames.IntervalSec
	}
	if cfg.OCR.VideoFrames.MaxFrames == 0 {
		cfg.OCR.VideoFrames.MaxFrames = defaultCfg.OCR.VideoFrames.MaxFrames
	}
	if cfg.ASR.Enabled == nil {
		cfg.ASR.Enabled = defaultCfg.ASR.Enabled
	}
	if cfg.ASR.Provider == "" {
		cfg.ASR.Provider = defaultCfg.ASR.Provider
	}
	if cfg.ASR.BaseURL == "" {
		cfg.ASR.BaseURL = defaultCfg.ASR.BaseURL
	}
	// Leave Model empty so ModelOrDefault can choose a provider-aware default.
	// For api this is whisper-1; for local this is small.
	if cfg.ASR.ResponseFormat == "" {
		cfg.ASR.ResponseFormat = defaultCfg.ASR.ResponseFormat
	}
	if cfg.ASR.TimeoutSec == 0 {
		cfg.ASR.TimeoutSec = defaultCfg.ASR.TimeoutSec
	}
	if cfg.ASR.MaxTimeoutSec == 0 {
		cfg.ASR.MaxTimeoutSec = defaultCfg.ASR.MaxTimeoutSec
	}
	if cfg.ASR.FFmpegPath == "" {
		cfg.ASR.FFmpegPath = defaultCfg.ASR.FFmpegPath
	}
	if cfg.ASR.AudioSampleRate == 0 {
		cfg.ASR.AudioSampleRate = defaultCfg.ASR.AudioSampleRate
	}
	if cfg.ASR.MaxAudioBytes == 0 {
		cfg.ASR.MaxAudioBytes = defaultCfg.ASR.MaxAudioBytes
	}
	if cfg.ASR.LocalCommand == "" {
		cfg.ASR.LocalCommand = defaultCfg.ASR.LocalCommand
	}
	if cfg.Watchlist.DefaultInterval == "" {
		cfg.Watchlist.DefaultInterval = defaultCfg.Watchlist.DefaultInterval
	}
	if cfg.Watchlist.Scheduler.TickInterval == "" {
		cfg.Watchlist.Scheduler.TickInterval = defaultCfg.Watchlist.Scheduler.TickInterval
	}
	if cfg.Watchlist.MaxItemsPerRun == 0 {
		cfg.Watchlist.MaxItemsPerRun = defaultCfg.Watchlist.MaxItemsPerRun
	}
	if cfg.Watchlist.LockTTL == "" {
		cfg.Watchlist.LockTTL = defaultCfg.Watchlist.LockTTL
	}
	if cfg.Watchlist.Persistence.Driver == "" {
		cfg.Watchlist.Persistence.Driver = defaultCfg.Watchlist.Persistence.Driver
	}
	if cfg.Watchlist.Persistence.SQLitePath == "" {
		cfg.Watchlist.Persistence.SQLitePath = cfg.Watchlist.DBPathOrDefault(cfg.Collect.DBPathOrDefault())
	}
	if cfg.Watchlist.DBPath == "" {
		cfg.Watchlist.DBPath = cfg.Watchlist.StateDBPathOrDefault(cfg.Collect.DBPathOrDefault())
	}
	if cfg.Summary.Provider == "" {
		cfg.Summary.Provider = defaultCfg.Summary.Provider
	}
	if cfg.Summary.TimeoutSec == 0 {
		cfg.Summary.TimeoutSec = defaultCfg.Summary.TimeoutSec
	}
	if cfg.Summary.MaxInputChars == 0 {
		cfg.Summary.MaxInputChars = defaultCfg.Summary.MaxInputChars
	}
	if cfg.Summary.ImportanceThreshold == "" {
		cfg.Summary.ImportanceThreshold = defaultCfg.Summary.ImportanceThreshold
	}
	if cfg.Summary.FallbackProvider == "" {
		cfg.Summary.FallbackProvider = defaultCfg.Summary.FallbackProvider
	}
	if cfg.Summary.Mode == "" {
		cfg.Summary.Mode = defaultCfg.Summary.Mode
	}
	if cfg.Summary.InputBudget.TextChars == 0 {
		cfg.Summary.InputBudget.TextChars = defaultCfg.Summary.InputBudget.TextChars
	}
	if cfg.Summary.InputBudget.ASRChars == 0 {
		cfg.Summary.InputBudget.ASRChars = defaultCfg.Summary.InputBudget.ASRChars
	}
	if cfg.Summary.InputBudget.OCRChars == 0 {
		cfg.Summary.InputBudget.OCRChars = defaultCfg.Summary.InputBudget.OCRChars
	}
	if cfg.Summary.InputBudget.MetadataChars == 0 {
		cfg.Summary.InputBudget.MetadataChars = defaultCfg.Summary.InputBudget.MetadataChars
	}
	if cfg.Summary.DigFlow.RuntimeMode == "" {
		cfg.Summary.DigFlow.RuntimeMode = defaultCfg.Summary.DigFlow.RuntimeMode
	}
	if cfg.Summary.DigFlow.RootDir == "" {
		cfg.Summary.DigFlow.RootDir = defaultCfg.Summary.DigFlow.RootDir
	}
	if cfg.Summary.DigFlow.ConfigPath == "" {
		cfg.Summary.DigFlow.ConfigPath = defaultCfg.Summary.DigFlow.ConfigPath
	}
	if cfg.Summary.DigFlow.GormConfigPath == "" {
		cfg.Summary.DigFlow.GormConfigPath = defaultCfg.Summary.DigFlow.GormConfigPath
	}
	if cfg.Summary.DigFlow.EinoConfigPath == "" {
		cfg.Summary.DigFlow.EinoConfigPath = defaultCfg.Summary.DigFlow.EinoConfigPath
	}
	if cfg.Summary.DigFlow.ManifestPath == "" {
		cfg.Summary.DigFlow.ManifestPath = defaultCfg.Summary.DigFlow.ManifestPath
	}
	if cfg.Summary.DigFlow.GlobalToolsPath == "" {
		cfg.Summary.DigFlow.GlobalToolsPath = defaultCfg.Summary.DigFlow.GlobalToolsPath
	}
	if cfg.Summary.DigFlow.GlobalSkillsDir == "" {
		cfg.Summary.DigFlow.GlobalSkillsDir = defaultCfg.Summary.DigFlow.GlobalSkillsDir
	}
	if cfg.Summary.DigFlow.DisableCheckpoint == nil {
		cfg.Summary.DigFlow.DisableCheckpoint = defaultCfg.Summary.DigFlow.DisableCheckpoint
	}
	if cfg.Summary.DigFlow.BaseURL == "" {
		cfg.Summary.DigFlow.BaseURL = defaultCfg.Summary.DigFlow.BaseURL
	}
	if cfg.Summary.DigFlow.Endpoint == "" {
		cfg.Summary.DigFlow.Endpoint = defaultCfg.Summary.DigFlow.Endpoint
	}
	if cfg.Summary.DigFlow.Agent == "" {
		cfg.Summary.DigFlow.Agent = defaultCfg.Summary.DigFlow.Agent
	}
	if cfg.Summary.LLM.BaseURL == "" {
		cfg.Summary.LLM.BaseURL = defaultCfg.Summary.LLM.BaseURL
	}
	if cfg.Summary.LLM.Model == "" {
		cfg.Summary.LLM.Model = defaultCfg.Summary.LLM.Model
	}
}

// MergedAllowedDomains returns the unified gateway + local browser domain allowlist.
func (c *Config) MergedAllowedDomains() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, d := range append(c.Gateway.AllowedDomains, c.LocalBrowser.AllowedDomains...) {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}

// Save writes configuration to a YAML file.
func Save(path string, cfg *Config) error {
	mergeDefaults(cfg)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// EnableDefaultSkillASR enables the two opt-in switches required for local
// video ASR in the standard user Skill profile, persists them, and replaces
// the active process configuration with that profile. It deliberately leaves
// the chosen ASR provider and every credential untouched.
func EnableDefaultSkillASR() (*Config, error) {
	path := DefaultSkillProfilePath()
	// Stage the update before writing. defaultCfg is also the configuration
	// used by the current MCP runtime, so changing it before a failed write
	// would leave this session claiming an opt-in that was not persisted.
	skillMu.Lock()
	defer skillMu.Unlock()
	staged := defaultCfg.Skill
	staged.Media.DownloadMedia = true
	if strings.TrimSpace(staged.Media.Dir) == "" {
		staged.Media.Dir = filepath.Join(DefaultConfigDir(), "media")
	}
	enabled := true
	staged.ASR.Enabled = &enabled
	if err := updateSkillASROptIn(path, staged.Media.Dir); err != nil {
		return nil, err
	}
	defaultCfg.Skill = staged
	result := defaultCfg
	return &result, nil
}

// updateSkillASROptIn changes only the opt-in keys owned by this operation.
// In particular, it must not serialize the merged runtime config: that could
// rewrite unrelated user choices or persist values injected at runtime.
func updateSkillASROptIn(path, mediaDir string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("Skill profile must contain a YAML mapping")
	}
	root := document.Content[0]
	setYAMLBool(root, []string{"Skill", "Media", "DownloadMedia"}, true)
	setYAMLBool(root, []string{"Skill", "ASR", "Enabled"}, true)
	if strings.TrimSpace(yamlString(root, []string{"Skill", "Media", "Dir"})) == "" {
		setYAMLString(root, []string{"Skill", "Media", "Dir"}, mediaDir)
	}
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	return writeConfigAtomically(path, updated)
}

func setYAMLBool(root *yaml.Node, path []string, value bool) {
	setYAMLScalar(root, path, "!!bool", fmt.Sprintf("%t", value))
}

func setYAMLString(root *yaml.Node, path []string, value string) {
	setYAMLScalar(root, path, "!!str", value)
}

func setYAMLScalar(root *yaml.Node, path []string, tag, value string) {
	current := root
	for _, part := range path[:len(path)-1] {
		current = yamlMappingValue(current, part, true)
	}
	key := path[len(path)-1]
	for i := 0; i+1 < len(current.Content); i += 2 {
		if current.Content[i].Value == key {
			current.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
			return
		}
	}
	current.Content = append(current.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value},
	)
}

func yamlString(root *yaml.Node, path []string) string {
	current := root
	for _, part := range path {
		current = yamlMappingValue(current, part, false)
		if current == nil {
			return ""
		}
	}
	return current.Value
}

func yamlMappingValue(node *yaml.Node, key string, create bool) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	if !create {
		return nil
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child,
	)
	return child
}

func writeConfigAtomically(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".digcrawl-skill-*.yaml")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// IsDefaultSkillProfile reports whether a runtime was started from the
// standard user-owned Skill profile. The MCP configuration tool is limited to
// that profile and must never modify a repository or arbitrary --config file.
func IsDefaultSkillProfile(path string) bool {
	return normalizedConfigPath(path) == normalizedConfigPath(DefaultSkillProfilePath())
}

func normalizedConfigPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// DefaultConfigPath returns the default user config path. New user-level
// configuration follows the operating system convention instead of assuming a
// repository working directory.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.yaml")
}

// DefaultSkillProfilePath is the profile discovered by `digcrawl mcp` when
// --config is not supplied. Keeping it separate from the general configuration
// file preserves the existing explicit --config workflow for repository users.
func DefaultSkillProfilePath() string {
	return filepath.Join(DefaultConfigDir(), "digcrawl-skill.yaml")
}

// DefaultConfigDir returns the user-owned DigCrawl configuration directory.
func DefaultConfigDir() string {
	// XDG_CONFIG_HOME is an explicit user override. Honor it before choosing a
	// platform default so tests, portable installations, and cross-platform
	// automation can select one deterministic config root on every OS.
	if xdgDir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgDir != "" {
		return filepath.Join(xdgDir, "digcrawl")
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "DigCrawl")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "DigCrawl"
		}
		return filepath.Join(home, "AppData", "Roaming", "DigCrawl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "digcrawl")
	}
	return filepath.Join(home, ".config", "digcrawl")
}
