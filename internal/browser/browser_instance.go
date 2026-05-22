package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/originaleric/digcrawl/config"
)

// BrowserInstance 表示一个浏览器实例
type BrowserInstance struct {
	ID          string
	Name        string
	Browser     *rod.Browser
	ProfileDir  string
	CreatedAt   time.Time
	LastUsed    time.Time
	tabManager  *TabManager
	tabExecutor *TabExecutor
	cfg         config.LocalBrowserConfig
}

// InstanceManager 管理多个浏览器实例
type InstanceManager struct {
	mu        sync.RWMutex
	instances map[string]*BrowserInstance
	baseDir   string
	cfg       config.LocalBrowserConfig
}

var (
	instanceManagerOnce sync.Once
	globalInstanceMgr   *InstanceManager
)

// GetInstanceManager 获取全局实例管理器
func GetInstanceManager() *InstanceManager {
	instanceManagerOnce.Do(func() {
		cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
		baseDir := "storage/app/browser_instances"
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			// 如果创建失败，使用临时目录
			baseDir = os.TempDir()
		}
		globalInstanceMgr = &InstanceManager{
			instances: make(map[string]*BrowserInstance),
			baseDir:   baseDir,
			cfg:       cfg,
		}
	})
	return globalInstanceMgr
}

// CreateInstance 创建新的浏览器实例
func (im *InstanceManager) CreateInstance(ctx context.Context, name string) (*BrowserInstance, error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	// 生成实例ID
	instanceID := fmt.Sprintf("inst_%d", time.Now().UnixNano())
	if name == "" {
		name = instanceID
	}

	// 创建Profile目录
	profileDir := filepath.Join(im.baseDir, instanceID)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 Profile 目录失败: %w", err)
	}

	// 创建 TabExecutor
	executor := NewTabExecutor(im.cfg.MaxConcurrency)

	// 启动浏览器
	l := launcher.New().
		Context(ctx).
		NoSandbox(true).
		Headless(*im.cfg.Headless).
		Set(flags.Flag("disable-gpu")).
		UserDataDir(profileDir) // 设置用户数据目录
	if im.cfg.ChromePath != "" {
		l = l.Bin(im.cfg.ChromePath)
	}

	controlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("启动浏览器实例失败: %w", err)
	}

	browser := rod.New().
		Context(ctx).
		ControlURL(controlURL).
		Timeout(time.Duration(im.cfg.TotalTimeoutSec) * time.Second)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("连接浏览器实例失败: %w", err)
	}

	// 创建 TabManager
	tabTTL := time.Duration(im.cfg.TotalTimeoutSec*2) * time.Second
	tabManager := NewTabManager(browser, executor, im.cfg.MaxConcurrency*2, tabTTL)

	instance := &BrowserInstance{
		ID:          instanceID,
		Name:        name,
		Browser:     browser,
		ProfileDir:  profileDir,
		CreatedAt:   time.Now(),
		LastUsed:    time.Now(),
		tabManager:  tabManager,
		tabExecutor: executor,
		cfg:         im.cfg,
	}

	im.instances[instanceID] = instance
	return instance, nil
}

// GetInstance 获取指定实例
func (im *InstanceManager) GetInstance(instanceID string) (*BrowserInstance, error) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	instance, exists := im.instances[instanceID]
	if !exists {
		return nil, fmt.Errorf("实例 %s 不存在", instanceID)
	}

	// 检查浏览器是否仍然有效
	if _, err := instance.Browser.Version(); err != nil {
		// 浏览器已断开，需要重新连接或移除
		im.mu.RUnlock()
		im.mu.Lock()
		delete(im.instances, instanceID)
		im.mu.Unlock()
		return nil, fmt.Errorf("实例 %s 的浏览器已断开", instanceID)
	}

	instance.LastUsed = time.Now()
	return instance, nil
}

// ListInstances 列出所有实例
func (im *InstanceManager) ListInstances() []*BrowserInstance {
	im.mu.RLock()
	defer im.mu.RUnlock()

	instances := make([]*BrowserInstance, 0, len(im.instances))
	for _, instance := range im.instances {
		instances = append(instances, instance)
	}
	return instances
}

// StopInstance 停止指定实例
func (im *InstanceManager) StopInstance(instanceID string) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	instance, exists := im.instances[instanceID]
	if !exists {
		return fmt.Errorf("实例 %s 不存在", instanceID)
	}

	delete(im.instances, instanceID)

	// 关闭浏览器
	if instance.Browser != nil {
		_ = instance.Browser.Close()
	}

	return nil
}

// CleanupStaleInstances 清理过期的实例
func (im *InstanceManager) CleanupStaleInstances(maxAge time.Duration) int {
	if maxAge <= 0 {
		maxAge = 1 * time.Hour // 默认1小时
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	now := time.Now()
	var toDelete []string

	for id, instance := range im.instances {
		if now.Sub(instance.LastUsed) > maxAge {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		instance := im.instances[id]
		delete(im.instances, id)
		if instance.Browser != nil {
			_ = instance.Browser.Close()
		}
	}

	return len(toDelete)
}

// GetDefaultInstance 获取或创建默认实例
func (im *InstanceManager) GetDefaultInstance(ctx context.Context) (*BrowserInstance, error) {
	// 尝试获取默认实例
	im.mu.RLock()
	for _, instance := range im.instances {
		if instance.Name == "default" {
			im.mu.RUnlock()
			return im.GetInstance(instance.ID)
		}
	}
	im.mu.RUnlock()

	// 创建默认实例
	return im.CreateInstance(ctx, "default")
}

// InstanceStats 实例统计信息
type InstanceStats struct {
	TotalInstances int `json:"totalInstances"`
	MaxConcurrency int `json:"maxConcurrency"`
}

// Stats 返回实例管理器统计信息
func (im *InstanceManager) Stats() InstanceStats {
	im.mu.RLock()
	defer im.mu.RUnlock()

	return InstanceStats{
		TotalInstances: len(im.instances),
		MaxConcurrency: im.cfg.MaxConcurrency,
	}
}
