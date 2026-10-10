package browser

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
)

func TestRetiredConnectionDrainsBeforeReplacementLaunch(t *testing.T) {
	t.Parallel()
	firstStopped := make(chan struct{})
	allowFirstStop := make(chan struct{})
	first := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		close(firstStopped)
		<-allowFirstStop
		return nil
	})
	second := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	var opens atomic.Int32
	m := &browserManager{
		cfg: config.LocalBrowserConfig{TotalTimeoutSec: 1},
		openFn: func(context.Context) (*browserConnection, error) {
			opens.Add(1)
			return second, nil
		},
		active: first,
	}
	first.leases = 1
	m.retireConnection(first)

	result := make(chan *browserConnection, 1)
	failure := make(chan error, 1)
	go func() {
		conn, err := m.acquireConnection(context.Background())
		if err != nil {
			failure <- err
			return
		}
		result <- conn
	}()

	select {
	case <-firstStopped:
		t.Fatal("retired connection stopped before its final lease was released")
	case <-time.After(80 * time.Millisecond):
	}
	if opens.Load() != 0 {
		t.Fatalf("replacement opened before previous lease drained: %d", opens.Load())
	}

	m.releaseConnection(first)
	select {
	case <-firstStopped:
	case <-time.After(time.Second):
		t.Fatal("retired connection did not begin teardown after final release")
	}
	if opens.Load() != 0 {
		t.Fatalf("replacement opened before previous teardown completed: %d", opens.Load())
	}

	close(allowFirstStop)
	select {
	case err := <-failure:
		t.Fatalf("replacement acquire failed: %v", err)
	case conn := <-result:
		if conn != second {
			t.Fatal("acquire returned a connection other than the replacement")
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not open after previous teardown completed")
	}
	if opens.Load() != 1 {
		t.Fatalf("replacement launch count=%d want 1", opens.Load())
	}
}

func TestRetirementWaitHonorsCallerDeadline(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	first := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		<-block
		return nil
	})
	m := &browserManager{cfg: config.LocalBrowserConfig{TotalTimeoutSec: 1}, active: first}
	first.leases = 1
	m.retireConnection(first)
	m.releaseConnection(first)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := m.acquireConnection(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire error=%v, want deadline exceeded", err)
	}
	close(block)
}

func TestFailedRetirementRemainsQuarantined(t *testing.T) {
	t.Parallel()
	first := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		return errors.New("browser process is still alive")
	})
	second := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	var opens atomic.Int32
	m := &browserManager{
		cfg:    config.LocalBrowserConfig{TotalTimeoutSec: 1},
		active: first,
		openFn: func(context.Context) (*browserConnection, error) {
			opens.Add(1)
			return second, nil
		},
	}
	first.leases = 1
	m.retireConnection(first)
	m.releaseConnection(first)

	select {
	case <-first.stopped:
	case <-time.After(time.Second):
		t.Fatal("failed teardown did not finish")
	}
	_, err := m.acquireConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "previous browser teardown failed") {
		t.Fatalf("acquire error=%v, want retained teardown error", err)
	}
	if opens.Load() != 0 {
		t.Fatalf("replacement opened after failed teardown: %d", opens.Load())
	}
}

func TestFailedRetirementMayRecoverOnlyAfterReleaseVerification(t *testing.T) {
	t.Parallel()
	var released atomic.Bool
	first := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		return errors.New("graceful close timed out")
	})
	first.releaseFn = func(context.Context) error {
		if !released.Load() {
			return errors.New("owned resource remains in use")
		}
		return nil
	}
	second := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	var opens atomic.Int32
	m := &browserManager{
		cfg:    config.LocalBrowserConfig{TotalTimeoutSec: 1},
		active: first,
		openFn: func(context.Context) (*browserConnection, error) {
			opens.Add(1)
			return second, nil
		},
	}
	first.leases = 1
	m.retireConnection(first)
	m.releaseConnection(first)
	<-first.stopped

	_, err := m.acquireConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "resources remain unavailable") {
		t.Fatalf("acquire error=%v, want resource-release error", err)
	}
	if opens.Load() != 0 {
		t.Fatalf("replacement opened before release verification: %d", opens.Load())
	}

	released.Store(true)
	conn, err := m.acquireConnection(context.Background())
	if err != nil {
		t.Fatalf("acquire after release verification: %v", err)
	}
	if conn != second || opens.Load() != 1 {
		t.Fatalf("replacement conn=%p opens=%d", conn, opens.Load())
	}
}

func TestFailedOpenQuarantinesPartiallyOwnedBrowser(t *testing.T) {
	t.Parallel()
	allowStop := make(chan struct{})
	partial := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		<-allowStop
		return nil
	})
	replacement := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	var opens atomic.Int32
	m := &browserManager{
		cfg: config.LocalBrowserConfig{TotalTimeoutSec: 1},
		openFn: func(context.Context) (*browserConnection, error) {
			if opens.Add(1) == 1 {
				return partial, errors.New("CDP connection failed after launch")
			}
			return replacement, nil
		},
	}
	if _, err := m.acquireConnection(context.Background()); err == nil {
		t.Fatal("first acquire should report the failed launch")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := m.acquireConnection(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire while partial owner retires error=%v, want deadline exceeded", err)
	}
	if opens.Load() != 1 {
		t.Fatalf("replacement opened while partial owner was retiring: %d", opens.Load())
	}

	close(allowStop)
	conn, err := m.acquireConnection(context.Background())
	if err != nil {
		t.Fatalf("acquire after partial owner released: %v", err)
	}
	if conn != replacement || opens.Load() != 2 {
		t.Fatalf("replacement conn=%p opens=%d", conn, opens.Load())
	}
}

func TestManagerShutdownDrainsLeaseThenClosesOwnedConnection(t *testing.T) {
	closed := make(chan struct{})
	conn := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		close(closed)
		return nil
	})
	conn.leases = 1
	m := &browserManager{
		cfg:    config.LocalBrowserConfig{TotalTimeoutSec: 1},
		active: conn,
		stopCh: make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := m.shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown with active lease error=%v, want deadline exceeded", err)
	}
	select {
	case <-closed:
		t.Fatal("shutdown closed a browser with an active tool lease")
	default:
	}

	m.releaseConnection(conn)
	if err := m.shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown after lease drain: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not close the drained owned browser")
	}
}

func TestManagerShutdownImmediatelyClosesOwnedConnectionWithActiveLease(t *testing.T) {
	closed := make(chan struct{})
	conn := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error {
		close(closed)
		return nil
	})
	conn.owned = true
	conn.leases = 1
	m := &browserManager{
		cfg:    config.LocalBrowserConfig{TotalTimeoutSec: 1},
		active: conn,
		stopCh: make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.shutdownImmediately(ctx); err != nil {
		t.Fatalf("immediate shutdown: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("immediate shutdown did not close the active leased browser")
	}
	if conn.leases != 0 {
		t.Fatalf("remaining browser leases=%d, want 0", conn.leases)
	}
}

func TestWaitForProfileUnlock(t *testing.T) {
	t.Parallel()
	profile := t.TempDir()
	lock := filepath.Join(profile, "SingletonLock")
	if err := os.WriteFile(lock, []byte("locked"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := waitForProfileUnlock(ctx, profile); err == nil {
		t.Fatalf("unknown lock must be rejected, error=%v", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := waitForProfileUnlock(context.Background(), profile); err != nil {
		t.Fatalf("unlocked profile error=%v", err)
	}
}

func TestWaitForLocalTCPPortRelease(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := waitForLocalTCPPortRelease(ctx, port); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("occupied port error=%v, want deadline exceeded", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitForLocalTCPPortRelease(context.Background(), port); err != nil {
		t.Fatalf("released port error=%v", err)
	}
}
