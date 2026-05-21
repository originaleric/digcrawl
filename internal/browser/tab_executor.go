package browser

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
)

// TabExecutor 提供安全的标签页级并发执行
//
// 每个标签页顺序执行任务（一次一个），但多个标签页可以并发执行，
// 受可配置的限制。这可以防止资源耗尽，同时在硬件允许的情况下启用并行性。
//
// 架构：
//
//	Tab1 ─── sequential actions ───►
//	Tab2 ─── sequential actions ───►  (concurrent across tabs)
//	Tab3 ─── sequential actions ───►
type TabExecutor struct {
	semaphore   chan struct{}          // 限制并发标签页执行数
	tabLocks    map[string]*sync.Mutex // 每个标签页的顺序执行锁
	mu          sync.Mutex             // 保护 tabLocks map
	maxParallel int
}

// NewTabExecutor 创建 TabExecutor，使用给定的并发限制
// 如果 maxParallel <= 0，使用 DefaultMaxParallel()
func NewTabExecutor(maxParallel int) *TabExecutor {
	if maxParallel <= 0 {
		maxParallel = DefaultMaxParallel()
	}
	return &TabExecutor{
		semaphore:   make(chan struct{}, maxParallel),
		tabLocks:    make(map[string]*sync.Mutex),
		maxParallel: maxParallel,
	}
}

// DefaultMaxParallel 返回基于可用CPU的安全默认值
// 上限为8，以防止在大型机器上资源耗尽
func DefaultMaxParallel() int {
	n := runtime.NumCPU() * 2
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// MaxParallel 返回配置的并发限制
func (te *TabExecutor) MaxParallel() int {
	return te.maxParallel
}

// tabMutex 返回每个标签页的互斥锁，如果需要则创建一个
func (te *TabExecutor) tabMutex(tabID string) *sync.Mutex {
	te.mu.Lock()
	defer te.mu.Unlock()
	m, ok := te.tabLocks[tabID]
	if !ok {
		m = &sync.Mutex{}
		te.tabLocks[tabID] = m
	}
	return m
}

// Execute 为给定标签页运行任务，确保：
//   - 每个标签页一次只运行一个任务（标签页内顺序执行）
//   - 最多 maxParallel 个标签页并发执行（全局信号量）
//   - 任务内的 panic 会被恢复并作为错误返回
//   - 尊重上下文取消/超时
//
// 任务函数接收传递给 Execute 的相同上下文。
// 调用者应使用 context.WithTimeout 来限制执行时间。
func (te *TabExecutor) Execute(ctx context.Context, tabID string, task func(ctx context.Context) error) error {
	if tabID == "" {
		return fmt.Errorf("tabID 不能为空")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 获取全局信号量（尊重上下文取消）
	select {
	case te.semaphore <- struct{}{}:
		defer func() { <-te.semaphore }()
	case <-ctx.Done():
		return fmt.Errorf("标签页 %s: 等待执行槽位: %w", tabID, ctx.Err())
	}

	// 获取每个标签页的锁以实现标签页内顺序执行
	tabMu := te.tabMutex(tabID)
	locked := make(chan struct{})
	go func() {
		tabMu.Lock()
		close(locked)
	}()

	select {
	case <-locked:
		defer tabMu.Unlock()
	case <-ctx.Done():
		// 如果等待每个标签页锁时超时，需要清理。
		// 启动一个 goroutine 等待锁并立即释放它。
		go func() {
			<-locked
			tabMu.Unlock()
		}()
		return fmt.Errorf("标签页 %s: 等待标签页锁: %w", tabID, ctx.Err())
	}

	// 执行任务并恢复 panic
	return te.safeRun(ctx, tabID, task)
}

// safeRun 执行任务并恢复 panic
func (te *TabExecutor) safeRun(ctx context.Context, tabID string, task func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("标签页 %s: panic: %v", tabID, r)
		}
	}()
	return task(ctx)
}

// RemoveTab 在标签页关闭时清理每个标签页的互斥锁
// 它首先从 map 中删除条目，然后获取旧的互斥锁
// 以等待任何正在进行的任务完成后再返回。
//
// 注意：删除后，对同一 tabID 的并发 Execute 调用
// 将通过 tabMutex() 创建一个新的互斥锁。这是有意的——
// 旧标签页正在被移除，具有相同 ID 的新标签页应该
// 从干净的状态开始。调用者（CloseTab, CleanStaleTabs）
// 确保在调用 RemoveTab 之前取消标签页上下文，因此
// 正在进行的任务会迅速退出。
func (te *TabExecutor) RemoveTab(tabID string) {
	te.mu.Lock()
	m, ok := te.tabLocks[tabID]
	if !ok {
		te.mu.Unlock()
		return
	}
	delete(te.tabLocks, tabID)
	te.mu.Unlock()

	// 等待任何持有此互斥锁的正在进行的任务完成。
	// 我们获取锁以阻塞，直到活动任务释放它，
	// 然后立即释放——之后互斥锁被孤立。
	m.Lock()
	defer m.Unlock()
}

// ActiveTabs 返回具有关联互斥锁的标签页数量
func (te *TabExecutor) ActiveTabs() int {
	te.mu.Lock()
	defer te.mu.Unlock()
	return len(te.tabLocks)
}

// ExecutorStats 执行统计信息
type ExecutorStats struct {
	MaxParallel   int `json:"maxParallel"`
	ActiveTabs    int `json:"activeTabs"`
	SemaphoreUsed int `json:"semaphoreUsed"`
	SemaphoreFree int `json:"semaphoreFree"`
}

// Stats 返回执行统计信息
func (te *TabExecutor) Stats() ExecutorStats {
	used := len(te.semaphore)
	return ExecutorStats{
		MaxParallel:   te.maxParallel,
		ActiveTabs:    te.ActiveTabs(),
		SemaphoreUsed: used,
		SemaphoreFree: te.maxParallel - used,
	}
}

// ExecuteWithTimeout 是一个便利包装器，创建超时上下文
func (te *TabExecutor) ExecuteWithTimeout(ctx context.Context, tabID string, timeout time.Duration, task func(ctx context.Context) error) error {
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return te.Execute(tCtx, tabID, task)
}
