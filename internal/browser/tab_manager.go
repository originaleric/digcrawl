package browser

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/stealth"
)

// TabSession 表示一个标签页会话
type TabSession struct {
	ID        string
	Page      *rod.Page
	URL       string
	CreatedAt time.Time
	LastUsed  time.Time
	executor  *TabExecutor
}

// TabManager 管理标签页的生命周期
type TabManager struct {
	mu       sync.RWMutex
	tabs     map[string]*TabSession
	browser  *rod.Browser
	executor *TabExecutor
	maxTabs  int
	tabTTL   time.Duration
}

// NewTabManager 创建新的标签页管理器
func NewTabManager(browser *rod.Browser, executor *TabExecutor, maxTabs int, tabTTL time.Duration) *TabManager {
	if maxTabs <= 0 {
		maxTabs = 10 // 默认最多10个标签页
	}
	if tabTTL <= 0 {
		tabTTL = 30 * time.Minute // 默认30分钟TTL
	}
	return &TabManager{
		tabs:     make(map[string]*TabSession),
		browser:  browser,
		executor: executor,
		maxTabs:  maxTabs,
		tabTTL:   tabTTL,
	}
}

// GetOrCreateTab 获取或创建标签页
// 如果 tabID 为空，创建新标签页
// 如果 tabID 存在，返回现有标签页
func (tm *TabManager) GetOrCreateTab(ctx context.Context, tabID string) (*TabSession, error) {
	if tabID == "" {
		// 创建新标签页
		return tm.createTab(ctx)
	}

	// 获取现有标签页
	tm.mu.RLock()
	tab, exists := tm.tabs[tabID]
	tm.mu.RUnlock()

	if !exists {
		// 标签页不存在，创建新的
		return tm.createTab(ctx)
	}

	// 检查标签页是否过期
	if time.Since(tab.LastUsed) > tm.tabTTL {
		tm.mu.Lock()
		delete(tm.tabs, tabID)
		tm.mu.Unlock()
		_ = tab.Page.Close()
		// 创建新标签页
		return tm.createTab(ctx)
	}

	// 更新最后使用时间
	tab.LastUsed = time.Now()
	return tab, nil
}

// createTab 创建新标签页
func (tm *TabManager) createTab(ctx context.Context) (*TabSession, error) {
	// 检查标签页数量限制
	tm.mu.Lock()
	if len(tm.tabs) >= tm.maxTabs {
		// 清理最旧的标签页
		tm.cleanupOldestTabLocked()
	}
	tm.mu.Unlock()

	// 创建新页面
	page, err := stealth.Page(tm.browser)
	if err != nil {
		return nil, fmt.Errorf("创建 stealth 页面失败: %w", err)
	}

	tabID := fmt.Sprintf("tab_%d", time.Now().UnixNano())
	tab := &TabSession{
		ID:        tabID,
		Page:      page,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		executor:  tm.executor,
	}

	tm.mu.Lock()
	tm.tabs[tabID] = tab
	tm.mu.Unlock()

	return tab, nil
}

// cleanupOldestTabLocked 清理最旧的标签页（需要在持有锁的情况下调用）
func (tm *TabManager) cleanupOldestTabLocked() {
	if len(tm.tabs) == 0 {
		return
	}

	var oldestTab *TabSession
	var oldestID string
	oldestTime := time.Now()

	for id, tab := range tm.tabs {
		if tab.LastUsed.Before(oldestTime) {
			oldestTime = tab.LastUsed
			oldestTab = tab
			oldestID = id
		}
	}

	if oldestTab != nil {
		delete(tm.tabs, oldestID)
		_ = oldestTab.Page.Close()
	}
}

// CloseTab 关闭指定标签页
func (tm *TabManager) CloseTab(tabID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tab, exists := tm.tabs[tabID]
	if !exists {
		return fmt.Errorf("标签页 %s 不存在", tabID)
	}

	delete(tm.tabs, tabID)
	if tm.executor != nil {
		tm.executor.RemoveTab(tabID)
	}
	return tab.Page.Close()
}

// GetTab 获取标签页（不创建）
func (tm *TabManager) GetTab(tabID string) (*TabSession, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	tab, exists := tm.tabs[tabID]
	return tab, exists
}

// ListTabs 列出所有标签页
func (tm *TabManager) ListTabs() []*TabSession {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tabs := make([]*TabSession, 0, len(tm.tabs))
	for _, tab := range tm.tabs {
		tabs = append(tabs, tab)
	}
	return tabs
}

// CleanupStaleTabs 清理过期标签页
func (tm *TabManager) CleanupStaleTabs() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	var toDelete []string

	for id, tab := range tm.tabs {
		if now.Sub(tab.LastUsed) > tm.tabTTL {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		tab := tm.tabs[id]
		delete(tm.tabs, id)
		if tm.executor != nil {
			tm.executor.RemoveTab(id)
		}
		_ = tab.Page.Close()
	}

	return len(toDelete)
}

// ExecuteOnTab 在指定标签页上执行任务（使用 TabExecutor）
func (tm *TabManager) ExecuteOnTab(ctx context.Context, tabID string, task func(ctx context.Context, tab *TabSession) error) error {
	if tm.executor == nil {
		// 如果没有 executor，直接执行
		tab, err := tm.GetOrCreateTab(ctx, tabID)
		if err != nil {
			return err
		}
		return task(ctx, tab)
	}

	return tm.executor.Execute(ctx, tabID, func(ctx context.Context) error {
		tab, err := tm.GetOrCreateTab(ctx, tabID)
		if err != nil {
			return err
		}
		return task(ctx, tab)
	})
}

// Stats 返回标签页统计信息
type TabManagerStats struct {
	TotalTabs     int           `json:"totalTabs"`
	MaxTabs       int           `json:"maxTabs"`
	TabTTL        time.Duration `json:"tabTTL"`
	ExecutorStats ExecutorStats `json:"executorStats,omitempty"`
}

func (tm *TabManager) Stats() TabManagerStats {
	tm.mu.RLock()
	totalTabs := len(tm.tabs)
	tm.mu.RUnlock()

	stats := TabManagerStats{
		TotalTabs: totalTabs,
		MaxTabs:   tm.maxTabs,
		TabTTL:    tm.tabTTL,
	}

	if tm.executor != nil {
		stats.ExecutorStats = tm.executor.Stats()
	}

	return stats
}
