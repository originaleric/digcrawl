package collector

import (
	"time"

	"github.com/originaleric/digcrawl/protocol"
)

// shouldRetryToolResult 判断 Collector 是否应对工具失败做有限重试。
func shouldRetryToolResult(result *protocol.ToolResult) bool {
	if result == nil || result.Status == "success" {
		return false
	}
	if result.Error == nil {
		return false
	}
	if handled, _ := result.Error.Details["handled"].(bool); handled {
		return false
	}
	if retryable, ok := result.Error.Details["retryable"].(bool); ok {
		return retryable
	}
	switch result.Error.Code {
	case "BROWSER_NAVIGATION_FAILED", "BROWSER_PAGE_LOAD_FAILED", "BROWSER_SELECTOR_TIMEOUT", "BROWSER_PAGE_CLOSED", "BROWSER_TIMEOUT":
		return true

	default:
		return false
	}
}

func retryDelay(opts Options, attempt int) {
	if attempt <= 0 || opts.RetryDelay <= 0 {
		return
	}
	time.Sleep(opts.RetryDelay)
}

// These handlers may perform media/OCR/ASR after browser acquisition. Never
// replay the entire handler, even when that later error has no browser marker.
func managedBrowserTool(name string) bool {
	switch name {
	case "browser.browse", "browser.snapshot", "browser.action", "browser.tabs", "browser.login",
		"wechat.article.read", "xiaohongshu.note.read", "douyin.video.read", "toutiao.article.read", "toutiao.video.read", "x.post.read", "threads.post.read", "instagram.content.read", "youtube.video.read", "bilibili.video.read", "kuaishou.video.read", "zhihu.article.read", "zhihu.content.read", "weibo.post.read", "weibo.article.read", "reddit.post.read", "reddit.comments.read":
		return true
	}
	return false
}
