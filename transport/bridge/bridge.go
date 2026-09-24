// Package bridge owns DigCrawl's local browser runtime for one user profile.
// Stdio MCP processes are deliberately thin clients: they share this bridge
// over a private Unix socket instead of each managing a browser or CDP port.
package bridge

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	startupTimeout       = 8 * time.Second
	bridgeSocketDirName  = "bridge"
	bridgeSocketFileMode = 0o600
	// bridgeBrowserCleanupTimeout is intentionally independent from startup.
	// A direct restart rejects in-flight MCP work immediately, then gives the
	// owning browser manager enough time to release its Chrome profile before
	// this process releases the lifecycle lock to its replacement.
	bridgeBrowserCleanupTimeout = 20 * time.Second
	// artifactFetchTimeout bounds the post-call Unix-socket transfer used to
	// inline an artifact into an MCP response. It intentionally does not share
	// a tool's potentially long execution budget: by this point the tool has
	// already completed and a stalled bridge must not keep the MCP response open.
	artifactFetchTimeout = 120 * time.Second
)

var ErrAlreadyRunning = errors.New("local DigCrawl bridge is already running")

var ErrBridgeOwnerNotFound = errors.New("local DigCrawl bridge owns the lifecycle lock but its exact process was not found")

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
	executable string
	profile    string
	socket     string
	manifest   protocol.ToolManifest
	cfg        *config.Config
	artifacts  artifact.Store
	// artifactTimeout is normally zero, which selects artifactFetchTimeout. It
	// exists so the local RPC boundary can be exercised with a short test budget
	// without weakening the production timeout.
	artifactTimeout time.Duration
}

// NewExecutor connects to the bridge for profile, starting it on demand when
// no healthy bridge owns that profile. The bridge socket is scoped by the real
// profile file rather than its spelling, so symlink aliases cannot create
// duplicate Bridge owners.
func NewExecutor(ctx context.Context, executable, profile string, cfg *config.Config) (*Executor, error) {
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
	e := &Executor{executable: executable, profile: profile, socket: socket, cfg: cfg}
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
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensurePrivateDir(filepath.Dir(e.socket)); err != nil {
		return err
	}
	if _, _, err := probe(ctx, e.socket); err == nil {
		return nil
	}
	startupStatus := startupStatusFile{path: e.socket + ".startup-status"}
	_ = os.Remove(startupStatus.path)
	defer os.Remove(startupStatus.path)
	// The lifecycle lock is the only start arbiter. Concurrent starters may
	// briefly create a losing worker, but it immediately exits without owning a
	// listener, browser, or child process tree. Keeping no second transition
	// lock avoids another state that can outlive the operation it represents.
	cmd := exec.Command(e.executable, "bridge", "serve", "--managed-worker", "--config", e.profile, "--socket", e.socket, "--startup-status", startupStatus.path)
	detachBridgeProcess(cmd)
	// A persistent Bridge must not inherit the stdio proxy's pipes. In
	// particular, io.Discard and an in-memory stderr writer make os/exec
	// create parent-owned pipes; a host can close those independently of a
	// healthy Bridge. Real /dev/null descriptors make its I/O lifetime
	// independent while retaining the single socket as its only contract.
	nullOutput, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open bridge null output: %w", err)
	}
	cmd.Stdout = nullOutput
	cmd.Stderr = nullOutput
	if err := cmd.Start(); err != nil {
		_ = nullOutput.Close()
		return fmt.Errorf("start local bridge: %w", err)
	}
	// Start duplicates the descriptors into the child. Closing this copy
	// immediately keeps the proxy from holding an unnecessary FD.
	_ = nullOutput.Close()
	// Reap a bridge that exits before this proxy does. The bridge is
	// intentionally independent of the proxy's stdio lifetime, so waiting
	// happens in the background rather than coupling their shutdown.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	_, _, err = waitForBridgeOrExit(ctx, e.socket, exited, startupStatus)
	return err
}

// Stop terminates the singleton Bridge for profile. It is deliberately a
// direct stop for one local user: it does not drain MCP work
// or run a rolling handoff. A Bridge is launched in its own session, so this
// stops its exact process group (the Bridge and the Chromium tree it owns),
// then waits for both that group and the lifecycle lock to disappear before a
// replacement may start. A bridge that is already absent is a successful no-op.
func Stop(ctx context.Context, profile string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	profile, err := canonicalPath(profile)
	if err != nil {
		return false, fmt.Errorf("resolve bridge profile: %w", err)
	}
	socket, err := SocketPath(profile)
	if err != nil {
		return false, err
	}
	stopped := false
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		lock, err := acquireDaemonLock(socket + ".lock")
		if err == nil {
			defer lock.Close()
			// Holding the only lifecycle lock prevents a replacement worker from
			// starting while a previously signalled private group drains. Do not
			// signal a group from a record after its lock holder has exited: its
			// numeric PGID could have been reused. Waiting here makes that rare
			// unsafe state visible instead of risking an unrelated process.
			if err := waitForRecordedBridgeGroupRelease(ctx, socket); err != nil {
				return false, fmt.Errorf("wait for previous local bridge process group: %w", err)
			}
			removeBridgeOwner(socket)
			if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
				return false, fmt.Errorf("remove stale local bridge socket: %w", err)
			}
			return stopped, nil
		}
		if !errors.Is(err, ErrAlreadyRunning) {
			return false, err
		}
		if signalled, err := stopBridgeOwner(ctx, socket); err != nil {
			if !errors.Is(err, ErrBridgeOwnerNotFound) {
				return false, fmt.Errorf("terminate local bridge for %q: %w", profile, err)
			}
		} else if signalled {
			stopped = true
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
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
		// A Bridge writes this private handoff before it exits on a known
		// startup failure. Inspect it before a potentially slow socket probe:
		// under process-scheduling pressure cmd.Wait can otherwise arrive only
		// after the caller's deadline and erase the actionable cause.
		if stderr != nil {
			if detail := strings.TrimSpace(stderr.String()); detail != "" {
				return nil, protocol.ToolManifest{}, fmt.Errorf("local bridge reported startup failure: %s", detail)
			}
		}
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
// bridge owner. The Bridge itself has no idle shutdown: clients may be quiet
// between a readiness probe and a tool request, so stdio-session reclamation
// owns process cleanup while this singleton owns stable local transport.
func Serve(ctx context.Context, cfg *config.Config, socket string) error {
	if cfg == nil {
		return errors.New("bridge requires configuration")
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
	// The worker normally follows startOrWait, which already observed the
	// profile. Keep the same gate at the lock-owning boundary: a crashed stop
	// must not be bypassed by the next worker invocation. A bounded startup
	// error is safer than launching beside an old Chromium group whose profile
	// resources have not been released.
	recoveryCtx, cancelRecovery := context.WithTimeout(ctx, startupTimeout)
	err = waitForRecordedBridgeGroupRelease(recoveryCtx, socket)
	cancelRecovery()
	if err != nil {
		return err
	}
	if err := writeBridgeOwner(socket); err != nil {
		return err
	}
	defer removeBridgeOwner(socket)
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

	rt, err := runtime.NewRuntime(cfg)
	if err != nil {
		return fmt.Errorf("start local bridge runtime: %w", err)
	}
	gateway := httpgw.NewServer(rt, rt.ArtifactStore(), "")
	server := &http.Server{
		Handler:           gateway,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// This is an operator-triggered local restart, not a rolling service
		// deploy. Close immediately so in-flight reads are cancelled instead of
		// delaying the next run; browser cleanup below still owns Chrome.
		_ = server.Close()
	}()
	err = server.Serve(listener)
	shutdown, cancel := context.WithTimeout(context.Background(), bridgeBrowserCleanupTimeout)
	defer cancel()
	if closeErr := browser.ShutdownImmediately(shutdown); closeErr != nil && (err == nil || errors.Is(err, http.ErrServerClosed)) {
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
