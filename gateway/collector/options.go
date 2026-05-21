package collector

import (
	"strings"
	"time"

	diggatecfg "github.com/originaleric/diggate/config"
)

// Options configures the Collector client.
type Options struct {
	ServerURL            string
	WSPath               string
	Token                string
	InstanceID           string
	HeartbeatInterval    time.Duration
	PullInterval         time.Duration
	PullBatchSize        int
	ReconnectDelay       time.Duration
	MaxConcurrentCalls   int
}

// OptionsFromConfig builds collector options from app config.
func OptionsFromConfig(cfg *diggatecfg.Config) Options {
	c := cfg.Collector
	heartbeat := time.Duration(c.HeartbeatIntervalSec) * time.Second
	if heartbeat <= 0 {
		heartbeat = 30 * time.Second
	}
	pull := time.Duration(c.PullIntervalSec) * time.Second
	reconnect := time.Duration(c.ReconnectDelaySec) * time.Second
	if reconnect <= 0 {
		reconnect = 5 * time.Second
	}
	batch := c.PullBatchSize
	if batch <= 0 {
		batch = 1
	}
	maxConc := c.MaxConcurrentCalls
	if maxConc <= 0 {
		maxConc = 1
	}
	instanceID := strings.TrimSpace(c.InstanceID)
	if instanceID == "" {
		instanceID = cfg.Gateway.InstanceID
	}
	if instanceID == "" {
		instanceID = "diggate-collector"
	}
	wsPath := strings.TrimSpace(c.WSPath)
	if wsPath == "" {
		wsPath = "/diggate/v1/collector/ws"
	}
	return Options{
		ServerURL:          strings.TrimSpace(c.ServerURL),
		WSPath:             wsPath,
		Token:              strings.TrimSpace(c.Token),
		InstanceID:         instanceID,
		HeartbeatInterval:  heartbeat,
		PullInterval:       pull,
		PullBatchSize:      batch,
		ReconnectDelay:     reconnect,
		MaxConcurrentCalls: maxConc,
	}
}
