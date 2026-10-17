package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/originaleric/digcrawl/config"
	"strings"
	"sync"
	"time"
)

type browseRetryKey struct{}

// One in-memory mark is shared by all browser fallbacks in a tool invocation.
type browseRetryState struct {
	mu                sync.Mutex
	attempts, retries int
	retryUsed         bool
	deadline          time.Time
	recovery          string
	recoveryStatus    string
	firstFailure      map[string]any
	diagnostic        string
}

func WithBrowseRetryContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(browseRetryKey{}).(*browseRetryState); ok {
		return ctx
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	return context.WithValue(ctx, browseRetryKey{}, &browseRetryState{diagnostic: hex.EncodeToString(id[:]), recovery: "none", recoveryStatus: "not_attempted"})
}
func BrowseDiagnostics(ctx context.Context) map[string]any {
	s, ok := ctx.Value(browseRetryKey{}).(*browseRetryState)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fallbacks := s.attempts - s.retries - 1
	if fallbacks < 0 {
		fallbacks = 0
	}
	return map[string]any{"fallback_count": fallbacks, "handled": true, "attempts": s.attempts, "retry_count": s.retries, "max_retries": 1, "recovery_action": s.recovery, "recovery_status": s.recoveryStatus, "first_failure": s.firstFailure, "diagnostic_id": s.diagnostic}
}
func canRetryBrowse(ctx context.Context, err error) bool {
	if ctx.Err() != nil || err == nil {
		return false
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*time.Second {
		return false
	}
	var e *BrowseError
	if !errors.As(err, &e) || e.Diagnostics["replay_safe"] == false || browseCauseReason(e.Cause) == "external_target_cleanup_required" || isLocalBrowserFailure(nil, e.Cause) {
		return false
	}
	if isConnectionFailure(e.Cause) {
		return true
	}
	// Selector/content errors belong to finite business fallbacks.
	return e.Stage == "navigate" || e.Stage == "wait_load" || (e.Stage == "acquire" && browseCauseReason(e.Cause) == "timeout")
}
func finishBrowseError(ctx context.Context, err error) error {
	var e *BrowseError
	if !errors.As(err, &e) {
		return err
	}
	d := BrowseDiagnostics(ctx)
	for k, v := range e.Diagnostics {
		d[k] = v
	}
	d["retry_exhausted"] = d["retry_count"].(int) >= 1
	action := "run_doctor"
	if isLocalBrowserFailure(nil, e.Cause) {
		action = "wait"
	}
	if errors.Is(e.Cause, ErrProfileInUse) || errors.Is(e.Cause, errInteractiveLoginInProgress) {
		action = "wait"
	}
	if ctx.Err() != nil {
		action = "wait"
		d["reason"] = "budget_exhausted"
		if errors.Is(ctx.Err(), context.Canceled) {
			d["reason"] = "cancelled"
		}
	}
	instruction := "运行 doctor --config <实际 profile>，检查失败步骤和诊断编号后再读取"
	if action == "wait" {
		instruction = "等待当前任务结束；若 profile 被占用，先结束对应占用会话，再发起新的读取"
	}
	if ctx.Err() != nil {
		instruction = "本次请求已取消或总时间耗尽；确认仍需读取后再发起新的操作"
	}
	if ctx.Err() == nil && !isConnectionFailure(e.Cause) && !isLocalBrowserFailure(nil, e.Cause) && (e.Stage == "wait_selector" || e.Stage == "extract_content") {
		action = "retry_read"
		instruction = "核对链接与内容选择器，修正后再读取"
	}
	if ctx.Err() == nil && e.Diagnostics["reason"] == "page_changed" {
		action = "retry_read"
		instruction = "页面在提取期间发生切换；等待页面稳定后重新读取，本次混合内容已丢弃"
	}
	if browseCauseReason(e.Cause) == "external_target_cleanup_required" {
		action = "cleanup_external_browser"
		instruction = "清理本次读取遗留的外部 CDP 页面；无法确认时重启该外部浏览器，再重启 DigCrawl 后读取"
	}
	d["next_actions"] = []map[string]any{{"type": action, "executor": "user_or_agent", "precondition": instruction, "description": instruction}}
	e.Diagnostics = d
	e.Retryable = false // the sole browser owner already made this invocation's decision
	return e
}

// Only selector/content fallback is valid after the browser owner returned.
func CanBrowseFallback(err error) bool {
	var e *BrowseError
	return errors.As(err, &e) && e.Diagnostics["reason"] != "cancelled" && e.Diagnostics["reason"] != "budget_exhausted" && !errors.Is(e.Cause, context.Canceled) && e.Stage != "acquire" && !isConnectionFailure(e.Cause) && !isLocalBrowserFailure(nil, e.Cause) && (e.Stage == "wait_selector" || e.Stage == "extract_content")
}

// Both DOM reads and page-context API fetches share this invocation budget.
func browseInvocationContext(ctx context.Context) (context.Context, *browseRetryState, context.CancelFunc) {
	ctx = WithBrowseRetryContext(ctx)
	s := ctx.Value(browseRetryKey{}).(*browseRetryState)
	s.mu.Lock()
	if s.deadline.IsZero() {
		s.deadline = time.Now().Add(time.Duration(normalizeLocalBrowserConfig(config.Get().LocalBrowser).TotalTimeoutSec) * time.Second)
	}
	deadline := s.deadline
	s.mu.Unlock()
	ctx, cancel := context.WithDeadline(ctx, deadline)
	return ctx, s, cancel
}
func (s *browseRetryState) beginAttempt(replay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if replay {
		s.retries++
	}
}
func (s *browseRetryState) succeeded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retries > 0 {
		s.recoveryStatus = "succeeded"
	}
}
func (s *browseRetryState) claimRetry(ctx context.Context, err error, replay bool) bool {
	retry := canRetryBrowse(ctx, err)
	s.mu.Lock()
	defer s.mu.Unlock()
	if replay {
		s.recoveryStatus = "failed"
	}
	if !retry || s.retryUsed {
		return false
	}
	s.retryUsed = true
	s.recoveryStatus = "pending"
	var e *BrowseError
	if errors.As(err, &e) {
		if s.firstFailure == nil {
			s.firstFailure = map[string]any{"code": e.Code, "stage": e.Stage, "reason": browseCauseReason(e.Cause), "operation": e.Diagnostics["operation"]}
		}
		retiredOwner := e.Diagnostics["operation"] == "health_probe" || (e.Cause != nil && (strings.Contains(e.Cause.Error(), "create_target:") || strings.Contains(e.Cause.Error(), "close_target:")))
		if isConnectionFailure(err) || retiredOwner {
			s.recovery = "replace_connection"
		} else {
			s.recovery = "retry_read"
		}
	}
	return true
}
func (s *browseRetryState) waitRetry(ctx context.Context) bool {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		s.mu.Lock()
		s.recoveryStatus = "cancelled"
		s.mu.Unlock()
		return false
	}
}
