package browser

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/stealth"
	"github.com/originaleric/digcrawl/config"
)

type browserSession struct {
	id        uint64
	page      *rod.Page
	conn      *browserConnection
	startedAt time.Time
	released  atomic.Bool
}

type browserManager struct {
	mu  sync.Mutex
	cfg config.LocalBrowserConfig
	// active is the only connection allowed to receive new page leases. A
	// retired connection remains visible separately until every lease has been
	// returned and its owner-specific teardown has completed.
	active   *browserConnection
	retiring *browserConnection
	opening  chan struct{}
	// openFn is injected only by lifecycle tests. Keeping the state machine
	// independent from Chromium makes teardown/replacement invariants testable.
	openFn     func(context.Context) (*browserConnection, error)
	slots      chan struct{}
	sessions   map[uint64]*browserSession
	nextID     uint64
	stopCh     chan struct{}
	stopOnce   sync.Once
	cleanupRun sync.Once
}

var (
	browserManagerOnce sync.Once
	globalBrowserMgr   *browserManager
)

func getBrowserManager() *browserManager {
	browserManagerOnce.Do(func() {
		cfg := normalizeLocalBrowserConfig(config.Get().LocalBrowser)
		globalBrowserMgr = &browserManager{
			cfg:      cfg,
			slots:    make(chan struct{}, cfg.MaxConcurrency),
			sessions: make(map[uint64]*browserSession),
			stopCh:   make(chan struct{}),
		}
		globalBrowserMgr.startCleanupLoop()
	})
	return globalBrowserMgr
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
	// Playwright stores Chromium in a versioned cache directory. A path that was
	// valid at configuration time can therefore disappear after `playwright
	// install` or a dependency update. Treat a missing explicit path the same as
	// an unset one so the discovery order below can select a current executable.
	if !isExecutableFile(cfg.ChromePath) {
		cfg.ChromePath = discoverLocalChromePath()
	}
	if isHeadlessShellPath(cfg.ChromePath) {
		cfg.ExtraArgs = appendMissingArgs(cfg.ExtraArgs, headlessShellCompatibilityArgs()...)
	}
	return cfg
}

// CheckAvailable verifies that DigCrawl can locate an executable Chromium
// browser without launching it. It is used by the local doctor command so a
// profile check stays side-effect free.
func CheckAvailable(cfg config.LocalBrowserConfig) error {
	cfg = normalizeLocalBrowserConfig(cfg)
	if cfg.ChromePath == "" || !isExecutableFile(cfg.ChromePath) {
		return fmt.Errorf("no supported Chrome/Chromium executable found; set LocalBrowser.ChromePath or install Chrome/Chromium")
	}
	return nil
}

func discoverLocalChromePath() string {
	for _, key := range []string{"DIGCRAWL_CHROME_PATH", "CHROME_PATH", "CHROME_BIN"} {
		if path := strings.TrimSpace(os.Getenv(key)); isExecutableFile(path) {
			return path
		}
	}

	// A bundled Playwright headless shell is an ephemeral cache artifact. Prefer
	// an installed browser when available: it survives Playwright cache cleanup
	// and is less sensitive to version-specific launch flags on desktop hosts.
	if path := firstExecutablePath(localChromeCandidates(), playwrightHeadlessShellCandidates()); path != "" {
		return path
	}

	for _, name := range []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"chrome",
		"microsoft-edge",
		"brave-browser",
	} {
		if path, err := exec.LookPath(name); err == nil && isExecutableFile(path) {
			return path
		}
	}
	return ""
}

func firstExecutablePath(groups ...[]string) string {
	for _, paths := range groups {
		for _, path := range paths {
			if isExecutableFile(path) {
				return path
			}
		}
	}
	return ""
}

// discoverInteractiveChromePath finds a browser that can show a login window.
// The headless shell remains valid for crawling, but cannot satisfy an
// interactive login flow.
func discoverInteractiveChromePath(preferred string) string {
	if isExecutableFile(preferred) && !isHeadlessShellPath(preferred) {
		return strings.TrimSpace(preferred)
	}
	for _, key := range []string{"DIGCRAWL_CHROME_PATH", "CHROME_PATH", "CHROME_BIN"} {
		if path := strings.TrimSpace(os.Getenv(key)); isExecutableFile(path) && !isHeadlessShellPath(path) {
			return path
		}
	}
	for _, path := range localChromeCandidates() {
		if isExecutableFile(path) {
			return path
		}
	}
	for _, name := range []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"chrome",
		"microsoft-edge",
		"brave-browser",
	} {
		if path, err := exec.LookPath(name); err == nil && isExecutableFile(path) {
			return path
		}
	}
	return ""
}

func playwrightHeadlessShellCandidates() []string {
	roots := []string{}
	for _, key := range []string{"DIGCRAWL_PLAYWRIGHT_BROWSERS_PATH", "PLAYWRIGHT_BROWSERS_PATH"} {
		if root := strings.TrimSpace(os.Getenv(key)); root != "" {
			roots = append(roots, root)
		}
	}
	if home, _ := os.UserHomeDir(); home != "" {
		roots = append(roots,
			filepath.Join(home, "Library/Caches/ms-playwright"),
			filepath.Join(home, ".cache/ms-playwright"),
		)
	}
	seen := map[string]struct{}{}
	var candidates []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		var rel string
		switch runtime.GOOS {
		case "darwin":
			rel = "chromium_headless_shell-*/chrome-headless-shell-mac-*/chrome-headless-shell"
		case "windows":
			rel = "chromium_headless_shell-*/chrome-headless-shell-win*/chrome-headless-shell.exe"
		default:
			rel = "chromium_headless_shell-*/chrome-headless-shell-linux*/chrome-headless-shell"
		}
		matches, _ := filepath.Glob(filepath.Join(root, rel))
		sort.Strings(matches)
		for i := len(matches) - 1; i >= 0; i-- {
			if _, ok := seen[matches[i]]; ok {
				continue
			}
			seen[matches[i]] = struct{}{}
			candidates = append(candidates, matches[i])
		}
	}
	return candidates
}

func localChromeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		candidates := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
				filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium"),
				filepath.Join(home, "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
			)
		}
		return candidates
	case "windows":
		candidates := []string{}
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			base = strings.TrimSpace(base)
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "Google/Chrome/Application/chrome.exe"),
				filepath.Join(base, "Chromium/Application/chrome.exe"),
				filepath.Join(base, "Microsoft/Edge/Application/msedge.exe"),
				filepath.Join(base, "BraveSoftware/Brave-Browser/Application/brave.exe"),
			)
		}
		return candidates
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			"/usr/bin/microsoft-edge",
			"/usr/bin/brave-browser",
		}
	}
}

func isExecutableFile(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func isHeadlessShellPath(path string) bool {
	return strings.Contains(strings.ToLower(filepath.Base(strings.TrimSpace(path))), "chrome-headless-shell")
}

func headlessShellCompatibilityArgs() []string {
	args := []string{"--disable-dev-shm-usage"}
	if runtime.GOOS == "darwin" {
		args = append(args,
			"--single-process",
			"--no-zygote",
			"--disable-crash-reporter",
			"--disable-crashpad",
			"--disable-breakpad",
			"--disable-features=Crashpad",
		)
	}
	return args
}

func appendMissingArgs(existing []string, required ...string) []string {
	seen := map[string]struct{}{}
	for _, arg := range existing {
		seen[strings.TrimSpace(arg)] = struct{}{}
	}
	for _, arg := range required {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if _, ok := seen[arg]; ok {
			continue
		}
		existing = append(existing, arg)
		seen[arg] = struct{}{}
	}
	return existing
}

// browserProcessOwned reports whether this manager launched the Chromium
// process itself. An externally managed CDP browser is a durable service owned
// by the user; rod.Browser.Close sends Browser.close and must never be used for
// that connection.
func browserProcessOwned(cfg config.LocalBrowserConfig) bool {
	return cfg.CDPConnectExisting == nil || !*cfg.CDPConnectExisting
}

func (m *browserManager) acquire(ctx context.Context) (*browserSession, error) {
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("等待浏览器并发槽位超时: %w", ctx.Err())
	}

	conn, err := m.acquireConnection(ctx)
	if err != nil {
		<-m.slots
		return nil, err
	}

	page, err := stealth.Page(conn.browser)
	if err != nil {
		m.retireConnection(conn)
		m.releaseConnection(conn)
		conn, err = m.acquireConnection(ctx)
		if err != nil {
			<-m.slots
			return nil, err
		}
		page, err = stealth.Page(conn.browser)
		if err != nil {
			m.retireConnection(conn)
			m.releaseConnection(conn)
			<-m.slots
			return nil, fmt.Errorf("创建 stealth 页面失败: %w", err)
		}
	}

	id := atomic.AddUint64(&m.nextID, 1)
	s := &browserSession{
		id:        id,
		page:      page,
		conn:      conn,
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
		// A renderer crash can leave the CDP close command unanswered. Releasing
		// a pool slot must never inherit that stuck request and hold the caller
		// (or the next crawl) indefinitely.
		if err := closePageWithin(s.page, 3*time.Second); err != nil {
			// A page that cannot acknowledge close may keep renderer/CDP state
			// behind after its slot is returned. Retire the shared connection so
			// it drains safely instead of reusing an uncertain process forever.
			m.retireConnection(s.conn)
		}
	}

	m.mu.Lock()
	delete(m.sessions, s.id)
	m.mu.Unlock()
	m.releaseConnection(s.conn)

	select {
	case <-m.slots:
	default:
	}
}

func closePageWithin(page *rod.Page, timeout time.Duration) error {
	if page == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Rod binds CDP calls to the page context. Using a timed clone keeps a
	// renderer crash from blocking a pool release without leaving one goroutine
	// behind for every stuck Close call.
	return page.Context(ctx).Close()
}

// browserOperationContext keeps CDP setup and health checks bounded by both
// the caller's deadline and the configured browser limit. A shared browser
// must outlive a call; a shared *operation* must not.
func browserOperationContext(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 60 * time.Second
	}
	return context.WithTimeout(ctx, limit)
}

// connectCDPBrowser separates process ownership from transport ownership.
// Rod's Browser.Close sends Browser.close, which would terminate the persistent
// Chrome service. By retaining the WebSocket we can abort a slow setup or reset
// only DigCrawl's connection while leaving Chrome running.
func connectCDPBrowser(ctx context.Context, controlURL string, timeout time.Duration) (*rod.Browser, *cdp.WebSocket, error) {
	transport := &cdp.WebSocket{}
	dialer := &cancelableCDPDialer{}
	transport.Dialer = dialer
	if err := connectWebSocketWithin(ctx, transport, dialer, controlURL); err != nil {
		safeCloseCDPTransport(transport)
		return nil, nil, fmt.Errorf("连接 Chromium CDP 失败: %w", err)
	}
	browser := rod.New().
		Context(context.Background()).
		ControlURL("").
		Client(cdp.New().Start(transport)).
		Timeout(timeout)
	if err := connectBrowserWithin(ctx, browser, transport); err != nil {
		safeCloseCDPTransport(transport)
		return nil, nil, fmt.Errorf("连接 Chromium CDP 失败: %w", err)
	}
	return browser, transport, nil
}

// cancelableCDPDialer closes a connection that was established just as the
// caller timed out. rod's WebSocket handshake otherwise blocks in ReadResponse
// and does not observe the context after the TCP dial succeeds.
type cancelableCDPDialer struct {
	mu     sync.Mutex
	dialer net.Dialer
	conn   net.Conn
	closed bool
}

func (d *cancelableCDPDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		_ = conn.Close()
		return nil, context.Canceled
	}
	d.conn = conn
	return conn, nil
}

func (d *cancelableCDPDialer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.conn != nil {
		_ = d.conn.Close()
	}
}

func connectWebSocketWithin(ctx context.Context, transport *cdp.WebSocket, dialer *cancelableCDPDialer, controlURL string) error {
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx, controlURL, nil) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		dialer.Close()
		<-done // closing the tracked socket unblocks a stalled HTTP handshake
		return ctx.Err()
	}
}

// connectBrowserWithin makes Rod's post-handshake discovery cancellable even
// though the retained Browser deliberately uses a background lifetime context.
func connectBrowserWithin(ctx context.Context, browser *rod.Browser, transport *cdp.WebSocket) error {
	done := make(chan error, 1)
	go func() { done <- browser.Connect() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		safeCloseCDPTransport(transport)
		err := <-done
		if err != nil {
			return ctx.Err()
		}
		return ctx.Err()
	}
}

func safeCloseCDPTransport(transport *cdp.WebSocket) {
	if transport == nil {
		return
	}
	// cdp.WebSocket does not expose whether dialing reached a connection; Close
	// panics when it did not. Its transport is private to this package, so turn
	// that library edge case into a harmless best-effort cleanup.
	defer func() { _ = recover() }()
	_ = transport.Close()
}

func (m *browserManager) openBrowserConnection(ctx context.Context) (*browserConnection, error) {
	operationCtx, cancel := browserOperationContext(ctx, time.Duration(m.cfg.TotalTimeoutSec)*time.Second)
	defer cancel()
	owned := browserProcessOwned(m.cfg)
	var started *launcher.Launcher
	profileDir := strings.TrimSpace(m.cfg.UserDataDir)
	cdpPort := 0
	var controlURL string
	var err error
	if m.cfg.CDPConnectExisting != nil && *m.cfg.CDPConnectExisting {
		port := m.cfg.CDPPort
		if port <= 0 {
			port = 9222
		}
		u := fmt.Sprintf("http://127.0.0.1:%d", port)
		controlURL, err = launcher.ResolveURL(u)
		if err != nil {
			return nil, fmt.Errorf("连接已有 CDP 浏览器失败: %w", err)
		}
	} else {
		started = launcher.New().
			Context(operationCtx).
			NoSandbox(true).
			Headless(*m.cfg.Headless).
			Set(flags.Flag("disable-gpu"))
		if m.cfg.ChromePath != "" {
			started = started.Bin(m.cfg.ChromePath)
		}
		if profileDir != "" {
			if err := os.MkdirAll(profileDir, 0o755); err != nil {
				return nil, fmt.Errorf("创建 UserDataDir 失败: %w", err)
			}
			started = started.UserDataDir(profileDir)
		}
		if m.cfg.CDPMode != nil && *m.cfg.CDPMode {
			port := m.cfg.CDPPort
			if port <= 0 {
				port = 9222
			}
			cdpPort = port
			started = started.RemoteDebuggingPort(port)
		}
		if proxy := strings.TrimSpace(m.cfg.ProxyURL); proxy != "" {
			started = started.Proxy(proxy)
		}
		started = applyLauncherExtraArgs(started, m.cfg.ExtraArgs)
		controlURL, err = started.Launch()
	}
	if err != nil {
		return nil, fmt.Errorf("启动 Chromium 失败: %w", err)
	}
	browser, transport, err := connectCDPBrowser(operationCtx, controlURL, time.Duration(m.cfg.TotalTimeoutSec)*time.Second)
	if err != nil {
		if started != nil {
			started.Kill()
			if releaseErr := waitForBrowserResourceRelease(operationCtx, profileDir, cdpPort); releaseErr != nil {
				return newBrowserConnection(nil, nil, started, true, profileDir, cdpPort), fmt.Errorf("cleanup failed browser launch: %w", releaseErr)
			}
		}
		return nil, err
	}
	return newBrowserConnection(browser, transport, started, owned, profileDir, cdpPort), nil
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
