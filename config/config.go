package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds DigCrawl runtime settings.
type Config struct {
	Gateway      GatewayConfig      `yaml:"Gateway" json:"Gateway"`
	Collector    CollectorConfig    `yaml:"Collector" json:"Collector"`
	Collect      CollectConfig      `yaml:"Collect" json:"Collect"`
	LocalBrowser LocalBrowserConfig `yaml:"LocalBrowser" json:"LocalBrowser"`
	Platform     PlatformConfig     `yaml:"Platform" json:"Platform"`
	Providers    ProvidersConfig    `yaml:"Providers" json:"Providers"`
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
}

// PlatformConfig platform read/retry/health settings.
type PlatformConfig struct {
	BrowseRetryMaxAttempts int               `yaml:"BrowseRetryMaxAttempts" json:"BrowseRetryMaxAttempts,omitempty"`
	BrowseRetryDelayMs     int               `yaml:"BrowseRetryDelayMs" json:"BrowseRetryDelayMs,omitempty"`
	ReadMetricsEnabled     *bool             `yaml:"ReadMetricsEnabled" json:"ReadMetricsEnabled,omitempty"`
	HealthProbes           map[string]string `yaml:"HealthProbes" json:"HealthProbes,omitempty"`
}

// GatewayConfig HTTP Tool Gateway settings.
type GatewayConfig struct {
	Enabled            *bool    `yaml:"Enabled" json:"Enabled,omitempty"`
	ListenAddr         string   `yaml:"ListenAddr" json:"ListenAddr,omitempty"`
	InstanceID         string   `yaml:"InstanceID" json:"InstanceID,omitempty"`
	AuthToken          string   `yaml:"AuthToken" json:"AuthToken,omitempty"`
	AllowedTools       []string `yaml:"AllowedTools" json:"AllowedTools,omitempty"`
	AllowedReadPaths   []string `yaml:"AllowedReadPaths" json:"AllowedReadPaths,omitempty"`
	AllowedDomains     []string `yaml:"AllowedDomains" json:"AllowedDomains,omitempty"`
	ArtifactEnabled    *bool    `yaml:"ArtifactEnabled" json:"ArtifactEnabled,omitempty"`
	ArtifactDir        string   `yaml:"ArtifactDir" json:"ArtifactDir,omitempty"`
	ArtifactTTLMinutes int      `yaml:"ArtifactTTLMinutes" json:"ArtifactTTLMinutes,omitempty"`
	RequireApproval    *bool    `yaml:"RequireApproval" json:"RequireApproval,omitempty"`
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
	StubEnabled     *bool `yaml:"StubEnabled" json:"StubEnabled,omitempty"`
}

var (
	defaultCfg Config
	once       sync.Once
)

func init() {
	once.Do(func() {
		enabled := true
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
					"x.post.read",
				},
				ArtifactDir:        "storage/digcrawl/artifacts",
				ArtifactTTLMinutes: 60,
				ArtifactEnabled:    &enabled,
				RequireApproval:    &enabled,
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
					"x.post.read",
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
					"x.com", "twitter.com", "www.twitter.com",
				},
				Headless:       &enabled,
				CookieStoreDir: "storage/digcrawl/browser_cookies",
			},
			Platform: PlatformConfig{
				BrowseRetryMaxAttempts: 2,
				BrowseRetryDelayMs:     800,
				ReadMetricsEnabled:     &enabled,
				HealthProbes:           map[string]string{},
			},
			Providers: ProvidersConfig{
				ExampleEnabled:  &enabled,
				PlatformEnabled: &enabled,
				BrowserEnabled:  &enabled,
			},
		}
	})
}

// Get returns the default configuration (after Load or defaults).
func Get() *Config {
	return &defaultCfg
}

// Load reads YAML from path and replaces the process-wide default config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	mergeDefaults(&cfg)
	defaultCfg = cfg
	return &defaultCfg, nil
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
}

// MergedAllowedDomains returns gateway + local browser domain allowlists.
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

// DefaultConfigPath returns the default user config path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config/config.yaml"
	}
	return filepath.Join(home, ".digcrawl", "config.yaml")
}
