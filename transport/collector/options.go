package collector

import (
	"strings"
	"time"

	digcrawlcfg "github.com/originaleric/digcrawl/config"
)

// Options configures the Collector client.
type Options struct {
	ServerURL          string
	WSPath             string
	Token              string
	InstanceID         string
	HeartbeatInterval  time.Duration
	PullInterval       time.Duration
	PullBatchSize      int
	ReconnectDelay     time.Duration
	MaxConcurrentCalls int
	RateLimitCooldown  time.Duration
	CallQueueSize      int
	CallQueueWait      time.Duration
	RetryMaxAttempts   int
	RetryDelay         time.Duration
}

// OptionsFromConfig builds collector options from app config.
func OptionsFromConfig(cfg *digcrawlcfg.Config) Options {
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
	cooldownMs := c.RateLimitCooldownMs
	if cooldownMs <= 0 {
		cooldownMs = 1000
	}
	queueSize := c.CallQueueSize
	if queueSize <= 0 {
		queueSize = 32
	}
	queueWaitMs := c.CallQueueWaitMs
	if queueWaitMs <= 0 {
		queueWaitMs = 30000
	}
	retryMax := c.RetryMaxAttempts
	if retryMax <= 0 {
		retryMax = 1
	}
	retryDelayMs := c.RetryDelayMs
	if retryDelayMs <= 0 {
		retryDelayMs = 1500
	}
	instanceID := strings.TrimSpace(c.InstanceID)
	if instanceID == "" {
		instanceID = cfg.Gateway.InstanceID
	}
	if instanceID == "" {
		instanceID = "digcrawl-collector"
	}
	wsPath := strings.TrimSpace(c.WSPath)
	if wsPath == "" {
		wsPath = "/digcrawl/v1/collector/ws"
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
		RateLimitCooldown:  time.Duration(cooldownMs) * time.Millisecond,
		CallQueueSize:      queueSize,
		CallQueueWait:      time.Duration(queueWaitMs) * time.Millisecond,
		RetryMaxAttempts:   retryMax,
		RetryDelay:         time.Duration(retryDelayMs) * time.Millisecond,
	}
}
