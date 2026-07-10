package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/stealth"
	"github.com/originaleric/digcrawl/config"
)

type browserSession struct {
	id        uint64
	page      *rod.Page
	startedAt time.Time
	released  atomic.Bool
}

type browserManager struct {
	mu         sync.Mutex
	cfg        config.LocalBrowserConfig
	browser    *rod.Browser
	slots      chan struct{}
	sessions   map[uint64]*browserSession
	nextID     uint64
	stopCh     chan struct{}
	stopOnce   sync.Once
	cleanupRun sync.Once
	// 新增：标签页管理
	tabManager  *TabManager
	tabExecutor *TabExecutor
}

var (
	browserManagerOnce sync.Once
	globalBrowserMgr   *browserManager
)

func getBrowserManager() *browserManager {
	browserManagerOnce.Do(func() {
		cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
		executor := NewTabExecutor(cfg.MaxConcurrency)
		globalBrowserMgr = &browserManager{
			cfg:         cfg,
			slots:       make(chan struct{}, cfg.MaxConcurrency),
			sessions:    make(map[uint64]*browserSession),
			stopCh:      make(chan struct{}),
			tabExecutor: executor,
		}
		globalBrowserMgr.startCleanupLoop()
	})
	return globalBrowserMgr
}

// getTabManager 获取或创建标签页管理器
func (m *browserManager) getTabManager() (*TabManager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tabManager != nil {
		return m.tabManager, nil
	}

	// 确保浏览器已初始化
	ctx := context.Background()
	browser, err := m.ensureBrowser(ctx)
	if err != nil {
		return nil, err
	}

	// 创建标签页管理器
	tabTTL := time.Duration(m.cfg.TotalTimeoutSec*2) * time.Second
	m.tabManager = NewTabManager(browser, m.tabExecutor, m.cfg.MaxConcurrency*2, tabTTL)
	return m.tabManager, nil
}

func normalizeLocalBrowserConfig(cfg config.LocalBrowserConfig) config.LocalBrowserConfig {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 3
	}
	if cfg.TotalTimeoutSec <= 0 {
		cfg.TotalTimeoutSec = 60
	}
	if cfg.NavigateTimeoutSec <= 0 {
		cfg.NavigateTimeoutSec = 30
	}
	if cfg.WaitSelectorTimeoutSec <= 0 {
		cfg.WaitSelectorTimeoutSec = 20
	}
	if cfg.CookieStoreDir == "" {
		cfg.CookieStoreDir = "storage/app/browser_cookies"
	}
	if cfg.Headless == nil {
		defaultHeadless := true
		cfg.Headless = &defaultHeadless
	}
	return cfg
}

func (m *browserManager) acquire(ctx context.Context) (*browserSession, error) {
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("等待浏览器并发槽位超时: %w", ctx.Err())
	}

	browser, err := m.ensureBrowser(ctx)
	if err != nil {
		<-m.slots
		return nil, err
	}

	page, err := stealth.Page(browser)
	if err != nil {
		_ = m.resetBrowser()
		browser, err = m.ensureBrowser(ctx)
		if err != nil {
			<-m.slots
			return nil, err
		}
		page, err = stealth.Page(browser)
		if err != nil {
			<-m.slots
			return nil, fmt.Errorf("创建 stealth 页面失败: %w", err)
		}
	}

	id := atomic.AddUint64(&m.nextID, 1)
	s := &browserSession{
		id:        id,
		page:      page,
		startedAt: time.Now(),
	}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

func (m *browserManager) release(s *browserSession) {
	if s == nil || !s.released.CompareAndSwap(false, true) {
		return
	}

	if s.page != nil {
		_ = s.page.Close()
	}

	m.mu.Lock()
	delete(m.sessions, s.id)
	m.mu.Unlock()

	select {
	case <-m.slots:
	default:
	}
}

func (m *browserManager) ensureBrowser(ctx context.Context) (*rod.Browser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser != nil {
		if _, err := m.browser.Version(); err == nil {
			return m.browser, nil
		}
		_ = m.browser.Close()
		m.browser = nil
	}

	controlURL, err := m.launchControlURL(ctx)
	if err != nil {
		return nil, err
	}

	browser := rod.New().
		Context(ctx).
		ControlURL(controlURL).
		Timeout(time.Duration(m.cfg.TotalTimeoutSec) * time.Second)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("连接 Chromium 失败: %w", err)
	}

	m.browser = browser
	return m.browser, nil
}

func (m *browserManager) launchControlURL(ctx context.Context) (string, error) {
	if m.cfg.CDPConnectExisting != nil && *m.cfg.CDPConnectExisting {
		port := m.cfg.CDPPort
		if port <= 0 {
			port = 9222
		}
		u := fmt.Sprintf("http://127.0.0.1:%d", port)
		ws, err := launcher.ResolveURL(u)
		if err != nil {
			return "", fmt.Errorf("连接已有 CDP 浏览器失败: %w", err)
		}
		return ws, nil
	}

	l := launcher.New().
		Context(ctx).
		NoSandbox(true).
		Headless(*m.cfg.Headless).
		Set(flags.Flag("disable-gpu"))
	if m.cfg.ChromePath != "" {
		l = l.Bin(m.cfg.ChromePath)
	}
	if dir := strings.TrimSpace(m.cfg.UserDataDir); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("创建 UserDataDir 失败: %w", err)
		}
		l = l.UserDataDir(dir)
	}
	if m.cfg.CDPMode != nil && *m.cfg.CDPMode {
		port := m.cfg.CDPPort
		if port <= 0 {
			port = 9222
		}
		l = l.RemoteDebuggingPort(port)
	}
	if proxy := strings.TrimSpace(m.cfg.ProxyURL); proxy != "" {
		l = l.Proxy(proxy)
	}
	l = applyLauncherExtraArgs(l, m.cfg.ExtraArgs)

	controlURL, err := l.Launch()
	if err != nil {
		return "", fmt.Errorf("启动 Chromium 失败: %w", err)
	}
	return controlURL, nil
}

// ProfileDirForPlatform returns persistent profile path for a platform login session.
func ProfileDirForPlatform(baseDir, platform string) string {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "storage/digcrawl/browser_profiles"
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "default"
	}
	return filepath.Join(baseDir, platform)
}

func (m *browserManager) resetBrowser() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.browser != nil {
		err := m.browser.Close()
		m.browser = nil
		return err
	}
	return nil
}

func (m *browserManager) startCleanupLoop() {
	m.cleanupRun.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					m.cleanupZombieSessions()
				case <-m.stopCh:
					return
				}
			}
		}()
	})
}

func (m *browserManager) cleanupZombieSessions() {
	m.mu.Lock()
	sessions := make([]*browserSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	timeout := time.Duration(m.cfg.TotalTimeoutSec*2) * time.Second
	m.mu.Unlock()

	for _, s := range sessions {
		if time.Since(s.startedAt) > timeout {
			m.release(s)
		}
	}
}
