package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds DigGate runtime settings.
type Config struct {
	Gateway   GatewayConfig   `yaml:"Gateway" json:"Gateway"`
	Collector CollectorConfig `yaml:"Collector" json:"Collector"`
	Providers ProvidersConfig `yaml:"Providers" json:"Providers"`
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

// ProvidersConfig toggles built-in example tools.
type ProvidersConfig struct {
	ExampleEnabled *bool `yaml:"ExampleEnabled" json:"ExampleEnabled,omitempty"`
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
				ListenAddr:         ":8787",
				InstanceID:         "diggate-local",
				ArtifactDir:        "storage/diggate/artifacts",
				ArtifactTTLMinutes: 60,
				ArtifactEnabled:    &enabled,
			},
			Collector: CollectorConfig{
				WSPath:               "/diggate/v1/collector/ws",
				HeartbeatIntervalSec: 30,
				PullIntervalSec:      0,
				ReconnectDelaySec:    5,
				PullBatchSize:        1,
				MaxConcurrentCalls:   1,
			},
			Providers: ProvidersConfig{
				ExampleEnabled: &enabled,
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
	if cfg.Providers.ExampleEnabled == nil {
		cfg.Providers.ExampleEnabled = defaultCfg.Providers.ExampleEnabled
	}
}
