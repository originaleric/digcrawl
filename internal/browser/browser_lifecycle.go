package browser

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	browser    *rod.Browser
	transport  *cdp.WebSocket
	launcher   *launcher.Launcher
	owned      bool
	profileDir string
	cdpPort    int

	probeFn   func(context.Context) error
	closeFn   func(context.Context) error
	releaseFn func(context.Context) error

	leases   int
	retired  bool
	stopping bool
	stopped  chan struct{}
	stopErr  error
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
		return nil
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
		c.launcher.Kill()
	}

	// Browser.close is the graceful path. If it stalls, or if the persistent
	// profile remains locked after it returned, use the launcher that created
	// this process to reclaim only our own process group. Attached CDP browsers
	// never enter this branch.
	if closeErr != nil && c.launcher != nil {
		c.launcher.Kill()
	}
	if err := c.waitForOwnedResourceRelease(ctx); err != nil {
		if c.launcher != nil {
			c.launcher.Kill()
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
	if c == nil || !c.owned {
		return nil
	}
	return waitForBrowserResourceRelease(ctx, c.profileDir, c.cdpPort)
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
		m.mu.Lock()
		if conn := m.active; conn != nil {
			conn.leases++
			m.mu.Unlock()
			probeCtx, cancel := browserOperationContext(ctx, time.Duration(m.cfg.TotalTimeoutSec)*time.Second)
			err := conn.probe(probeCtx)
			cancel()
			if err == nil {
				return conn, nil
			}
			m.retireConnection(conn)
			m.releaseConnection(conn)
			continue
		}
		if retiring := m.retiring; retiring != nil {
			stopped := retiring.stopped
			m.mu.Unlock()
			select {
			case <-stopped:
				if retiring.stopErr != nil {
					if retiring.releaseFn == nil {
						return nil, fmt.Errorf("previous browser teardown failed: %w", retiring.stopErr)
					}
					if err := retiring.releaseFn(ctx); err != nil {
						return nil, fmt.Errorf("previous browser teardown failed and resources remain unavailable: %w", err)
					}
					m.mu.Lock()
					if m.retiring == retiring {
						m.retiring = nil
					}
					m.mu.Unlock()
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		if opening := m.opening; opening != nil {
			m.mu.Unlock()
			select {
			case <-opening:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}

		opening := make(chan struct{})
		m.opening = opening
		m.mu.Unlock()
		open := m.openFn
		if open == nil {
			open = m.openBrowserConnection
		}
		conn, err := open(ctx)

		m.mu.Lock()
		m.opening = nil
		close(opening)
		if err == nil && conn == nil {
			err = fmt.Errorf("browser opener returned nil connection")
		}
		if err == nil {
			conn.leases = 1
			m.active = conn
		} else if conn != nil {
			// A launch can fail after Chromium has created its profile or bound a
			// fixed CDP port. Quarantine that partially initialized owner through
			// the same barrier used by normal retirement before any retry launches.
			conn.retired = true
			m.retiring = conn
			m.startConnectionStopLocked(conn)
		}
		m.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
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
		ctx, cancel := context.WithTimeout(context.Background(), browserShutdownTimeout)
		var err error
		if conn.closeFn == nil {
			err = fmt.Errorf("browser connection has no teardown function")
		} else {
			err = conn.closeFn(ctx)
		}
		cancel()

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

// newLifecycleTestConnection keeps the state machine directly testable without
// a real Chromium process. Production connections always use
// newBrowserConnection above.
func newLifecycleTestConnection(probe func(context.Context) error, closeFn func(context.Context) error) *browserConnection {
	return &browserConnection{probeFn: probe, closeFn: closeFn, stopped: make(chan struct{})}
}
