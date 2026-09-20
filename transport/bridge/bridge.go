// Package bridge owns DigCrawl's local browser runtime for one user profile.
// Stdio MCP processes are deliberately thin clients: they share this bridge
// over a private Unix socket instead of each managing a browser or CDP port.
package bridge

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/originaleric/digcrawl/client"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
	httpgw "github.com/originaleric/digcrawl/transport/http"
)

const (
	DefaultIdleTimeout   = 15 * time.Minute
	startupTimeout       = 8 * time.Second
	staleStartLockAfter  = 15 * time.Second
	bridgeSocketDirName  = "bridge"
	bridgeSocketFileMode = 0o600
	startupStderrLimit   = 8 << 10
	// artifactFetchTimeout bounds the post-call Unix-socket transfer used to
	// inline an artifact into an MCP response. It intentionally does not share
	// a tool's potentially long execution budget: by this point the tool has
	// already completed and a stalled bridge must not keep the MCP response open.
	artifactFetchTimeout = 120 * time.Second
)

var ErrAlreadyRunning = errors.New("local DigCrawl bridge is already running")

// ErrBrowserProfileInUse means a different local bridge already owns the
// configured persistent Chrome profile. Sharing that browser while retaining
// separate configuration policies would make the first bridge's policy silently
// apply to the second profile, so callers must use separate UserDataDir values.
var ErrBrowserProfileInUse = errors.New("configured Chrome profile is already owned by another local DigCrawl bridge")

// Executor is the MCP-facing view of the one local bridge. It intentionally
// contains no browser state: reconnecting can start a new bridge before a
// call, but a failed call is never replayed because a tool may write local
// configuration or have another side effect.
type Executor struct {
	executable  string
	profile     string
	socket      string
	idleTimeout time.Duration
	manifest    protocol.ToolManifest
	cfg         *config.Config
	artifacts   artifact.Store
	// artifactTimeout is normally zero, which selects artifactFetchTimeout. It
	// exists so the local RPC boundary can be exercised with a short test budget
	// without weakening the production timeout.
	artifactTimeout time.Duration
}

// NewExecutor connects to the bridge for profile, starting it on demand when
// no healthy bridge owns that profile. The bridge socket is scoped by the real
// profile file rather than its spelling, so symlink aliases cannot create
// duplicate Bridge owners.
func NewExecutor(ctx context.Context, executable, profile string, cfg *config.Config, idleTimeout time.Duration) (*Executor, error) {
	if cfg == nil {
		return nil, errors.New("bridge requires a profile configuration")
	}
	profile, err := canonicalPath(profile)
	if err != nil {
		return nil, fmt.Errorf("resolve bridge profile: %w", err)
	}
	if strings.TrimSpace(executable) == "" {
		executable, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locate DigCrawl executable: %w", err)
		}
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve DigCrawl executable: %w", err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		return nil, err
	}
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	e := &Executor{executable: executable, profile: profile, socket: socket, idleTimeout: idleTimeout, cfg: cfg}
	manifest, err := e.ensure(ctx)
	if err != nil {
		return nil, err
	}
	e.manifest = manifest
	e.artifacts = remoteArtifactStore{executor: e}
	return e, nil
}

func (e *Executor) Manifest() protocol.ToolManifest { return e.manifest }

func (e *Executor) ExecutionTimeout(call *protocol.ToolCall) time.Duration {
	if call != nil && call.Policy.TimeoutMs > 0 {
		return time.Duration(call.Policy.TimeoutMs) * time.Millisecond
	}
	return runtime.RecommendedCallTimeout(e.cfg, call)
}

func (e *Executor) ArtifactStore() artifact.Store { return e.artifacts }

func (e *Executor) ArtifactInlineLimit() int64 {
	if e == nil || e.cfg == nil {
		return artifact.DefaultInlineBytes
	}
	return artifact.InlineLimit(e.cfg.Gateway.ArtifactMaxInlineBytes)
}

func (e *Executor) artifactFetchBudget() time.Duration {
	if e != nil && e.artifactTimeout > 0 {
		return e.artifactTimeout
	}
	return artifactFetchTimeout
}

func (e *Executor) Execute(ctx context.Context, call *protocol.ToolCall) *protocol.ToolResult {
	if call == nil {
		return bridgeErrorResult("", errors.New("nil tool call"))
	}
	callCtx, cancel := withExecutionTimeout(ctx, e.ExecutionTimeout(call))
	defer cancel()
	client, _, err := e.clientAndManifest(callCtx)
	if err != nil {
		return bridgeErrorResult(call.ID, err)
	}
	result, err := client.Call(callCtx, *call)
	if err != nil {
		// Do not retry a call here. The bridge could have executed it and died
		// while writing the response, so replay would break at-most-once intent.
		return bridgeErrorResult(call.ID, fmt.Errorf("local bridge call failed: %w", err))
	}
	return &result
}

// withExecutionTimeout establishes the same finite tool budget at the local
// RPC boundary. context.WithTimeout preserves a shorter deadline supplied by
// the host, so this can only bound a call further; it never extends it.
func withExecutionTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func bridgeErrorResult(id string, err error) *protocol.ToolResult {
	return &protocol.ToolResult{
		Type:   protocol.TypeToolResult,
		ID:     id,
		Status: "error",
		Error:  &protocol.ToolError{Code: "LOCAL_BRIDGE_UNAVAILABLE", Message: err.Error()},
	}
}

func (e *Executor) ensure(ctx context.Context) (protocol.ToolManifest, error) {
	_, manifest, err := e.clientAndManifest(ctx)
	return manifest, err
}

func (e *Executor) clientAndManifest(ctx context.Context) (*client.Client, protocol.ToolManifest, error) {
	if e == nil {
		return nil, protocol.ToolManifest{}, errors.New("nil local bridge executor")
	}
	if c, manifest, err := probe(ctx, e.socket); err == nil {
		return c, manifest, nil
	}
	if err := e.startOrWait(ctx); err != nil {
		return nil, protocol.ToolManifest{}, err
	}
	return waitForBridge(ctx, e.socket)
}

func (e *Executor) startOrWait(ctx context.Context) error {
	if err := ensurePrivateDir(filepath.Dir(e.socket)); err != nil {
		return err
	}
	lockPath := e.socket + ".start"
	deadline := time.Now().Add(startupTimeout)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}

	for {
		if _, _, err := probe(ctx, e.socket); err == nil {
			return nil
		}
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			defer os.Remove(lockPath)
			cmd := exec.Command(e.executable, "bridge", "serve", "--config", e.profile, "--socket", e.socket, "--idle-timeout", e.idleTimeout.String())
			detachBridgeProcess(cmd)
			cmd.Stdout = io.Discard
			// The bridge outlives this stdio proxy. Do not inherit the proxy's
			// stderr pipe: hosts (and os/exec) wait for that pipe to close when a
			// proxy exits, while a healthy idle bridge deliberately remains alive.
			stderr := &limitedBuffer{limit: startupStderrLimit}
			cmd.Stderr = stderr
			if err := cmd.Start(); err != nil {
				return fmt.Errorf("start local bridge: %w", err)
			}
			// Reap a bridge that exits before this proxy does. The bridge is
			// intentionally independent of the proxy's stdio lifetime, so waiting
			// happens in the background rather than coupling their shutdown.
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			// Keep the creator lock until the listener is observable. Otherwise
			// dozens of MCP children starting together can all spawn a losing
			// daemon before the first socket is ready.
			_, _, err := waitForBridgeOrExit(ctx, e.socket, exited, stderr)
			return err
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquire local bridge start lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleStartLockAfter {
			// The lock only protects process creation. A ready bridge is always
			// detected through its socket, so an old creator lock is safe to reap.
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("local bridge did not become ready before its startup deadline")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// limitedBuffer retains the short error emitted by a bridge that cannot start
// without retaining an inherited stderr pipe for its whole idle lifetime.
// It is only read after cmd.Wait reports that the child exited.
type limitedBuffer struct {
	limit int
	data  []byte
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b == nil || b.limit <= len(b.data) {
		return len(p), nil
	}
	remaining := b.limit - len(b.data)
	if remaining > len(p) {
		remaining = len(p)
	}
	b.data = append(b.data, p[:remaining]...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b == nil {
		return ""
	}
	return string(b.data)
}

func waitForBridge(ctx context.Context, socket string) (*client.Client, protocol.ToolManifest, error) {
	return waitForBridgeOrExit(ctx, socket, nil, nil)
}

// waitForBridgeOrExit distinguishes a missing listener from a bridge that
// already exited (for example because another configuration owns UserDataDir).
// Startup diagnostics stay in the MCP process instead of becoming a later,
// opaque browser-unavailable failure.
func waitForBridgeOrExit(ctx context.Context, socket string, exited <-chan error, stderr fmt.Stringer) (*client.Client, protocol.ToolManifest, error) {
	deadline := time.Now().Add(startupTimeout)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}
	var lastErr error
	for time.Now().Before(deadline) {
		if c, manifest, err := probe(ctx, socket); err == nil {
			return c, manifest, nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, protocol.ToolManifest{}, ctx.Err()
		case exitErr, ok := <-exited:
			// A competing proxy can win the daemon lock just before this child
			// observes it. Prefer the now-ready bridge over a harmless loser exit.
			if c, manifest, probeErr := probe(ctx, socket); probeErr == nil {
				return c, manifest, nil
			}
			if !ok || exitErr == nil {
				// runBridge exits successfully when a concurrent proxy already
				// owns this socket. That owner may still be between lock acquisition
				// and listen, so retain the ordinary startup wait instead of turning
				// a harmless race into a false bridge failure.
				exited = nil
				continue
			}
			detail := ""
			if stderr != nil {
				detail = strings.TrimSpace(stderr.String())
			}
			if detail != "" {
				return nil, protocol.ToolManifest{}, fmt.Errorf("local bridge exited during startup: %w: %s", exitErr, detail)
			}
			return nil, protocol.ToolManifest{}, fmt.Errorf("local bridge exited during startup: %w", exitErr)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no bridge response")
	}
	return nil, protocol.ToolManifest{}, fmt.Errorf("local bridge did not become ready: %w", lastErr)
}

func probe(parent context.Context, socket string) (*client.Client, protocol.ToolManifest, error) {
	ctx, cancel := context.WithTimeout(parent, 500*time.Millisecond)
	defer cancel()
	c := newSocketClient(socket)
	manifest, err := c.Manifest(ctx)
	if err != nil {
		return nil, protocol.ToolManifest{}, err
	}
	return c, manifest, nil
}

func newSocketClient(socket string) *client.Client {
	c := client.New("http://digcrawl-bridge", "")
	dialer := &net.Dialer{}
	c.HTTPClient = &http.Client{Transport: &http.Transport{
		// Executors probe before each call and are short-lived stdio proxies.
		// Do not retain one idle Unix-socket FD per probe/client; the bridge is
		// the shared long-lived owner, not its forwarding clients.
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		},
	}}
	return c
}

// SocketPath returns the private socket path for one real profile file. It
// resolves symlinks so a profile opened through two aliases shares one bridge.
func SocketPath(profile string) (string, error) {
	profile, err := canonicalPath(profile)
	if err != nil {
		return "", fmt.Errorf("resolve bridge profile: %w", err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve local bridge cache directory: %w", err)
	}
	digest := sha256.Sum256([]byte(profile))
	dir := filepath.Join(cache, "digcrawl", bridgeSocketDirName)
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%x.sock", digest[:12])), nil
}

func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create local bridge directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("protect local bridge directory: %w", err)
	}
	return nil
}

// Serve owns one local Runtime and its managed browser lifetime. It keeps no
// daemon registry: the Unix socket and an advisory process lock are the only
// ownership records, and a stale socket is safely replaced only by the next
// bridge owner.
func Serve(ctx context.Context, cfg *config.Config, socket string, idleTimeout time.Duration) error {
	if cfg == nil {
		return errors.New("bridge requires configuration")
	}
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return errors.New("bridge socket path is required")
	}
	if err := ensurePrivateDir(filepath.Dir(socket)); err != nil {
		return err
	}

	lock, err := acquireDaemonLock(socket + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	prepareManagedConfig(cfg)
	profileLock, err := acquireBrowserProfileLock(cfg)
	if err != nil {
		return err
	}
	if profileLock != nil {
		defer profileLock.Close()
	}
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale local bridge socket: %w", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on local bridge socket: %w", err)
	}
	defer listener.Close()
	if err := os.Chmod(socket, bridgeSocketFileMode); err != nil {
		return fmt.Errorf("protect local bridge socket: %w", err)
	}

	serveCtx, stop := context.WithCancel(ctx)
	defer stop()
	rt, err := runtime.NewRuntime(cfg)
	if err != nil {
		return fmt.Errorf("start local bridge runtime: %w", err)
	}
	tracker := newActivityTracker()
	handler := tracker.wrap(httpgw.NewServer(rt, rt.ArtifactStore(), ""))
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
	}
	go tracker.stopWhenIdle(serveCtx, server, idleTimeout)
	go func() {
		<-serveCtx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if closeErr := browser.Shutdown(shutdown); closeErr != nil && (err == nil || errors.Is(err, http.ErrServerClosed)) {
		err = fmt.Errorf("stop bridge-owned browser: %w", closeErr)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func prepareManagedConfig(cfg *config.Config) {
	cfg.LocalBrowser = browser.ManagedConfig(cfg.LocalBrowser)
	// Unix-socket directory permissions are the bridge's authentication
	// boundary. Do not inherit a remote HTTP gateway token into this local-only
	// server, where it would create an unnecessary second source of truth.
	cfg.Gateway.AuthToken = ""
}

// acquireBrowserProfileLock prevents different configuration files from
// starting separate bridge processes against the same persistent Chrome
// profile. The lock deliberately lives in the private cache rather than in
// UserDataDir: Chrome owns that directory's own Singleton* files and the
// bridge must claim ownership before it launches Chrome.
func acquireBrowserProfileLock(cfg *config.Config) (*daemonLock, error) {
	if cfg == nil {
		return nil, errors.New("bridge requires configuration")
	}
	userDataDir := strings.TrimSpace(cfg.LocalBrowser.UserDataDir)
	if userDataDir == "" {
		// Chrome creates a distinct temporary profile in this mode, so there is
		// no persistent browser identity to coordinate.
		return nil, nil
	}
	identity, err := canonicalPath(userDataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve configured Chrome profile: %w", err)
	}
	// Use the same identity for Chrome's launch argument. Otherwise a symlink
	// retargeted between locking and launch could make the lock and the browser
	// refer to different directories.
	cfg.LocalBrowser.UserDataDir = identity
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve local bridge cache directory: %w", err)
	}
	dir := filepath.Join(cache, "digcrawl", bridgeSocketDirName)
	if err := ensurePrivateDir(dir); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(identity))
	lock, err := acquireDaemonLock(filepath.Join(dir, fmt.Sprintf("browser-%x.lock", digest[:12])))
	if errors.Is(err, ErrAlreadyRunning) {
		return nil, ErrBrowserProfileInUse
	}
	if err != nil {
		return nil, fmt.Errorf("lock configured Chrome profile: %w", err)
	}
	return lock, nil
}

type activityTracker struct {
	mu     sync.Mutex
	active int
	last   time.Time
	wake   chan struct{}
}

func newActivityTracker() *activityTracker {
	return &activityTracker{last: time.Now(), wake: make(chan struct{}, 1)}
}

func (t *activityTracker) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A tool response is not complete until MCP has retrieved any inline
		// artifacts. Treat that delivery as runtime activity too, otherwise an
		// explicitly short Bridge idle timeout can close the server between the
		// tool result and its artifact response.
		activeRequest := (r.Method == http.MethodPost && r.URL.Path == "/tools/call") ||
			(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/artifacts/"))
		if !activeRequest {
			next.ServeHTTP(w, r)
			return
		}
		t.start()
		defer t.done()
		next.ServeHTTP(w, r)
	})
}

func (t *activityTracker) start() {
	t.mu.Lock()
	t.active++
	t.last = time.Now()
	t.mu.Unlock()
	t.notify()
}

func (t *activityTracker) done() {
	t.mu.Lock()
	t.active--
	t.last = time.Now()
	t.mu.Unlock()
	t.notify()
}

func (t *activityTracker) notify() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *activityTracker) idleFor(timeout time.Duration) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active > 0 {
		return timeout, false
	}
	remaining := timeout - time.Since(t.last)
	if remaining <= 0 {
		return 0, true
	}
	return remaining, false
}

func (t *activityTracker) stopWhenIdle(parent context.Context, server *http.Server, timeout time.Duration) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		delay, idle := t.idleFor(timeout)
		if idle {
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(shutdown)
			cancel()
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
		select {
		case <-parent.Done():
			return
		case <-t.wake:
		case <-timer.C:
		}
	}
}

type remoteArtifactStore struct{ executor *Executor }

func (s remoteArtifactStore) Put(context.Context, string, string, string, []byte) (protocol.Artifact, error) {
	return protocol.Artifact{}, errors.New("MCP bridge clients cannot create artifacts")
}

func (s remoteArtifactStore) Get(ctx context.Context, id string) ([]byte, string, error) {
	if s.executor == nil {
		return nil, "", errors.New("local bridge artifact store is unavailable")
	}
	// The tool's execution context is already complete before MCP asks the
	// store to inline artifacts. Establish an independent, finite delivery
	// budget at this RPC boundary so an unresponsive bridge cannot leave the
	// user-facing MCP response pending forever. A shorter caller deadline is
	// preserved by withExecutionTimeout.
	artifactCtx, cancel := withExecutionTimeout(ctx, s.executor.artifactFetchBudget())
	defer cancel()
	c, _, err := s.executor.clientAndManifest(artifactCtx)
	if err != nil {
		return nil, "", err
	}
	return c.FetchArtifact(artifactCtx, id)
}
