package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
)

const (
	browserShutdownTimeout      = 12 * time.Second
	browserForcedReleaseTimeout = 3 * time.Second
)

// browserConnection is the ownership boundary for one usable CDP connection.
// A connection is either owned (DigCrawl launched the Chromium process) or
// attached (the CDP service owns it). Leases prevent an in-flight crawl from
// being torn down by another crawl's recovery path.
type browserConnection struct {
	browser       *rod.Browser
	transport     *cdp.WebSocket
	launcher      *launcher.Launcher
	owned         bool
	profileDir    string
	cdpPort       int
	profileLock   *ProfileLock
	ownerIdentity string

	probeFn   func(context.Context) error
	closeFn   func(context.Context) error
	releaseFn func(context.Context) error

	leases     int
	retired    bool
	stopping   bool
	stopped    chan struct{}
	stopErr    error
	cleanupMu  sync.Mutex
	cleanupErr error // unresolved targets on an attached CDP service
}

func newBrowserConnection(browser *rod.Browser, transport *cdp.WebSocket, started *launcher.Launcher, owned bool, profileDir string, cdpPort int) *browserConnection {
	c := &browserConnection{
		browser:    browser,
		transport:  transport,
		launcher:   started,
		owned:      owned,
		profileDir: profileDir,
		cdpPort:    cdpPort,
		stopped:    make(chan struct{}),
	}
	if started != nil && started.PID() > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c.ownerIdentity, _ = ownedProcessIdentity(ctx, started.PID())
		cancel()
	}
	c.probeFn = func(ctx context.Context) error {
		if browser == nil {
			return fmt.Errorf("browser connection is nil")
		}
		_, err := browser.Context(ctx).Version()
		return err
	}
	c.closeFn = c.closeResources
	c.releaseFn = c.waitForOwnedResourceRelease
	return c
}

func (c *browserConnection) probe(ctx context.Context) error {
	if c == nil || c.probeFn == nil {
		return fmt.Errorf("browser connection has no health probe")
	}
	return c.probeFn(ctx)
}

func (c *browserConnection) closeResources(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if !c.owned {
		safeCloseCDPTransport(c.transport)
		return c.externalCleanupError()
	}

	var closeErr error
	if c.browser != nil {
		done := make(chan error, 1)
		go func() { done <- c.browser.Context(ctx).Close() }()
		select {
		case closeErr = <-done:
		case <-ctx.Done():
			closeErr = ctx.Err()
		}
	}
	safeCloseCDPTransport(c.transport)
	// A failed startup has no connected Rod browser, but can still leave the
	// launcher-owned process holding a profile or fixed CDP port. Kill it before
	// entering the common release barrier.
	if c.browser == nil && c.launcher != nil {
		if err := killVerifiedLauncher(c); err != nil {
			return err
		}
	}

	// Browser.close is the graceful path. If it stalls, or if the persistent
	// profile remains locked after it returned, use the launcher that created
	// this process to reclaim only our own process group. Attached CDP browsers
	// never enter this branch.
	if closeErr != nil && c.launcher != nil {
		if err := killVerifiedLauncher(c); err != nil {
			return err
		}
	}
	if err := c.waitForOwnedResourceRelease(ctx); err != nil {
		if c.launcher != nil {
			if err := killVerifiedLauncher(c); err != nil {
				return err
			}
		}
		// The graceful wait may have consumed the shutdown context. Give the
		// forced kill a short, independent window to release the resources it
		// owned; a replacement must never race a still-live process.
		retryCtx, cancel := context.WithTimeout(context.Background(), browserForcedReleaseTimeout)
		retryErr := c.waitForOwnedResourceRelease(retryCtx)
		cancel()
		if retryErr != nil {
			return fmt.Errorf("wait for owned browser resource release: %w", retryErr)
		}
	}
	// The lifecycle boundary is process/profile release, not whether the
	// best-effort Browser.close RPC received its response. Once the owned
	// process has been reclaimed (or the unique temporary profile has no lock),
	// a replacement is safe and must not be permanently blocked by a transport
	// error from the already-dead browser.
	return nil
}

// waitForOwnedResourceRelease verifies every resource whose reuse can make a
// replacement attach to, or conflict with, an old owned Chromium process.
func (c *browserConnection) waitForOwnedResourceRelease(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if !c.owned {
		return c.externalCleanupError()
	}
	if err := waitForOwnedProcessRelease(ctx, c); err != nil {
		return err
	}
	err := waitForBrowserResourceRelease(ctx, c.profileDir, c.cdpPort)
	if err == nil && c.profileLock != nil {
		return c.profileLock.Close()
	}
	return err
}

func waitForBrowserResourceRelease(ctx context.Context, profileDir string, cdpPort int) error {
	if err := waitForProfileUnlock(ctx, profileDir); err != nil {
		return err
	}
	if cdpPort > 0 {
		return waitForLocalTCPPortRelease(ctx, cdpPort)
	}
	return nil
}

// waitForProfileUnlock is intentionally scoped to a configured persistent
// profile. Launcher-created temporary profiles are unique per process and
// cannot conflict with the replacement browser.
func waitForProfileUnlock(ctx context.Context, profileDir string) error {
	if profileDir == "" {
		return nil
	}
	locked := func() bool {
		for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
			if _, err := os.Lstat(filepath.Join(profileDir, name)); err == nil {
				return true
			} else if !os.IsNotExist(err) {
				return true
			}
		}
		return false
	}
	if !locked() {
		return nil
	}
	if _, err := prepareInteractiveLoginProfile(profileDir, nil); err == nil {
		return nil
	} else if !errors.Is(err, errInteractiveLoginInProgress) {
		return err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !locked() {
				return nil
			}
			if _, err := prepareInteractiveLoginProfile(profileDir, nil); err == nil {
				return nil
			} else if !errors.Is(err, errInteractiveLoginInProgress) {
				return err
			}
		}
	}
}

// waitForLocalTCPPortRelease checks bindability instead of connecting to the
// CDP endpoint. A bindable loopback port proves that no old process can own
// the fixed debugging address, even if that process has stopped answering
// HTTP while it is still shutting down.
func waitForLocalTCPPortRelease(ctx context.Context, port int) error {
	if port <= 0 {
		return nil
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	available := func() bool {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return false
		}
		_ = listener.Close()
		return true
	}
	if available() {
		return nil
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if available() {
				return nil
			}
		}
	}
}

// acquireConnection returns a leased connection. It is the sole place that
// transitions idle -> active, active -> retired, and retired -> replacement;
// callers cannot race a new launch with teardown of the previous owner.
func (m *browserManager) acquireConnection(ctx context.Context) (*browserConnection, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.mu.Lock()
		select {
		case <-m.stopCh:
			m.mu.Unlock()
			return nil, fmt.Errorf("browser manager stopped")
		default:
		}
		if conn := m.active; conn != nil {
			conn.leases++
			m.mu.Unlock()
			probeCtx, cancel := browserOperationContext(ctx, 2*time.Second)
			start := time.Now()
			err := conn.probe(probeCtx)
			recordBrowserStage(ctx, "health_probe", conn, time.Since(start), err)
			cancel()
			if err == nil && ctx.Err() == nil {
				return conn, nil
			}
			if isLocalBrowserFailure(ctx.Err(), err) {
				m.releaseConnection(conn)
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, err
			}
			m.retireConnection(conn)
			m.releaseConnection(conn)
			// Recovery and replay belong to BrowserBrowse, not an acquire loop.
			return nil, fmt.Errorf("health_probe: %w", err)
		}
		if retiring := m.retiring; retiring != nil {
			stopped := retiring.stopped
			m.mu.Unlock()
			select {
			case <-stopped:
			case <-ctx.Done():
				return nil, fmt.Errorf("wait_retirement: %w", ctx.Err())
			}
			if retiring.stopErr != nil {
				if retiring.releaseFn == nil {
					return nil, fmt.Errorf("previous browser teardown failed: %w", retiring.stopErr)
				}
				checkCtx, cancel := browserOperationContext(ctx, 2*time.Second)
				err := retiring.releaseFn(checkCtx)
				cancel()
				if err != nil {
					return nil, fmt.Errorf("previous browser teardown failed and resources remain unavailable: %w", err)
				}
				m.mu.Lock()
				if m.retiring == retiring {
					m.retiring = nil
				}
				m.mu.Unlock()
			}
			continue
		}
		opening := m.opening
		result := m.openingErr
		if opening == nil {
			opening = make(chan struct{})
			m.opening = opening
			result = new(error)
			m.openingErr = result
			setupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			m.openingCancel = cancel
			go m.finishOpening(setupCtx, cancel, opening, result)
		}
		m.mu.Unlock()
		select {
		case <-opening:
		case <-ctx.Done():
			return nil, fmt.Errorf("wait_opening: %w", ctx.Err())
		}
		if result != nil && *result != nil {
			return nil, *result
		}
	}
}

func (m *browserManager) finishOpening(ctx context.Context, cancel context.CancelFunc, opening chan struct{}, result *error) {
	defer cancel()
	open := m.openFn
	if open == nil {
		open = m.openBrowserConnection
	}
	start := time.Now()
	conn, err := open(ctx)
	recordBrowserStage(ctx, "setup", conn, time.Since(start), err)
	if err == nil && conn == nil {
		err = fmt.Errorf("browser opener returned nil connection")
	}
	if err == nil {
		err = ctx.Err()
	}
	m.mu.Lock()
	select {
	case <-m.stopCh:
		err = fmt.Errorf("browser manager stopped during setup")
	default:
	}
	if m.opening != opening {
		err = fmt.Errorf("browser opening replaced")
	}
	if err == nil {
		// Shared setup owns no caller lease. Each live waiter acquires its own.
		conn.leases = 0
		m.active = conn
	} else if conn != nil {
		conn.retired = true
		m.retiring = conn
		m.startConnectionStopLocked(conn)
	}
	*result = err
	m.opening = nil
	m.openingCancel = nil
	close(opening)
	m.mu.Unlock()
}

func (m *browserManager) retireConnection(conn *browserConnection) {
	if conn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != conn || conn.retired {
		return
	}
	m.active = nil
	m.retiring = conn
	conn.retired = true
	if conn.leases == 0 {
		m.startConnectionStopLocked(conn)
	}
}

func (m *browserManager) releaseConnection(conn *browserConnection) {
	if conn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn.leases <= 0 {
		return
	}
	conn.leases--
	if conn.retired && conn.leases == 0 {
		m.startConnectionStopLocked(conn)
	}
}

func (m *browserManager) startConnectionStopLocked(conn *browserConnection) {
	if conn.stopping {
		return
	}
	conn.stopping = true
	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), browserShutdownTimeout)
		var err error
		if conn.closeFn == nil {
			err = fmt.Errorf("browser connection has no teardown function")
		} else {
			err = conn.closeFn(ctx)
		}
		cancel()
		recordBrowserStage(context.Background(), "retirement", conn, time.Since(start), err)

		m.mu.Lock()
		conn.stopErr = err
		// A failed close remains quarantined. Only acquireConnection can clear
		// it after independently proving all owned resources were released.
		if err == nil && m.retiring == conn {
			m.retiring = nil
		}
		close(conn.stopped)
		m.mu.Unlock()
	}()
}

func (m *browserManager) shutdown(ctx context.Context) error            { return m.stop(ctx, false) }
func (m *browserManager) shutdownImmediately(ctx context.Context) error { return m.stop(ctx, true) }

func (m *browserManager) stop(ctx context.Context, immediate bool) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.stopCh == nil {
		m.stopCh = make(chan struct{})
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
	if m.openingCancel != nil {
		m.openingCancel()
	}
	opening := m.opening
	m.mu.Unlock()
	if opening != nil {
		select {
		case <-opening:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	conn := m.active
	if conn != nil {
		m.active = nil
		m.retiring = conn
		conn.retired = true
	} else {
		conn = m.retiring
	}
	if conn != nil {
		if immediate && conn.owned {
			conn.leases = 0
		}
		if conn.leases == 0 {
			m.startConnectionStopLocked(conn)
		}
	}
	m.mu.Unlock()
	if conn == nil {
		return nil
	}
	select {
	case <-conn.stopped:
		m.mu.Lock()
		err := conn.stopErr
		m.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newLifecycleTestConnection keeps the state machine directly testable without
// a real Chromium process. Production connections always use
// newBrowserConnection above.
func newLifecycleTestConnection(probe func(context.Context) error, closeFn func(context.Context) error) *browserConnection {
	return &browserConnection{probeFn: probe, closeFn: closeFn, stopped: make(chan struct{})}
}

// Attached services survive transport retirement. If an owned target cannot
// be proved closed, keep the existing retirement barrier closed until the
// operator cleans/restarts that service and restarts DigCrawl.
func (c *browserConnection) externalCleanupError() error {
	if c == nil || c.owned {
		return nil
	}
	c.cleanupMu.Lock()
	defer c.cleanupMu.Unlock()
	return c.cleanupErr
}
func (c *browserConnection) rememberUnreleasedTarget(err error) {
	if c == nil || c.owned || err == nil {
		return
	}
	c.cleanupMu.Lock()
	defer c.cleanupMu.Unlock()
	if c.cleanupErr == nil {
		c.cleanupErr = fmt.Errorf("external target cleanup required: %w", err)
	}
}
