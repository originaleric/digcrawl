package browser

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-rod/rod"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/cdp"
)

func TestShutdownWaitsForOpeningAndCleansLateOwner(t *testing.T) {
	started, finish, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	c := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { close(closed); return nil })
	m := &browserManager{stopCh: make(chan struct{}), openFn: func(context.Context) (*browserConnection, error) { close(started); <-finish; return c, nil }}
	done := make(chan error, 1)
	go func() { _, err := m.acquireConnection(context.Background()); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.shutdownImmediately(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown=%v", err)
	}
	close(finish)
	if err := <-done; err == nil {
		t.Fatal("late opening returned a connection")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("late owner not cleaned")
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active != nil {
		t.Fatal("connection published after shutdown")
	}
	if err := m.shutdownImmediately(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type scriptedCDP struct {
	call   func(context.Context, string, any) ([]byte, error)
	events chan *cdp.Event
}

func (c *scriptedCDP) Event() <-chan *cdp.Event { return c.events }
func (c *scriptedCDP) Call(ctx context.Context, _ string, method string, params any) ([]byte, error) {
	return c.call(ctx, method, params)
}

func TestKnownTargetAttachmentFailureUsesFreshCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := false
	client := &scriptedCDP{events: make(chan *cdp.Event), call: func(ctx context.Context, method string, params any) ([]byte, error) {
		switch method {
		case "Target.createTarget":
			return []byte(`{"targetId":"owned-target"}`), nil
		case "Target.attachToTarget":
			cancel()
			return nil, context.Canceled
		case "Target.closeTarget":
			if ctx.Err() != nil {
				t.Fatal("target cleanup inherited canceled context")
			}
			data, _ := json.Marshal(params)
			if string(data) != `{"targetId":"owned-target"}` {
				t.Fatalf("wrong target: %s", data)
			}
			closed = true
		}
		return []byte(`{}`), nil
	}}
	b := rod.New().Context(ctx).ControlURL("").Client(client)
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	if _, err := createStealthPage(ctx, b); !errors.Is(err, context.Canceled) || !closed {
		t.Fatalf("err=%v closed=%v", err, closed)
	}
}

func TestUncertainTargetCreationRetiresOwnerAndReturnsSlot(t *testing.T) {
	closed := make(chan struct{})
	c := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { close(closed); return nil })
	c.browser = rod.New().ControlURL("").Client(&scriptedCDP{call: func(context.Context, string, any) ([]byte, error) { return nil, context.DeadlineExceeded }})
	m := &browserManager{active: c, stopCh: make(chan struct{}), slots: make(chan struct{}, 1), sessions: make(map[uint64]*browserSession)}
	if _, err := m.acquire(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire=%v", err)
	}
	<-closed
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.slots) != 0 || len(m.sessions) != 0 || c.leases != 0 || !c.retired {
		t.Fatal("uncertain target owner retained resources")
	}
}

func TestProfileEntriesReplacedDuringOwnerCheckArePreserved(t *testing.T) {
	profile := t.TempDir()
	host, _ := os.Hostname()
	lock := filepath.Join(profile, "SingletonLock")
	cookie := filepath.Join(profile, "SingletonCookie")
	if err := os.Symlink(host+"-12345", lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("old-cookie", cookie); err != nil {
		t.Fatal(err)
	}
	_, err := prepareInteractiveLoginProfile(profile, func(int) (bool, error) {
		if e := os.Rename(cookie, cookie+".old"); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink("replacement-cookie", cookie); e != nil {
			t.Fatal(e)
		}
		return false, nil
	})
	if err == nil {
		t.Fatal("replacement identity was not rejected")
	}
	if target, _ := os.Readlink(cookie); target != "replacement-cookie" {
		t.Fatal("replacement entry deleted")
	}
	if _, err = os.Lstat(lock); err != nil {
		t.Fatal("owner evidence deleted")
	}
}

type clearDeadlineFailureConn struct{ net.Conn }

func (c clearDeadlineFailureConn) SetWriteDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		return errors.New("cannot clear deadline")
	}
	return c.Conn.SetWriteDeadline(deadline)
}
func TestCannotClearWriteDeadlineClosesTransport(t *testing.T) {
	c, peer := net.Pipe()
	defer c.Close()
	defer peer.Close()
	go func() { var b [1]byte; _, _ = peer.Read(b[:]) }()
	var closed atomic.Bool
	w := &pipeWebSocket{conn: c}
	transport := &boundedCDPTransport{ws: w, conn: clearDeadlineFailureConn{c}, close: func() { closed.Store(true); _ = c.Close() }, sender: make(chan struct{}, 1), limit: time.Second}
	err := transport.Send([]byte{1})
	if !isConnectionFailure(err) || !closed.Load() {
		t.Fatalf("err=%v closed=%v", err, closed.Load())
	}
	if err = transport.Send([]byte{2}); err == nil {
		t.Fatal("closed transport reused")
	}
}

func TestCancelledSelectorFailureStopsFallbackAndKeepsCancellationAction(t *testing.T) {
	ctx, cancel := context.WithCancel(WithBrowseRetryContext(context.Background()))
	cancel()
	err := finishBrowseError(ctx, newBrowseError(ErrorBrowserSelectorTimeout, "wait_selector", "selector", context.DeadlineExceeded, true))
	var e *BrowseError
	if !errors.As(err, &e) || CanBrowseFallback(err) {
		t.Fatal("canceled operation permits selector fallback")
	}
	actions := e.Diagnostics["next_actions"].([]map[string]any)
	if actions[0]["type"] != "wait" || e.Diagnostics["reason"] != "cancelled" || e.Retryable {
		t.Fatalf("diagnostics=%v", e.Diagnostics)
	}
	localErr := finishBrowseError(WithBrowseRetryContext(context.Background()), newBrowseError(ErrorBrowserSelectorTimeout, "wait_selector", "selector", context.DeadlineExceeded, true))
	if !CanBrowseFallback(localErr) {
		t.Fatal("local selector timeout wrongly consumes all business fallbacks")
	}
}
func TestCancelledFirstWaiterDoesNotCancelSharedSetupOrLeaveLease(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	c := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	m := &browserManager{stopCh: make(chan struct{}), openFn: func(ctx context.Context) (*browserConnection, error) {
		close(started)
		<-finish
		if ctx.Err() != nil {
			t.Errorf("shared setup cancelled: %v", ctx.Err())
		}
		return c, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := m.acquireConnection(ctx); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait=%v", err)
	}
	close(finish)
	conn, err := m.acquireConnection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	leases := conn.leases
	m.mu.Unlock()
	if leases != 1 {
		t.Fatalf("leases=%d", leases)
	}
	m.releaseConnection(conn)
	if err = m.shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestQueuedHealthProbeDoesNotRetireConnection(t *testing.T) {
	c := newLifecycleTestConnection(func(context.Context) error { return &cdpSendError{operation: "send_queue", cause: errors.New("busy")} }, func(context.Context) error { t.Error("healthy connection closed"); return nil })
	m := &browserManager{active: c}
	if _, err := m.acquireConnection(context.Background()); err == nil {
		t.Fatal("missing local error")
	}
	if c.retired || c.leases != 0 || m.active != c {
		t.Fatal("local queue error changed ownership")
	}
}

type pipeWebSocket struct {
	conn      net.Conn
	readsDone chan struct{}
	writes    atomic.Int32
}

func (w *pipeWebSocket) Send(data []byte) error {
	w.writes.Add(1)
	_, err := w.conn.Write(data)
	return err
}
func (w *pipeWebSocket) Read() ([]byte, error) {
	defer close(w.readsDone)
	var b [1]byte
	_, err := w.conn.Read(b[:])
	return nil, err
}
func TestCDPSynchronousWriteHasRealBoundAfterContextExpires(t *testing.T) {
	c, peer := net.Pipe()
	defer peer.Close()
	defer c.Close()
	w := &pipeWebSocket{conn: c, readsDone: make(chan struct{})}
	transport := &boundedCDPTransport{ws: w, conn: c, close: func() { _ = c.Close() }, sender: make(chan struct{}, 1), limit: 50 * time.Millisecond}
	client := cdp.New().Start(transport)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := client.Call(ctx, "", "Browser.getVersion", nil)
	if err == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("write=%v elapsed=%v", err, time.Since(start))
	}
	select {
	case <-w.readsDone:
	case <-time.After(time.Second):
		t.Fatal("reader leaked")
	}
}
func TestCDPQueueExpiryDoesNotCloseOtherSender(t *testing.T) {
	c, peer := net.Pipe()
	defer c.Close()
	defer peer.Close()
	w := &pipeWebSocket{conn: c, readsDone: make(chan struct{})}
	var closed atomic.Bool
	transport := &boundedCDPTransport{ws: w, conn: c, close: func() { closed.Store(true) }, sender: make(chan struct{}, 1), limit: 20 * time.Millisecond}
	transport.sender <- struct{}{}
	err := transport.Send([]byte("waiting"))
	var send *cdpSendError
	if !errors.As(err, &send) || send.operation != "send_queue" || closed.Load() || w.writes.Load() != 0 {
		t.Fatalf("queue err=%v closed=%v writes=%d", err, closed.Load(), w.writes.Load())
	}
	<-transport.sender
	go func() { _, _ = io.Copy(io.Discard, peer) }()
	if err = transport.Send([]byte("healthy")); err != nil {
		t.Fatal(err)
	}
}
func TestProfileGuardCanonicalizesAliasesAndProtectsLiveOwner(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(profile, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(profile), "alias")
	if err := os.Symlink(profile, alias); err != nil {
		t.Skip(err)
	}
	lock, identity, err := AcquireProfileLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, other, err := AcquireProfileLock(alias); !errors.Is(err, ErrProfileInUse) || other != identity {
		t.Fatalf("alias=%s err=%v", other, err)
	}
	host, _ := os.Hostname()
	target := host + "-" + strconv.Itoa(os.Getpid())
	if err = os.Symlink(target, filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareInteractiveLoginProfile(profile, nil); !errors.Is(err, errInteractiveLoginInProgress) {
		t.Fatalf("live owner=%v", err)
	}
	if _, err = os.Readlink(filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal("live lock removed")
	}
}
func TestSharedBrowserRetryMarkAllowsOnlyOneFaultReplay(t *testing.T) {
	ctx := WithBrowseRetryContext(context.Background())
	c := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { return nil })
	m := &browserManager{}
	m.acquireSessionFn = func(context.Context) (*browserSession, error) { return &browserSession{conn: c}, nil }
	m.releaseSessionFn = func(*browserSession) error { return nil }
	var calls atomic.Int32
	runner := func(context.Context, *BrowserBrowseRequest, *browserSession) (*BrowserBrowseResponse, error) {
		calls.Add(1)
		return nil, newBrowseError(ErrorBrowserNavigationFailed, "navigate", "failure", errors.New("temporary navigation"), true)
	}
	for i := 0; i < 2; i++ {
		_, err := browserBrowseWithSessionRunner(ctx, &BrowserBrowseRequest{URL: "https://example.com"}, func() *browserManager { return m }, runner)
		var e *BrowseError
		if !errors.As(err, &e) || e.Retryable || e.ToolErrorDetails()["handled"] != true {
			t.Fatalf("error=%v", err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("two reads must share one replay, calls=%d", calls.Load())
	}
}

func TestProfilePermissionFailureNeverMeansDeadOwner(t *testing.T) {
	profile := t.TempDir()
	host, _ := os.Hostname()
	if err := os.Symlink(host+"-12345", filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	denied := errors.New("permission denied")
	cleared, err := prepareInteractiveLoginProfile(profile, func(int) (bool, error) { return false, denied })
	if cleared || !errors.Is(err, denied) {
		t.Fatalf("permission failure cleared=%v err=%v", cleared, err)
	}
	if _, err = os.Readlink(filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal("unverified owner lock deleted")
	}
}

func TestProfileOrphanEntriesCannotAuthorizeRecovery(t *testing.T) {
	profile := t.TempDir()
	if err := os.Symlink("unknown-socket", filepath.Join(profile, "SingletonSocket")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareInteractiveLoginProfile(profile, nil); err == nil {
		t.Fatal("orphan socket treated as dead owner")
	}
	if _, err := os.Lstat(filepath.Join(profile, "SingletonSocket")); err != nil {
		t.Fatal("unknown socket deleted")
	}
}

func TestExternalUnknownTargetRemainsQuarantined(t *testing.T) {
	client := &scriptedCDP{call: func(context.Context, string, any) ([]byte, error) { return nil, io.EOF }}
	c := newBrowserConnection(rod.New().ControlURL("").Client(client), nil, nil, false, "", 0)
	c.probeFn = func(context.Context) error { return nil }
	opens := 0
	m := &browserManager{active: c, stopCh: make(chan struct{}), slots: make(chan struct{}, 1), sessions: make(map[uint64]*browserSession), openFn: func(context.Context) (*browserConnection, error) { opens++; return c, nil }}
	_, err := m.acquire(context.Background())
	if browseCauseReason(err) != "external_target_cleanup_required" {
		t.Fatalf("uncertain target lost: %v", err)
	}
	<-c.stopped
	for i := 0; i < 2; i++ {
		if _, err = m.acquireConnection(context.Background()); browseCauseReason(err) != "external_target_cleanup_required" {
			t.Fatalf("quarantine removed: %v", err)
		}
	}
	if opens != 0 || len(m.slots) != 0 {
		t.Fatalf("opens=%d slots=%d", opens, len(m.slots))
	}
}
func TestKnownPageUsesTargetCloseWithoutClosingExternalBrowser(t *testing.T) {
	forced := false
	client := &scriptedCDP{events: make(chan *cdp.Event), call: func(ctx context.Context, method string, _ any) ([]byte, error) {
		switch method {
		case "Target.createTarget":
			return []byte(`{"targetId":"read-target"}`), nil
		case "Target.attachToTarget":
			return []byte(`{"sessionId":"session"}`), nil
		case "Target.getTargetInfo":
			return []byte(`{"targetInfo":{"targetId":"read-target","type":"page"}}`), nil
		case "Page.close":
			return nil, errors.New("renderer stuck")
		case "Target.closeTarget":
			if ctx.Err() != nil {
				t.Fatal("force close inherited request deadline")
			}
			forced = true
			return []byte(`{"success":true}`), nil
		case "Browser.close":
			t.Fatal("external browser was closed")
		}
		return []byte(`{}`), nil
	}}
	b := rod.New().ControlURL("").Client(client)
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	p, err := createStealthPage(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if err := closePageWithin(p, time.Second); err != nil || !forced {
		t.Fatalf("forced=%v err=%v", forced, err)
	}
}
func TestExternalUnreleasedKnownPageBlocksReplacement(t *testing.T) {
	c := newBrowserConnection(nil, nil, nil, false, "", 0)
	c.rememberUnreleasedTarget(errors.New("target close failed"))
	m := &browserManager{active: c, stopCh: make(chan struct{})}
	m.retireConnection(c)
	<-c.stopped
	if _, err := m.acquireConnection(context.Background()); browseCauseReason(err) != "external_target_cleanup_required" {
		t.Fatalf("failed target was discarded: %v", err)
	}
}

func TestImmediateExternalShutdownWaitsForTargetLease(t *testing.T) {
	closed := make(chan struct{})
	c := newLifecycleTestConnection(func(context.Context) error { return nil }, func(context.Context) error { close(closed); return nil })
	c.leases = 1
	m := &browserManager{active: c, stopCh: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.shutdownImmediately(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lease forgotten: %v", err)
	}
	select {
	case <-closed:
		t.Fatal("external transport closed with outstanding target")
	default:
	}
	m.releaseConnection(c)
	if err := m.shutdownImmediately(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTargetCloseMustBeAcknowledgedOrProvedAbsent(t *testing.T) {
	client := &scriptedCDP{events: make(chan *cdp.Event), call: func(_ context.Context, method string, _ any) ([]byte, error) {
		if method == "Target.closeTarget" {
			return []byte(`{"success":false}`), nil
		}
		if method == "Target.getTargets" {
			return []byte(`{"targetInfos":[{"targetId":"read-target","type":"page"},{"targetId":"user-target","type":"page"}]}`), nil
		}
		return []byte(`{}`), nil
	}}
	b := rod.New().ControlURL("").Client(client)
	if err := b.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := closeTargetWithin(b, "read-target", time.Second); err == nil {
		t.Fatal("unacknowledged close accepted while target still exists")
	}
}
