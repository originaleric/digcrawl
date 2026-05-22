package platform

import (
	"time"

	"github.com/originaleric/digcrawl/config"
)

// BrowseRetryPolicy 从配置读取浏览器重试策略。
func BrowseRetryPolicy() RetryPolicy {
	cfg := config.Get()
	p := DefaultBrowseRetry
	if cfg == nil {
		return p
	}
	if cfg.Platform.BrowseRetryMaxAttempts > 0 {
		p.MaxAttempts = cfg.Platform.BrowseRetryMaxAttempts
	}
	if cfg.Platform.BrowseRetryDelayMs > 0 {
		p.Delay = time.Duration(cfg.Platform.BrowseRetryDelayMs) * time.Millisecond
	}
	return p
}

// ReadMetricsEnabled 是否输出 read 指标日志。
func ReadMetricsEnabled() bool {
	cfg := config.Get()
	if cfg == nil || cfg.Platform.ReadMetricsEnabled == nil {
		return true
	}
	return *cfg.Platform.ReadMetricsEnabled
}
