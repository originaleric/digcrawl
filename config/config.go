package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds DigGate runtime settings.
type Config struct {
	Gateway      GatewayConfig      `yaml:"Gateway" json:"Gateway"`
	Collector    CollectorConfig    `yaml:"Collector" json:"Collector"`
	Collect      CollectConfig      `yaml:"Collect" json:"Collect"`
	LocalBrowser LocalBrowserConfig `yaml:"LocalBrowser" json:"LocalBrowser"`
	Providers    ProvidersConfig    `yaml:"Providers" json:"Providers"`
}

// CollectConfig local content collect and export settings.
type CollectConfig struct {
	OutputDir         string   `yaml:"OutputDir" json:"OutputDir,omitempty"`
	InboxDir          string   `yaml:"InboxDir" json:"InboxDir,omitempty"`
	DefaultFormats    []string `yaml:"DefaultFormats" json:"DefaultFormats,omitempty"`
	IncludeMedia      *bool    `yaml:"IncludeMedia" json:"IncludeMedia,omitempty"`
	DownloadMedia     *bool    `yaml:"DownloadMedia" json:"DownloadMedia,omitempty"`
	IndexEnabled      *bool    `yaml:"IndexEnabled" json:"IndexEnabled,omitempty"`
	MaxMediaBytes     int64    `yaml:"MaxMediaBytes" json:"MaxMediaBytes,omitempty"`
	MediaTimeoutSec   int      `yaml:"MediaTimeoutSec" json:"MediaTimeoutSec,omitempty"`
	InboxPollInterval int      `yaml:"InboxPollIntervalSec" json:"InboxPollIntervalSec,omitempty"`
	RateLimitMs       int      `yaml:"RateLimitMs" json:"RateLimitMs,omitempty"`
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
				InstanceID: "diggate-local",
				AllowedTools: []string{
					"gateway.ping",
					"browser.browse",
					"browser.snapshot",
					"wechat.article.read",
					"xiaohongshu.note.read",
					"douyin.video.read",
					"x.post.read",
				},
				ArtifactDir:        "storage/diggate/artifacts",
				ArtifactTTLMinutes: 60,
				ArtifactEnabled:    &enabled,
				RequireApproval:    &enabled,
			},
			Collector: CollectorConfig{
				WSPath:               "/diggate/v1/collector/ws",
				HeartbeatIntervalSec: 30,
				PullIntervalSec:      0,
				ReconnectDelaySec:    5,
				PullBatchSize:        1,
				MaxConcurrentCalls:   1,
				AllowedTools: []string{
					"gateway.ping",
					"browser.browse",
					"browser.snapshot",
					"wechat.article.read",
					"xiaohongshu.note.read",
					"douyin.video.read",
					"x.post.read",
				},
			},
			Collect: CollectConfig{
				OutputDir:         "collect-output",
				InboxDir:          "collect-inbox",
				DefaultFormats:    []string{"md", "json"},
				IncludeMedia:      &enabled,
				DownloadMedia:     &enabled,
				IndexEnabled:      &enabled,
				MaxMediaBytes:     10 * 1024 * 1024,
				MediaTimeoutSec:   30,
				InboxPollInterval: 5,
				RateLimitMs:       1500,
			},
			LocalBrowser: LocalBrowserConfig{
				Enabled:                &enabled,
				MaxConcurrency:         2,
				TotalTimeoutSec:        90,
				NavigateTimeoutSec:     45,
				WaitSelectorTimeoutSec: 25,
				AllowedDomains: []string{
					"mp.weixin.qq.com", "weixin.qq.com",
					"xiaohongshu.com", "www.xiaohongshu.com", "xhslink.com",
					"douyin.com", "www.douyin.com", "iesdouyin.com",
					"x.com", "twitter.com", "www.twitter.com",
				},
				Headless:       &enabled,
				CookieStoreDir: "storage/diggate/browser_cookies",
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
	if len(cfg.LocalBrowser.AllowedDomains) == 0 {
		cfg.LocalBrowser.AllowedDomains = defaultCfg.LocalBrowser.AllowedDomains
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
	return filepath.Join(home, ".diggate", "config.yaml")
}
