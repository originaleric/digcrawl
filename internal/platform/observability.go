package platform

import (
	"log"
	"time"
)

// ReadMetrics 平台读取观测指标。
type ReadMetrics struct {
	Platform         string
	Channel          string // api | rss | browser
	ExtractionSource string
	APILatencyMs     int64
	FeedLatencyMs    int64
	BrowserLatencyMs int64
	RetryCount       int
}

// ApplyReadMetrics 将观测指标写入 platform_metadata。
func ApplyReadMetrics(meta map[string]any, m ReadMetrics) map[string]any {
	meta = EnsureMeta(meta)
	if m.Channel != "" {
		meta["read_channel"] = m.Channel
	}
	if m.ExtractionSource != "" {
		meta["extraction_source"] = m.ExtractionSource
	}
	if m.APILatencyMs > 0 {
		meta["api_latency_ms"] = m.APILatencyMs
	}
	if m.FeedLatencyMs > 0 {
		meta["feed_latency_ms"] = m.FeedLatencyMs
	}
	if m.BrowserLatencyMs > 0 {
		meta["browser_latency_ms"] = m.BrowserLatencyMs
	}
	if m.RetryCount > 0 {
		meta["retry_count"] = m.RetryCount
	}
	return meta
}

// LogReadMetrics 输出结构化读取日志。
func LogReadMetrics(m ReadMetrics) {
	log.Printf("[platform-read] platform=%s channel=%s source=%s api_ms=%d feed_ms=%d browser_ms=%d retries=%d",
		m.Platform,
		m.Channel,
		m.ExtractionSource,
		m.APILatencyMs,
		m.FeedLatencyMs,
		m.BrowserLatencyMs,
		m.RetryCount,
	)
}

// MergeContentMetrics 合并指标到 Content 并打日志。
func MergeContentMetrics(c *Content, m ReadMetrics) {
	if c == nil {
		return
	}
	c.PlatformMetadata = ApplyReadMetrics(c.PlatformMetadata, m)
	if ReadMetricsEnabled() {
		LogReadMetrics(m)
	}
}

// Timed 计时 helper。
func Timed(fn func() error) (elapsed time.Duration, err error) {
	start := time.Now()
	err = fn()
	return time.Since(start), err
}
