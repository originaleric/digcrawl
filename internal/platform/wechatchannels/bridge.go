package wechatchannels

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"golang.org/x/net/http2"
)

const (
	DefaultBridgePort       = 20261
	defaultBridgeStateDir   = "storage/digcrawl/wechat-channels-bridge"
	bridgeStateFile         = "bridge-state.json"
	bridgeCACertFile        = "bridge-ca.pem"
	bridgeCAKeyFile         = "bridge-ca-key.pem"
	bridgeCACommonName      = "DigCrawl WeChat Channels Local Bridge CA"
	bridgePACPath           = "/v1/wechat-channels/proxy.pac"
	bridgeStopPath          = "/v1/wechat-channels/stop"
	bridgeDownloadPath      = "/v1/wechat-channels/download"
	bridgeDownloadReadyPath = "/v1/wechat-channels/download-ready"
	bridgeTokenHeader       = "X-DigCrawl-Bridge-Token"
	bridgePageScriptPath    = "/.well-known/digcrawl-wechat-channels-capture.js"
	bridgePageReportPath    = "/.well-known/digcrawl-wechat-channels-capture"
	bridgeReportHeader      = "X-DigCrawl-Bridge-Page-Report"
	bridgeModuleVersionKey  = "digcrawl_bridge"
	bridgeModuleVersion     = "3"
	maxCapturedBody         = 2 << 20
	maxDiagnosticPageHTML   = 16 << 20
	maxReportedMetadata     = 16 << 10
	maxReportedMediaURL     = 32 << 10
	maxInt64                = int64(1<<63 - 1)
	// DefaultDownloadMaxBytes is the safe compatibility limit used when a
	// legacy local CLI does not send max_bytes to a current bridge.
	DefaultDownloadMaxBytes = int64(2 * 1024 * 1024 * 1024)
	downloadProtocolVersion = 1
	remoteStopTimeout       = 10 * time.Second
	bridgeDialTimeout       = 10 * time.Second
	bridgeResponseTimeout   = 20 * time.Second
	maxBridgeCertificates   = 32
)

// BridgeOptions controls the local macOS bridge. The bridge only listens on
// loopback, intercepts TLS for qq.com, and retains only the current video's
// metadata in memory.
type BridgeOptions struct {
	Port           int
	StateDir       string
	NetworkService string
	WeChatAppPath  string
	// PageHTMLCapturePath is an explicit local-only diagnostic output. It is
	// never enabled by normal bridge operation and may contain sensitive page
	// state, so it must be an absolute path supplied by the user.
	PageHTMLCapturePath string
	// DiagnosticMetadataPath is an explicit local-only NDJSON output for
	// candidate request metadata. It excludes queries, headers, and all bodies,
	// so a protocol change can be investigated without collecting payloads.
	DiagnosticMetadataPath string
	// EnablePageAdapter enables the explicitly requested, page-module adapter.
	// It rewrites the Channels module graph and reports only the current
	// detail feed's download fields to this loopback bridge. It is off by
	// default because it changes the rendered WebView.
	EnablePageAdapter bool
}

// Bridge is the explicit-lifecycle local proxy and active-video endpoint used
// by the import command. It never writes WeChat request or response payloads.
type Bridge struct {
	options BridgeOptions
	env     bridgeEnvironment

	mu     sync.RWMutex
	phase  bridgePhase
	active activeVideoResponse
	// activeDownload is session-only and is never persisted or returned by the
	// metadata import endpoint. It contains the current video's short-lived
	// playback URL and optional decoding key for one explicit download.
	activeDownload activeDownloadResource
	// activeAmbiguous means more than one distinct video identity was observed
	// during this bridge session. Import must then fail rather than guessing.
	activeAmbiguous  bool
	captureStats     bridgeCaptureStats
	pageReportToken  string
	pageHTMLCaptured bool
	metadataCaptured bool

	server           *http.Server
	listener         net.Listener
	state            bridgeState
	pac              string
	webProxy         proxyConfig
	secureProxy      proxyConfig
	socksProxy       proxyConfig
	bypassDomains    []string
	transport        *http.Transport
	connections      map[net.Conn]context.CancelFunc
	certificates     map[string]tls.Certificate
	certificateHosts []string // oldest first; bounded session-only leaf cache
	certificateMu    sync.Mutex

	stopRequested     chan struct{}
	stopRequestedOnce sync.Once
}

// bridgePhase is the single lifecycle authority for the in-process bridge.
// All runtime resources belong to a non-idle phase and are accessed while
// Bridge.mu is held. The persisted bridgeState is recovery intent, not a
// second runtime state machine.
type bridgePhase uint8

const (
	bridgeIdle bridgePhase = iota
	bridgeStarting
	bridgeRunning
	bridgeStopping
)

// bridgeCaptureStats is session-local, aggregate-only observability for a
// failed import. It intentionally contains no host names, paths, headers,
// query values, identities, or request/response bytes.
type bridgeCaptureStats struct {
	TLSHTTP1                uint64
	TLSHTTP2                uint64
	HTTP1ParseErrors        uint64
	HTTPRequests            uint64
	CandidateRequests       uint64
	JSONRequests            uint64
	IdentityRequests        uint64
	JSONResponses           uint64
	JSONResponseIdentities  uint64
	CapturedResponses       uint64
	PageHTMLInjections      uint64
	PageScriptServes        uint64
	PageModuleInjections    uint64
	PageReports             uint64
	PageReportFailures      uint64
	PageAdapterRewrites     uint64
	RequestReadFailures     uint64
	ResponseReadFailures    uint64
	DiagnosticMetadata      uint64
	DiagnosticWriteFailures uint64
}

func (stats bridgeCaptureStats) String() string {
	return fmt.Sprintf("tls_h1=%d;tls_h2=%d;http1_parse_errors=%d;http_requests=%d;candidate_requests=%d;json_requests=%d;identity_requests=%d;json_responses=%d;json_response_identities=%d;captured_responses=%d;page_html_injections=%d;page_script_serves=%d;page_module_injections=%d;page_reports=%d;page_report_failures=%d;page_adapter_rewrites=%d;request_read_failures=%d;response_read_failures=%d;diagnostic_metadata=%d;diagnostic_write_failures=%d",
		stats.TLSHTTP1, stats.TLSHTTP2, stats.HTTP1ParseErrors, stats.HTTPRequests, stats.CandidateRequests, stats.JSONRequests, stats.IdentityRequests, stats.JSONResponses, stats.JSONResponseIdentities, stats.CapturedResponses, stats.PageHTMLInjections, stats.PageScriptServes, stats.PageModuleInjections, stats.PageReports, stats.PageReportFailures, stats.PageAdapterRewrites, stats.RequestReadFailures, stats.ResponseReadFailures, stats.DiagnosticMetadata, stats.DiagnosticWriteFailures)
}

// diagnosticRequestMetadata is deliberately limited to the user-approved
// protocol fields. In particular it has no query, headers, request body, or
// response body fields.
type diagnosticRequestMetadata struct {
	Host              string `json:"host"`
	Path              string `json:"path"`
	Method            string `json:"method"`
	ResponseStatus    int    `json:"response_status"`
	ContentType       string `json:"content_type"`
	ContentLength     int64  `json:"content_length"`
	TransportProtocol string `json:"transport_protocol"`
}

var (
	pageHeadTag            = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	pageModuleSource       = regexp.MustCompile(`(?i)((?:\bsrc|\bhref)\s*=\s*["'])([^"']+\.js(?:\?[^"']*)?)(["'])`)
	pageModuleImport       = regexp.MustCompile(`(?i)(\b(?:from|import)\s*(?:\(\s*)?["'])([^"']+\.js(?:\?[^"']*)?)(["'])`)
	finderGetCommentDetail = regexp.MustCompile(`async finderGetCommentDetail\((\w+)\)\{(.*?)\}async`)
)

// pageVideoReport is the exact, small metadata contract permitted from the
// injected QQ page. It excludes page HTML, headers, cookies, and all
// unrecognized fields. The media fields are permitted only while the page
// adapter is explicitly enabled; they stay in bridge memory and are used by
// the separate explicit download command.
type pageVideoReport struct {
	ObjectID      string `json:"object_id"`
	ObjectNonceID string `json:"object_nonce_id"`
	MediaType     int    `json:"media_type"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	AuthorID      string `json:"author_id"`
	AuthorName    string `json:"author_name"`
	PublishedAt   string `json:"published_at"`
	MediaURL      string `json:"media_url"`
	MediaURLToken string `json:"media_url_token"`
	DecodeKey     string `json:"decode_key"`
}

type activeDownloadResource struct {
	ObjectID      string
	ObjectNonceID string
	URL           string
	DecodeKey     uint64
}

type bridgeEnvironment struct {
	run        commandRunner
	listen     func(network, address string) (net.Listener, error)
	startApp   func(context.Context, string) error
	goos       string
	loadCA     func(string) (*tls.Certificate, string, error)
	writeState func(string, bridgeState) error
	readState  func(string) (bridgeState, error)
	remove     func(string) error
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
	RunInput(context.Context, string, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (execRunner) RunInput(ctx context.Context, name, input string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = strings.NewReader(input)
	return command.CombinedOutput()
}

type bridgeState struct {
	// LegacyVersion exists only to recover state files written by abandoned
	// bridge implementations. New state is identified by StateMachine instead
	// of a sequential product version.
	LegacyVersion           int    `json:"version,omitempty"`
	StateMachine            bool   `json:"state_machine,omitempty"`
	Service                 string `json:"service"`
	Port                    int    `json:"port"`
	CertificateSHA1         string `json:"certificate_sha1"`
	CertificateInstalled    bool   `json:"certificate_installed"`
	ProxyEnabled            bool   `json:"proxy_enabled"`
	ProxyMode               string `json:"proxy_mode,omitempty"`
	AutoProxyEnabled        bool   `json:"auto_proxy_enabled_before,omitempty"`
	AutoProxyURL            string `json:"auto_proxy_url_before,omitempty"`
	ControlToken            string `json:"control_token,omitempty"`
	DownloadProtocolVersion int    `json:"download_protocol_version,omitempty"`
	OwnerPID                int    `json:"owner_pid,omitempty"`
}

type proxyConfig struct {
	Enabled       bool
	Server        string
	Port          int
	Authenticated bool
}

type autoProxyConfig struct {
	Enabled bool
	URL     string
}

// NewBridge creates a bridge that must subsequently be started explicitly.
func NewBridge(options BridgeOptions) (*Bridge, error) {
	if options.Port == 0 {
		options.Port = DefaultBridgePort
	}
	if options.Port < 1 || options.Port > 65535 {
		return nil, fmt.Errorf("bridge port must be between 1 and 65535")
	}
	if strings.TrimSpace(options.StateDir) == "" {
		options.StateDir = defaultBridgeStateDir
	}
	if strings.TrimSpace(options.WeChatAppPath) == "" {
		options.WeChatAppPath = "/Applications/WeChat.app"
	}
	if path := strings.TrimSpace(options.PageHTMLCapturePath); path != "" {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("diagnostic page HTML path must be absolute")
		}
		options.PageHTMLCapturePath = filepath.Clean(path)
	}
	if path := strings.TrimSpace(options.DiagnosticMetadataPath); path != "" {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("diagnostic metadata path must be absolute")
		}
		options.DiagnosticMetadataPath = filepath.Clean(path)
		if _, err := os.Lstat(options.DiagnosticMetadataPath); err == nil {
			return nil, fmt.Errorf("diagnostic metadata path must not already exist")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect diagnostic metadata path: %w", err)
		}
	}
	return newBridge(options, defaultBridgeEnvironment()), nil
}

func newBridge(options BridgeOptions, env bridgeEnvironment) *Bridge {
	return &Bridge{options: options, env: env, connections: make(map[net.Conn]context.CancelFunc), certificates: make(map[string]tls.Certificate)}
}

func (b *Bridge) beginStart() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.phase != bridgeIdle {
		return fmt.Errorf("wechat channels bridge is already %s", b.phase)
	}
	b.phase = bridgeStarting
	b.active = activeVideoResponse{}
	b.activeDownload = activeDownloadResource{}
	b.activeAmbiguous = false
	b.captureStats = bridgeCaptureStats{}
	b.pageReportToken = ""
	b.pageHTMLCaptured = false
	b.metadataCaptured = false
	b.connections = make(map[net.Conn]context.CancelFunc)
	b.certificates = make(map[string]tls.Certificate)
	b.certificateHosts = nil
	return nil
}

func (b *Bridge) abandonUnstartedSession() {
	b.mu.Lock()
	transport := b.transport
	b.transport = nil
	b.state = bridgeState{}
	b.stopRequested = nil
	b.captureStats = bridgeCaptureStats{}
	b.pageReportToken = ""
	b.pageHTMLCaptured = false
	b.metadataCaptured = false
	b.connections = make(map[net.Conn]context.CancelFunc)
	b.certificates = make(map[string]tls.Certificate)
	b.certificateHosts = nil
	b.phase = bridgeIdle
	b.mu.Unlock()
	if transport != nil {
		transport.CloseIdleConnections()
	}
}

func (b *Bridge) finishStart() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.phase != bridgeStarting || b.server == nil {
		return false
	}
	b.phase = bridgeRunning
	return true
}

func (phase bridgePhase) String() string {
	switch phase {
	case bridgeIdle:
		return "idle"
	case bridgeStarting:
		return "starting"
	case bridgeRunning:
		return "running"
	case bridgeStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

func defaultBridgeEnvironment() bridgeEnvironment {
	return bridgeEnvironment{
		run:        execRunner{},
		listen:     net.Listen,
		startApp:   openWeChat,
		goos:       runtime.GOOS,
		loadCA:     loadOrCreateCA,
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	}
}

// Start installs this bridge's per-user CA, starts the loopback server, and
// enables a PAC that sends only qq.com through the bridge. Other traffic keeps
// the existing manual proxy selection. Call Stop to restore the network state.
func (b *Bridge) Start(ctx context.Context, openApp bool) error {
	if b.env.goos != "darwin" {
		return fmt.Errorf("wechat channels bridge is currently supported only on macOS")
	}
	if err := b.beginStart(); err != nil {
		return err
	}
	statePath := b.statePath()
	if _, err := b.env.readState(statePath); err == nil {
		b.abandonUnstartedSession()
		return fmt.Errorf("a previous bridge session is still recorded; run `digcrawl wechat-channels bridge stop` first")
	} else if !errors.Is(err, os.ErrNotExist) {
		b.abandonUnstartedSession()
		return fmt.Errorf("read bridge state: %w", err)
	}

	if err := os.MkdirAll(b.options.StateDir, 0o700); err != nil {
		b.abandonUnstartedSession()
		return fmt.Errorf("create bridge state directory: %w", err)
	}
	cert, fingerprint, err := b.env.loadCA(b.options.StateDir)
	if err != nil {
		b.abandonUnstartedSession()
		return err
	}
	service, err := b.resolveService(ctx)
	if err != nil {
		b.abandonUnstartedSession()
		return err
	}
	pac, autoProxy, err := b.preparePAC(ctx, service)
	if err != nil {
		b.abandonUnstartedSession()
		return err
	}
	b.mu.Lock()
	b.pac = pac
	b.transport = &http.Transport{
		Proxy: b.outboundProxy,
		// Some local proxy implementations emit bytes after a request has
		// completed. Do not return that connection to the idle pool, where the
		// standard library would log those bytes and risk contaminating a later
		// forwarded request.
		DisableKeepAlives: true,
		ForceAttemptHTTP2: true,
		DialContext: (&net.Dialer{
			Timeout: bridgeDialTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   bridgeDialTimeout,
		ResponseHeaderTimeout: bridgeResponseTimeout,
		IdleConnTimeout:       bridgeResponseTimeout,
	}
	b.mu.Unlock()
	controlToken, err := newControlToken()
	if err != nil {
		b.abandonUnstartedSession()
		return fmt.Errorf("generate bridge control token: %w", err)
	}
	pageReportToken, err := newControlToken()
	if err != nil {
		b.abandonUnstartedSession()
		return fmt.Errorf("generate bridge page-report token: %w", err)
	}
	b.mu.Lock()
	b.stopRequested = make(chan struct{})
	b.stopRequestedOnce = sync.Once{}
	b.pageReportToken = pageReportToken
	b.state = bridgeState{StateMachine: true, Service: service, Port: b.options.Port, CertificateSHA1: fingerprint, ProxyMode: "pac", AutoProxyEnabled: autoProxy.Enabled, AutoProxyURL: autoProxy.URL, ControlToken: controlToken, DownloadProtocolVersion: downloadProtocolVersion, OwnerPID: os.Getpid()}
	state := b.state
	b.mu.Unlock()
	if err := b.env.writeState(statePath, state); err != nil {
		b.abandonUnstartedSession()
		return fmt.Errorf("write bridge recovery state: %w", err)
	}

	certificatePresent, err := b.certificatePresent(ctx)
	if err != nil {
		_ = b.env.remove(statePath)
		b.abandonUnstartedSession()
		return err
	}
	if !certificatePresent {
		// Persist cleanup intent before changing the keychain. If the process
		// stops during security(1), bridge stop can still remove this exact CA.
		b.mu.Lock()
		b.state.CertificateInstalled = true
		state = b.state
		b.mu.Unlock()
		if err := b.env.writeState(statePath, state); err != nil {
			_ = b.env.remove(statePath)
			b.abandonUnstartedSession()
			return fmt.Errorf("write bridge recovery state: %w", err)
		}
		if err := b.installCertificate(ctx); err != nil {
			return b.cleanupStartFailure(ctx, err)
		}
	}

	listener, err := b.env.listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.options.Port)))
	if err != nil {
		return b.cleanupStartFailure(ctx, fmt.Errorf("listen on local bridge port: %w", err))
	}
	b.mu.Lock()
	b.listener = listener
	server := &http.Server{Handler: b.handler(cert), ReadHeaderTimeout: 5 * time.Second}
	b.server = server
	b.mu.Unlock()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			b.requestStop()
		}
	}()

	b.mu.Lock()
	b.state.ProxyEnabled = true
	state = b.state
	b.mu.Unlock()
	if err := b.env.writeState(statePath, state); err != nil {
		return b.cleanupStartFailure(ctx, fmt.Errorf("write bridge recovery state: %w", err))
	}
	if err := b.enablePAC(ctx, service); err != nil {
		return b.cleanupStartFailure(ctx, err)
	}
	if b.stopRequestedBeforeActivation() {
		return b.cleanupStartFailure(ctx, fmt.Errorf("local bridge server stopped before activation"))
	}
	if !b.finishStart() {
		return b.cleanupStartFailure(ctx, fmt.Errorf("local bridge server stopped before activation"))
	}
	if openApp {
		if err := b.env.startApp(ctx, b.options.WeChatAppPath); err != nil {
			return b.cleanupStartFailure(ctx, err)
		}
	}
	return nil
}

// StopRequested is closed by an authenticated local stop request. The owning
// process remains the only writer of system proxy and certificate state.
func (b *Bridge) StopRequested() <-chan struct{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stopRequested
}

func (b *Bridge) requestStop() {
	b.mu.RLock()
	stopRequested := b.stopRequested
	b.mu.RUnlock()
	if stopRequested == nil {
		return
	}
	b.stopRequestedOnce.Do(func() { close(stopRequested) })
}

func (b *Bridge) stopRequestedBeforeActivation() bool {
	b.mu.RLock()
	stopRequested := b.stopRequested
	b.mu.RUnlock()
	if stopRequested == nil {
		return false
	}
	select {
	case <-stopRequested:
		return true
	default:
		return false
	}
}

func (b *Bridge) cleanupStartFailure(ctx context.Context, startErr error) error {
	// Start may have been interrupted, which cancels its context before the
	// network state has been restored. Cleanup must still get one bounded,
	// independent attempt to remove the PAC and certificate.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if cleanupErr := b.Stop(cleanupCtx); cleanupErr != nil {
		return fmt.Errorf("%w; cleanup requires `digcrawl wechat-channels bridge stop`: %v", startErr, cleanupErr)
	}
	return startErr
}

// Stop reverses only this bridge's active proxy settings, then removes only
// this bridge's CA certificate. A user-selected proxy made after Start is not
// overwritten.
func (b *Bridge) Stop(ctx context.Context) error {
	b.mu.RLock()
	phaseBeforeRead := b.phase
	inMemoryState := b.state
	b.mu.RUnlock()
	localOwner := phaseBeforeRead == bridgeStarting || phaseBeforeRead == bridgeRunning
	state, err := b.env.readState(b.statePath())
	if errors.Is(err, os.ErrNotExist) {
		if !localOwner {
			return nil
		}
		state = inMemoryState
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read bridge recovery state: %w", err)
	}
	b.mu.Lock()
	phase := b.phase
	if phase == bridgeStopping {
		b.mu.Unlock()
		return fmt.Errorf("wechat channels bridge is already stopping")
	}
	localOwner = phase == bridgeStarting || phase == bridgeRunning
	b.state = state
	if localOwner {
		b.phase = bridgeStopping
	}
	stopRequested := b.stopRequested
	b.mu.Unlock()
	b.options.Port = state.Port
	if !localOwner && stopRequested == nil && state.ControlToken != "" {
		if err := b.requestRunningBridgeStop(ctx, state); err == nil {
			return b.waitForOwnerCleanup(ctx)
		} else if !isBridgeUnavailable(err) {
			return fmt.Errorf("request running bridge stop: %w", err)
		} else if ownerProcessAlive(state.OwnerPID) {
			return fmt.Errorf("bridge owner process %d is starting or unavailable; wait and retry", state.OwnerPID)
		}
	}
	if !localOwner {
		b.mu.Lock()
		if b.phase == bridgeStopping {
			b.mu.Unlock()
			return fmt.Errorf("wechat channels bridge is already stopping")
		}
		b.phase = bridgeStopping
		b.mu.Unlock()
	}
	var errs []error
	if state.ProxyEnabled {
		if err := b.restoreProxy(ctx, state); err != nil {
			errs = append(errs, err)
		}
	}
	b.mu.RLock()
	server := b.server
	b.mu.RUnlock()
	if server != nil {
		b.closeConnections()
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		errs = append(errs, server.Shutdown(shutdownCtx))
		cancel()
		b.mu.Lock()
		b.server = nil
		b.listener = nil
		b.mu.Unlock()
	}
	b.certificateMu.Lock()
	b.mu.Lock()
	transport := b.transport
	b.transport = nil
	b.certificates = make(map[string]tls.Certificate)
	b.certificateHosts = nil
	b.connections = make(map[net.Conn]context.CancelFunc)
	b.mu.Unlock()
	b.certificateMu.Unlock()
	if transport != nil {
		transport.CloseIdleConnections()
	}
	if state.CertificateInstalled {
		if err := b.removeCertificate(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("stop wechat channels bridge: %w", err)
	}
	if err := b.env.remove(b.statePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b.mu.Lock()
	b.phase = bridgeIdle
	b.stopRequested = nil
	b.pageReportToken = ""
	b.state = bridgeState{}
	b.activeDownload = activeDownloadResource{}
	b.mu.Unlock()
	return nil
}

func (b *Bridge) statePath() string { return filepath.Join(b.options.StateDir, bridgeStateFile) }

var errBridgeUnavailable = errors.New("bridge control endpoint unavailable")

func (b *Bridge) requestRunningBridgeStop(ctx context.Context, state bridgeState) error {
	requestCtx, cancel := context.WithTimeout(ctx, remoteStopTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(state.Port)+bridgeStopPath, nil)
	if err != nil {
		return err
	}
	request.Header.Set(bridgeTokenHeader, state.ControlToken)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return errBridgeUnavailable
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected bridge control response: HTTP %d", response.StatusCode)
	}
	return nil
}

func (b *Bridge) waitForOwnerCleanup(ctx context.Context) error {
	deadline := time.NewTimer(remoteStopTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := b.env.readState(b.statePath()); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("read bridge recovery state: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("bridge accepted stop request but did not finish cleanup within %s", remoteStopTimeout)
		case <-ticker.C:
		}
	}
}

func isBridgeUnavailable(err error) bool { return errors.Is(err, errBridgeUnavailable) }

func ownerProcessAlive(pid int) bool {
	if pid < 1 {
		return false
	}
	alive, err := browser.ProcessAlive(pid)
	// An unknown owner must retain its recovery state until it can be checked.
	return alive || err != nil
}

func (b *Bridge) handler(cert *tls.Certificate) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLocalPACRequest(r) {
			b.servePAC(w)
			return
		}
		if b.serveBridgePageEndpoint(w, r, "") {
			return
		}
		if isLocalBridgeRequest(r) {
			b.serveActiveVideo(w, r)
			return
		}
		if isLocalBridgeDownloadRequest(r) {
			b.serveActiveDownload(w, r)
			return
		}
		if isLocalBridgeDownloadReadyRequest(r) {
			b.serveActiveDownloadReady(w, r)
			return
		}
		if isLocalBridgeStopRequest(r) {
			b.serveStopRequest(w, r)
			return
		}
		if !b.isProxyRunning() {
			http.Error(w, "bridge is not running", http.StatusServiceUnavailable)
			return
		}
		if !isQQProxyRequest(r) {
			http.Error(w, "bridge proxy accepts qq.com only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodConnect {
			b.serveConnect(w, r, cert)
			return
		}
		b.serveHTTPProxy(w, r)
	})
}

// serveBridgePageEndpoint owns the synthetic QQ page routes. It is called by
// every HTTP entrypoint: the clear-text proxy handler and both protocols after
// TLS interception. A request handled here must never reach the upstream QQ
// server, because it carries a bridge-local capability token.
func (b *Bridge) serveBridgePageEndpoint(w http.ResponseWriter, r *http.Request, connectedHost string) bool {
	if connectedHost != "" && requestHost(r) != authorityHost(connectedHost) {
		return false
	}
	if !b.options.EnablePageAdapter {
		return false
	}
	if isBridgePageScriptRequest(r) {
		b.servePageCaptureScript(w, r)
		return true
	}
	if isBridgePageReportRequest(r) {
		b.servePageReport(w, r)
		return true
	}
	return false
}

func isLocalPACRequest(r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != bridgePACPath {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return isLoopbackHost(host)
}

func (b *Bridge) servePAC(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, b.pac)
}

func isLocalBridgeRequest(r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != activeVideoAPIPath {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return isLoopbackHost(host)
}

func isLocalBridgeDownloadRequest(r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != bridgeDownloadPath {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return isLoopbackHost(host)
}

func isLocalBridgeDownloadReadyRequest(r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != bridgeDownloadReadyPath {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return isLoopbackHost(host)
}

func isBridgePageScriptRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == bridgePageScriptPath && requestHost(r) == "channels.weixin.qq.com"
}

func isBridgePageReportRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == bridgePageReportPath && requestHost(r) == "channels.weixin.qq.com"
}

func isLocalBridgeStopRequest(r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != bridgeStopPath {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	return isLoopbackHost(host)
}

func isQQProxyRequest(r *http.Request) bool {
	return isQQHost(requestHost(r))
}

func isQQHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(strings.Trim(host, "[]")))
	return host == "qq.com" || strings.HasSuffix(host, ".qq.com")
}

func requestHost(req *http.Request) string {
	if req == nil {
		return ""
	}
	if req.URL != nil {
		if host := strings.TrimSpace(req.URL.Hostname()); host != "" {
			return strings.ToLower(host)
		}
	}
	host := strings.TrimSpace(req.Host)
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

func (b *Bridge) serveActiveVideo(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.NotFound(w, r)
		return
	}
	if !b.isProxyRunning() {
		http.Error(w, "bridge is not running", http.StatusServiceUnavailable)
		return
	}
	b.mu.RLock()
	active := b.active
	ambiguous := b.activeAmbiguous
	b.mu.RUnlock()
	if ambiguous {
		http.Error(w, "multiple WeChat Channels videos were observed; restart the bridge and open only the video to import", http.StatusConflict)
		return
	}
	if strings.TrimSpace(active.ObjectID) == "" || strings.TrimSpace(active.ObjectNonceID) == "" {
		w.Header().Set("X-DigCrawl-Bridge-Capture-Stats", b.captureStatsHeader())
		http.Error(w, "open and play a WeChat Channels video first", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(active)
}

type activeDownloadRequest struct {
	OutputPath string `json:"output_path"`
	MaxBytes   int64  `json:"max_bytes"`
}

type activeDownloadResponse struct {
	OutputPath string `json:"output_path"`
	Bytes      int64  `json:"bytes"`
	Decrypted  bool   `json:"decrypted"`
}

type activeDownloadReadyResponse struct {
	Ready bool `json:"ready"`
}

// serveActiveDownloadReady exposes only whether a complete download resource
// is available. It deliberately keeps the media URL and decode key inside the
// bridge so foreground capture can wait without starting a long download.
func (b *Bridge) serveActiveDownloadReady(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.NotFound(w, r)
		return
	}
	if !b.isProxyRunning() {
		http.Error(w, "bridge is not running", http.StatusServiceUnavailable)
		return
	}
	b.mu.RLock()
	resource := b.activeDownload
	ambiguous := b.activeAmbiguous
	b.mu.RUnlock()
	if ambiguous {
		http.Error(w, "multiple WeChat Channels videos were observed; restart the bridge and open only the video to download", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(activeDownloadReadyResponse{
		Ready: resource.URL != "" && resource.ObjectID != "" && resource.ObjectNonceID != "",
	})
}

// serveActiveDownload writes one user-selected output path from the current
// bridge session. It never returns the playback URL or decoding key.
func (b *Bridge) serveActiveDownload(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.NotFound(w, r)
		return
	}
	if !b.isProxyRunning() {
		http.Error(w, "bridge is not running", http.StatusServiceUnavailable)
		return
	}
	var request activeDownloadRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid download request", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "invalid download request", http.StatusBadRequest)
		return
	}
	outputPath := strings.TrimSpace(request.OutputPath)
	if !filepath.IsAbs(outputPath) {
		http.Error(w, "download output path must be absolute", http.StatusBadRequest)
		return
	}
	maxBytes, err := normalizeDownloadMaxBytes(request.MaxBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	outputPath = filepath.Clean(outputPath)
	b.mu.RLock()
	resource := b.activeDownload
	ambiguous := b.activeAmbiguous
	b.mu.RUnlock()
	if ambiguous {
		http.Error(w, "multiple WeChat Channels videos were observed; restart the bridge and open only the video to download", http.StatusConflict)
		return
	}
	if resource.URL == "" || resource.ObjectID == "" || resource.ObjectNonceID == "" {
		w.Header().Set("X-DigCrawl-Bridge-Capture-Stats", b.captureStatsHeader())
		http.Error(w, "open and play one WeChat Channels video with the page adapter enabled first", http.StatusNotFound)
		return
	}
	bytesWritten, err := b.downloadActiveVideo(r.Context(), resource, outputPath, maxBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(activeDownloadResponse{OutputPath: outputPath, Bytes: bytesWritten, Decrypted: resource.DecodeKey != 0})
}

func (b *Bridge) downloadActiveVideo(ctx context.Context, resource activeDownloadResource, outputPath string, maxBytes int64) (int64, error) {
	if maxBytes <= 0 || maxBytes == maxInt64 {
		return 0, fmt.Errorf("video download maximum size must be between 1 and %d bytes", maxInt64-1)
	}
	target, err := url.Parse(resource.URL)
	if err != nil || target == nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || !isQQHost(target.Hostname()) {
		return 0, errors.New("captured video download URL is invalid")
	}
	if info, err := os.Lstat(outputPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		if err == nil && info != nil {
			return 0, fmt.Errorf("download output already exists: %s", outputPath)
		}
		return 0, fmt.Errorf("inspect download output: %w", err)
	}
	directory := filepath.Dir(outputPath)
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		if err != nil {
			return 0, fmt.Errorf("inspect download directory: %w", err)
		}
		return 0, fmt.Errorf("download parent is not a directory: %s", directory)
	}
	b.mu.RLock()
	transport := b.transport
	b.mu.RUnlock()
	if transport == nil {
		return 0, errors.New("bridge transport is not running")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("build video download request: %w", err)
	}
	request.Header.Set("Accept", "video/*")
	client := &http.Client{Transport: transport, CheckRedirect: func(next *http.Request, _ []*http.Request) error {
		if next.URL == nil || next.URL.Scheme != "https" || next.URL.User != nil || next.URL.Fragment != "" || !isQQHost(next.URL.Hostname()) {
			return errors.New("video download redirect left qq.com")
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("download current WeChat Channels video: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("QQ video source rejected the captured playback request (HTTP %d); reopen and play the video, then retry", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return 0, fmt.Errorf("current WeChat Channels video is %d bytes, exceeding the %d-byte limit", response.ContentLength, maxBytes)
	}
	temporary, err := os.CreateTemp(directory, ".digcrawl-wechat-channels-*.part")
	if err != nil {
		return 0, fmt.Errorf("create video download file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return 0, fmt.Errorf("secure video download file: %w", err)
	}
	reader := io.Reader(response.Body)
	if resource.DecodeKey != 0 {
		reader = newChannelsDecryptReader(response.Body, resource.DecodeKey, 131072)
	}
	bytesWritten, copyErr := io.Copy(temporary, io.LimitReader(reader, maxBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("write video download: %w", copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close video download: %w", closeErr)
	}
	if bytesWritten > maxBytes {
		return 0, fmt.Errorf("current WeChat Channels video exceeds the %d-byte limit", maxBytes)
	}
	if bytesWritten == 0 {
		return 0, errors.New("current WeChat Channels video download was empty")
	}
	// link(2) is an atomic no-overwrite publication in the same directory. It
	// keeps a concurrent user-created output file from being replaced.
	if err := os.Link(temporaryPath, outputPath); err != nil {
		return 0, fmt.Errorf("publish video download: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return 0, fmt.Errorf("finalize video download: %w", err)
	}
	return bytesWritten, nil
}

func normalizeDownloadMaxBytes(maxBytes int64) (int64, error) {
	if maxBytes == 0 {
		return DefaultDownloadMaxBytes, nil
	}
	if maxBytes < 0 || maxBytes == maxInt64 {
		return 0, fmt.Errorf("download maximum size must be between 1 and %d bytes", maxInt64-1)
	}
	return maxBytes, nil
}

func (b *Bridge) servePageCaptureScript(w http.ResponseWriter, r *http.Request) {
	if !b.isProxyRunning() {
		http.Error(w, "bridge is not running", http.StatusServiceUnavailable)
		return
	}
	b.mu.Lock()
	b.captureStats.PageScriptServes++
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, b.pageCaptureScript())
}

func (b *Bridge) servePageReport(w http.ResponseWriter, r *http.Request) {
	if !b.isProxyRunning() || !b.authorizedPageReport(r) {
		http.NotFound(w, r)
		return
	}
	if !isJSONRequest(r) || r.Body == nil {
		b.recordPageReportFailure()
		http.Error(w, "invalid page report", http.StatusBadRequest)
		return
	}
	var report pageVideoReport
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxCapturedBody+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		b.recordPageReportFailure()
		http.Error(w, "invalid page report", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		b.recordPageReportFailure()
		http.Error(w, "invalid page report", http.StatusBadRequest)
		return
	}
	video, resource, ok := activeVideoFromPageReport(report)
	if !ok {
		b.recordPageReportFailure()
		http.Error(w, "invalid page report", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.captureStats.PageReports++
	b.mu.Unlock()
	b.recordActiveVideoWithDownload(video, resource)
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bridge) authorizedPageReport(r *http.Request) bool {
	b.mu.RLock()
	token := b.pageReportToken
	b.mu.RUnlock()
	return token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(bridgeReportHeader)), []byte(token)) == 1
}

func (b *Bridge) recordPageReportFailure() {
	b.mu.Lock()
	b.captureStats.PageReportFailures++
	b.mu.Unlock()
}

func activeVideoFromPageReport(report pageVideoReport) (activeVideoResponse, activeDownloadResource, bool) {
	if report.MediaType != 4 {
		return activeVideoResponse{}, activeDownloadResource{}, false
	}
	video := activeVideoResponse{
		ObjectID:      strings.TrimSpace(report.ObjectID),
		ObjectNonceID: strings.TrimSpace(report.ObjectNonceID),
		Title:         trimReportedMetadata(report.Title),
		Description:   trimReportedMetadata(report.Description),
		AuthorID:      trimReportedMetadata(report.AuthorID),
		AuthorName:    trimReportedMetadata(report.AuthorName),
		PublishedAt:   trimReportedMetadata(report.PublishedAt),
	}
	if video.ObjectID == "" || video.ObjectNonceID == "" || len(video.ObjectID) > maxReportedMetadata || len(video.ObjectNonceID) > maxReportedMetadata {
		return activeVideoResponse{}, activeDownloadResource{}, false
	}
	// Metadata import remains available if a desktop build exposes the detail
	// object but not a usable playback resource. In that case only the explicit
	// download endpoint remains unavailable.
	resource, _ := downloadResourceFromPageReport(report, video)
	return video, resource, true
}

func downloadResourceFromPageReport(report pageVideoReport, video activeVideoResponse) (activeDownloadResource, bool) {
	base := strings.TrimSpace(report.MediaURL)
	if base == "" || len(base) > maxReportedMediaURL {
		return activeDownloadResource{}, false
	}
	token := strings.TrimSpace(report.MediaURLToken)
	if len(token) > maxReportedMediaURL || strings.ContainsAny(token, "\r\n") {
		return activeDownloadResource{}, false
	}
	if token != "" {
		switch {
		case strings.HasPrefix(token, "&"), strings.HasPrefix(token, "?"):
			base += token
		case strings.Contains(base, "?"):
			base += "&" + token
		default:
			base += "?" + token
		}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.User != nil || !isQQHost(parsed.Hostname()) || parsed.Fragment != "" {
		return activeDownloadResource{}, false
	}
	// The query is a signed playback contract. It may carry parameters beyond
	// encfilekey and token, so do not normalize, reorder, or remove any part
	// of it before the one explicit bridge download. It remains session-only.
	var decodeKey uint64
	if rawKey := strings.TrimSpace(report.DecodeKey); rawKey != "" {
		decodeKey, err = strconv.ParseUint(rawKey, 0, 64)
		if err != nil || decodeKey == 0 {
			return activeDownloadResource{}, false
		}
	}
	return activeDownloadResource{ObjectID: video.ObjectID, ObjectNonceID: video.ObjectNonceID, URL: parsed.String(), DecodeKey: decodeKey}, true
}

func trimReportedMetadata(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxReportedMetadata {
		return ""
	}
	return value
}

func (b *Bridge) pageCaptureScript() string {
	b.mu.RLock()
	token := b.pageReportToken
	b.mu.RUnlock()
	config, _ := json.Marshal(map[string]string{"endpoint": bridgePageReportPath, "token": token})
	return `(function(){"use strict";const config=` + string(config) + `;function text(value){return typeof value==="string"?value:""}function scalar(value){return typeof value==="string"?value:(typeof value==="number"&&Number.isSafeInteger(value)?String(value):"")}function report(feed){try{const desc=feed&&(feed.objectDesc||feed.object_desc),media=desc&&Array.isArray(desc.media)&&desc.media[0];if(!desc||Number(desc.mediaType||desc.media_type)!==4||!media){return}const objectID=scalar(feed.id||feed.objectId||feed.objectid),nonceID=scalar(feed.objectNonceId||feed.object_nonce_id||feed.nonceId);if(!objectID||!nonceID){return}const contact=feed.contact||{};const body={object_id:objectID,object_nonce_id:nonceID,media_type:4,title:text(desc.description||feed.description||feed.title),description:text(desc.description||feed.description||feed.title),author_id:scalar(contact.username||contact.id),author_name:text(contact.nickname||contact.name),published_at:scalar(feed.publishTime||feed.createtime||feed.createTime),media_url:text(media.url||media.videoUrl),media_url_token:text(media.urlToken),decode_key:scalar(media.decodeKey)};fetch(config.endpoint,{method:"POST",credentials:"omit",keepalive:true,headers:{"Content-Type":"application/json","` + bridgeReportHeader + `":config.token},body:JSON.stringify(body)}).catch(function(){});}catch(_){}}window.__digcrawlWechatChannelsReport=report;})();`
}

func (b *Bridge) serveStopRequest(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.NotFound(w, r)
		return
	}
	b.requestStop()
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bridge) authorized(r *http.Request) bool {
	b.mu.RLock()
	token := b.state.ControlToken
	b.mu.RUnlock()
	return token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(bridgeTokenHeader)), []byte(token)) == 1
}

func (b *Bridge) isProxyRunning() bool {
	b.mu.RLock()
	running := b.phase == bridgeRunning
	b.mu.RUnlock()
	return running
}

func (b *Bridge) captureStatsHeader() string {
	b.mu.RLock()
	stats := b.captureStats
	b.mu.RUnlock()
	return stats.String()
}

func (b *Bridge) recordTLSProtocol(protocol string) {
	b.mu.Lock()
	if protocol == "h2" {
		b.captureStats.TLSHTTP2++
	} else {
		b.captureStats.TLSHTTP1++
	}
	b.mu.Unlock()
}

func (b *Bridge) recordHTTP1ParseError() {
	b.mu.Lock()
	b.captureStats.HTTP1ParseErrors++
	b.mu.Unlock()
}

// serveConnect terminates TLS only after the PAC-selected peer has been
// constrained to qq.com. Both HTTP versions are then forwarded through the
// same bridge transport, so protocol negotiation cannot bypass the original
// proxy route or the metadata capture path.
func (b *Bridge) serveConnect(w http.ResponseWriter, r *http.Request, ca *tls.Certificate) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "bridge cannot take over CONNECT", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	connectionCtx, cancel := context.WithCancel(context.Background())
	if !b.trackConnection(client, cancel) {
		cancel()
		_ = client.Close()
		return
	}
	defer b.untrackConnection(client)
	defer cancel()
	defer client.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	authority := connectAuthority(r)
	host := authorityHost(authority)
	config, err := b.tlsConfigForHost(ca, host)
	if err != nil {
		return
	}
	tlsClient := tls.Server(client, config)
	_ = tlsClient.SetDeadline(time.Now().Add(10 * time.Second))
	if err := tlsClient.Handshake(); err != nil {
		return
	}
	_ = tlsClient.SetDeadline(time.Time{})
	protocol := tlsClient.ConnectionState().NegotiatedProtocol
	b.recordTLSProtocol(protocol)
	if protocol == "h2" {
		b.serveHTTP2(tlsClient, authority, connectionCtx)
		return
	}
	b.serveHTTP1(tlsClient, authority, connectionCtx)
}

func connectAuthority(r *http.Request) string {
	authority := strings.TrimSpace(r.Host)
	if authority == "" && r.URL != nil {
		authority = strings.TrimSpace(r.URL.Host)
	}
	if _, _, err := net.SplitHostPort(authority); err == nil {
		return authority
	}
	return net.JoinHostPort(strings.Trim(authority, "[]"), "443")
}

func authorityHost(authority string) string {
	host, _, err := net.SplitHostPort(authority)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(authority, "[]")
}

func (b *Bridge) serveHTTPProxy(w http.ResponseWriter, r *http.Request) {
	_, _ = b.observeRequest(r)
	response, err := b.forward(r.Context(), r, requestAuthority(r))
	if err != nil {
		http.Error(w, "bridge could not reach qq.com", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	b.captureResponse(response, r)
	copyResponse(w, response)
}

func requestAuthority(req *http.Request) string {
	if req == nil {
		return ""
	}
	if req.URL != nil && strings.TrimSpace(req.URL.Host) != "" {
		return strings.TrimSpace(req.URL.Host)
	}
	return strings.TrimSpace(req.Host)
}

func (b *Bridge) serveHTTP1(conn net.Conn, host string, connectionCtx context.Context) {
	reader := bufio.NewReader(conn)
	for {
		request, err := http.ReadRequest(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				b.recordHTTP1ParseError()
			}
			return
		}
		endpointResponse := newBridgeEndpointResponseWriter()
		if b.serveBridgePageEndpoint(endpointResponse, request, host) {
			if err := endpointResponse.writeTo(conn); err != nil || request.Close {
				return
			}
			continue
		}
		_, _ = b.observeRequest(request)
		response, err := b.forward(connectionCtx, request, host)
		if err != nil {
			_ = writeHTTPError(conn, http.StatusBadGateway)
			return
		}
		b.captureResponse(response, request)
		err = response.Write(conn)
		response.Body.Close()
		if err != nil || request.Close || response.Close {
			return
		}
	}
}

func (b *Bridge) serveHTTP2(conn net.Conn, host string, connectionCtx context.Context) {
	server := &http2.Server{}
	server.ServeConn(conn, &http2.ServeConnOpts{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.serveBridgePageEndpoint(w, r, host) {
			return
		}
		_, _ = b.observeRequest(r)
		requestCtx, cancel := bridgeRequestContext(r.Context(), connectionCtx)
		defer cancel()
		response, err := b.forward(requestCtx, r, host)
		if err != nil {
			http.Error(w, "bridge could not reach qq.com", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		b.captureResponse(response, r)
		copyResponse(w, response)
	})})
}

// bridgeEndpointResponseWriter adapts a local endpoint response to the
// already-hijacked HTTP/1.1 tunnel without creating a second routing path.
type bridgeEndpointResponseWriter struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func newBridgeEndpointResponseWriter() *bridgeEndpointResponseWriter {
	return &bridgeEndpointResponseWriter{header: make(http.Header)}
}

func (w *bridgeEndpointResponseWriter) Header() http.Header { return w.header }

func (w *bridgeEndpointResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
}

func (w *bridgeEndpointResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

func (w *bridgeEndpointResponseWriter) writeTo(destination io.Writer) error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	response := &http.Response{
		StatusCode:    w.status,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        w.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(w.body.Bytes())),
		ContentLength: int64(w.body.Len()),
	}
	defer response.Body.Close()
	return response.Write(destination)
}

func bridgeRequestContext(requestCtx, connectionCtx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(requestCtx)
	stop := context.AfterFunc(connectionCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (b *Bridge) forward(ctx context.Context, request *http.Request, connectedHost string) (*http.Response, error) {
	b.mu.RLock()
	transport := b.transport
	running := b.phase == bridgeRunning
	b.mu.RUnlock()
	if transport == nil || !running {
		return nil, errors.New("bridge transport is not running")
	}
	host := strings.TrimSpace(connectedHost)
	if host == "" {
		host = request.URL.Hostname()
	}
	if host == "" {
		return nil, errors.New("request has no destination host")
	}
	outbound := request.Clone(ctx)
	outbound.RequestURI = ""
	outbound.URL = cloneURLForOrigin(request.URL, host)
	outbound.Host = host
	removeHopHeaders(outbound.Header)
	// Let the Go transport negotiate and transparently decode gzip. The
	// desktop client may advertise encodings this bridge cannot inspect; after
	// decoding, the same JSON body is safely forwarded back to it.
	outbound.Header.Del("Accept-Encoding")
	return transport.RoundTrip(outbound)
}

func cloneURLForOrigin(source *url.URL, host string) *url.URL {
	copy := &url.URL{Scheme: "https", Host: host, Path: "/"}
	if source == nil {
		return copy
	}
	if source.Scheme == "http" || source.Scheme == "https" {
		copy.Scheme = source.Scheme
	}
	copy.Path = source.Path
	if copy.Path == "" {
		copy.Path = "/"
	}
	copy.RawPath = source.RawPath
	copy.RawQuery = source.RawQuery
	return copy
}

func copyResponse(w http.ResponseWriter, response *http.Response) {
	removeHopHeaders(response.Header)
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func writeHTTPError(w io.Writer, status int) error {
	_, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", status, http.StatusText(status))
	return err
}

func removeHopHeaders(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				headers.Del(name)
			}
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		headers.Del(name)
	}
}

func (b *Bridge) trackConnection(conn net.Conn, cancel context.CancelFunc) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.phase != bridgeRunning {
		return false
	}
	b.connections[conn] = cancel
	return true
}

func (b *Bridge) untrackConnection(conn net.Conn) {
	b.mu.Lock()
	delete(b.connections, conn)
	b.mu.Unlock()
}

func (b *Bridge) closeConnections() {
	b.mu.Lock()
	connections := b.connections
	b.connections = make(map[net.Conn]context.CancelFunc)
	b.mu.Unlock()
	for conn, cancel := range connections {
		cancel()
		_ = conn.Close()
	}
}

func (b *Bridge) tlsConfigForHost(ca *tls.Certificate, host string) (*tls.Config, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	b.certificateMu.Lock()
	defer b.certificateMu.Unlock()
	b.mu.RLock()
	certificate, ok := b.certificates[host]
	running := b.phase == bridgeRunning
	b.mu.RUnlock()
	if !running {
		return nil, errors.New("bridge is stopping")
	}
	if !ok {
		config, err := leafTLSConfig(ca, host)
		if err != nil {
			return nil, err
		}
		certificate = config.Certificates[0]
		b.mu.Lock()
		if b.phase != bridgeRunning {
			b.mu.Unlock()
			return nil, errors.New("bridge is stopping")
		}
		if cached, exists := b.certificates[host]; exists {
			certificate = cached
		} else {
			if len(b.certificates) >= maxBridgeCertificates {
				oldest := b.certificateHosts[0]
				delete(b.certificates, oldest)
				b.certificateHosts = b.certificateHosts[1:]
			}
			b.certificates[host] = certificate
			b.certificateHosts = append(b.certificateHosts, host)
		}
		b.mu.Unlock()
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{"h2", "http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func leafTLSConfig(ca *tls.Certificate, host string) (*tls.Config, error) {
	if ca == nil || len(ca.Certificate) == 0 {
		return nil, errors.New("local bridge certificate is unavailable")
	}
	parent, err := x509.ParseCertificate(ca.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse local bridge certificate: %w", err)
	}
	signer, ok := ca.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("local bridge certificate key cannot sign host certificates")
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || (host != "qq.com" && !strings.HasSuffix(host, ".qq.com")) {
		return nil, errors.New("bridge certificate requested for non-qq.com host")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate intercepted host key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate intercepted host serial: %w", err)
	}
	now := time.Now()
	notAfter := now.AddDate(1, 0, 0)
	if parent.NotAfter.Before(notAfter) {
		notAfter = parent.NotAfter
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
	if err != nil {
		return nil, fmt.Errorf("sign intercepted host certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der, ca.Certificate[0]}, PrivateKey: key}}}, nil
}

func (b *Bridge) captureResponse(resp *http.Response, req *http.Request) *http.Response {
	if resp == nil || req == nil {
		return resp
	}
	b.captureDiagnosticMetadata(req, resp)
	if !isJSONResponse(resp) || resp.Body == nil {
		return b.injectPageCapture(resp, req)
	}
	b.mu.Lock()
	b.captureStats.JSONResponses++
	b.mu.Unlock()
	originalBody := resp.Body
	body, err := io.ReadAll(io.LimitReader(originalBody, maxCapturedBody+1))
	if err != nil || len(body) > maxCapturedBody {
		b.mu.Lock()
		b.captureStats.ResponseReadFailures++
		b.mu.Unlock()
		// The proxy must remain transparent when a response cannot be safely
		// inspected. Replay the bytes already consumed, then continue from the
		// original stream instead of returning a closed/truncated response.
		resp.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(body), originalBody), Closer: originalBody}
		return b.injectPageCapture(resp, req)
	}
	_ = originalBody.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.TransferEncoding = nil
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	identities := videoIdentityCountFromJSON(body)
	b.mu.Lock()
	b.captureStats.JSONResponseIdentities += uint64(identities)
	b.mu.Unlock()
	// The desktop client may change its internal endpoint names. The stable
	// product boundary is the complete video feed returned to the client, not
	// an implementation-specific request path or request payload. Capture only
	// a response that contains exactly one complete video feed; list responses
	// and non-video feeds remain ineligible.
	if video, ok := activeVideoFromJSON(body); ok {
		b.mu.Lock()
		b.captureStats.CapturedResponses++
		b.mu.Unlock()
		b.recordActiveVideo(video)
	}
	return b.injectPageCapture(resp, req)
}

func (b *Bridge) captureDiagnosticMetadata(req *http.Request, resp *http.Response) {
	path := strings.TrimSpace(b.options.DiagnosticMetadataPath)
	if path == "" || req.URL == nil || !isChannelsAPIRequest(req) {
		return
	}
	record, err := json.Marshal(diagnosticRequestMetadata{
		Host:              requestHost(req),
		Path:              req.URL.Path,
		Method:            req.Method,
		ResponseStatus:    resp.StatusCode,
		ContentType:       resp.Header.Get("Content-Type"),
		ContentLength:     resp.ContentLength,
		TransportProtocol: resp.Proto,
	})
	if err != nil {
		return
	}
	record = append(record, '\n')

	b.mu.Lock()
	defer b.mu.Unlock()
	flags := os.O_WRONLY | os.O_APPEND
	if !b.metadataCaptured {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err == nil {
		_, err = file.Write(record)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		b.captureStats.DiagnosticWriteFailures++
		return
	}
	b.metadataCaptured = true
	b.captureStats.DiagnosticMetadata++
}

// injectPageCapture keeps normal bridge operation transparent. The page adapter
// is an explicit opt-in: it saves any requested diagnostic copy first, then
// injects a passive same-origin reporter and gives every JavaScript module in
// the Vite graph one session version. The reporter does not patch fetch, XHR,
// navigation, or any WebView global; the detail module calls it after its own
// request resolves.
func (b *Bridge) injectPageCapture(resp *http.Response, req *http.Request) *http.Response {
	if resp == nil || req == nil || resp.Body == nil {
		return resp
	}
	host := requestHost(req)
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	isPage := host == "channels.weixin.qq.com" && strings.Contains(contentType, "text/html")
	if isPage {
		return b.rewritePageResponse(resp, maxDiagnosticPageHTML, func(body []byte) ([]byte, bool) {
			if strings.TrimSpace(b.options.PageHTMLCapturePath) != "" {
				b.captureDiagnosticPageHTML(body)
			}
			if !b.options.EnablePageAdapter {
				return body, false
			}
			updated, changed := pageCaptureInjection(string(body))
			return []byte(updated), changed
		})
	}
	if !b.options.EnablePageAdapter || !isPageAdapterModuleRequest(req, contentType) {
		return resp
	}
	return b.rewritePageResponse(resp, maxCapturedBody, func(body []byte) ([]byte, bool) {
		updated, graphChanged := pageModuleGraphInjection(string(body))
		updated, detailChanged := pageDetailModuleInjection(updated)
		return []byte(updated), graphChanged || detailChanged
	})
}

func isPageAdapterModuleRequest(req *http.Request, contentType string) bool {
	if requestHost(req) != "res.wx.qq.com" || req.URL == nil || req.URL.Query().Get(bridgeModuleVersionKey) != bridgeModuleVersion {
		return false
	}
	return strings.Contains(contentType, "javascript") || strings.HasSuffix(strings.ToLower(req.URL.Path), ".js")
}

func (b *Bridge) rewritePageResponse(resp *http.Response, limit int64, rewrite func([]byte) ([]byte, bool)) *http.Response {
	originalBody := resp.Body
	body, err := io.ReadAll(io.LimitReader(originalBody, limit+1))
	if err != nil || int64(len(body)) > limit {
		b.mu.Lock()
		b.captureStats.ResponseReadFailures++
		b.mu.Unlock()
		resp.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(body), originalBody), Closer: originalBody}
		return resp
	}
	updated, changed := rewrite(body)
	_ = originalBody.Close()
	resp.Body = io.NopCloser(bytes.NewReader(updated))
	resp.ContentLength = int64(len(updated))
	resp.TransferEncoding = nil
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(updated)))
	if changed {
		b.mu.Lock()
		b.captureStats.PageAdapterRewrites++
		b.mu.Unlock()
	}
	return resp
}

// captureDiagnosticPageHTML saves the first original Channels document only
// when the user explicitly selected an output path. It deliberately writes the
// unmodified response so subsequent analysis can distinguish page behavior
// from bridge injection. The file is mode 0600 and must not already exist.
func (b *Bridge) captureDiagnosticPageHTML(body []byte) {
	path := strings.TrimSpace(b.options.PageHTMLCapturePath)
	if path == "" {
		return
	}
	b.mu.Lock()
	if b.pageHTMLCaptured {
		b.mu.Unlock()
		return
	}
	b.pageHTMLCaptured = true
	b.mu.Unlock()

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = file.Write(body)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err == nil {
		return
	}
	_ = os.Remove(path)
	b.mu.Lock()
	b.pageHTMLCaptured = false
	b.captureStats.ResponseReadFailures++
	b.mu.Unlock()
}

func pageCaptureInjection(source string) (string, bool) {
	const tag = `<script src="` + bridgePageScriptPath + `"></script>`
	if strings.Contains(source, bridgePageScriptPath) {
		return source, false
	}
	location := pageHeadTag.FindStringIndex(source)
	if location == nil {
		return source, false
	}
	updated := source[:location[1]] + tag + source[location[1]:]
	updated, _ = rewritePageModuleURL(updated, pageModuleSource)
	return updated, true
}

func rewritePageModuleURL(source string, matcher *regexp.Regexp) (string, bool) {
	changed := false
	updated := matcher.ReplaceAllStringFunc(source, func(match string) string {
		parts := matcher.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		moduleURL, err := url.Parse(parts[2])
		if err != nil {
			return match
		}
		query := moduleURL.Query()
		if query.Get(bridgeModuleVersionKey) == bridgeModuleVersion {
			return match
		}
		query.Set(bridgeModuleVersionKey, bridgeModuleVersion)
		moduleURL.RawQuery = query.Encode()
		changed = true
		return parts[1] + moduleURL.String() + parts[3]
	})
	return updated, changed
}

func pageModuleGraphInjection(source string) (string, bool) {
	return rewritePageModuleURL(source, pageModuleImport)
}

func pageDetailModuleInjection(source string) (string, bool) {
	if strings.Contains(source, "__digcrawlWechatChannelsReport") {
		return source, false
	}
	updated := finderGetCommentDetail.ReplaceAllString(source, `async finderGetCommentDetail($1){var result=await(async()=>{$2})();var feed=result&&result.data&&result.data.object;if(typeof window!=="undefined"&&typeof window.__digcrawlWechatChannelsReport==="function"){window.__digcrawlWechatChannelsReport(feed)}return result}async`)
	return updated, updated != source
}

func videoIdentityCountFromJSON(raw []byte) int {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0
	}
	identities := make(map[videoIdentity]struct{})
	visitJSON(value, func(object map[string]any) {
		if video, ok := activeVideoFromObject(object); ok {
			identities[videoIdentity{objectID: video.ObjectID, objectNonceID: video.ObjectNonceID}] = struct{}{}
		}
	})
	return len(identities)
}

func (b *Bridge) recordActiveVideo(video activeVideoResponse) {
	b.recordActiveVideoWithDownload(video, activeDownloadResource{})
}

func (b *Bridge) recordActiveVideoWithDownload(video activeVideoResponse, resource activeDownloadResource) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.activeAmbiguous {
		return
	}
	if b.active.ObjectID == "" {
		b.active = video
		b.activeDownload = resource
		return
	}
	if b.active.ObjectID != video.ObjectID || b.active.ObjectNonceID != video.ObjectNonceID {
		b.active = activeVideoResponse{}
		b.activeDownload = activeDownloadResource{}
		b.activeAmbiguous = true
		return
	}
	// The same detail request may be answered more than once. Keep the latest
	// metadata while preserving its unambiguous video identity.
	b.active = video
	if resource.URL != "" {
		b.activeDownload = resource
	}
}

type videoIdentity struct {
	objectID      string
	objectNonceID string
}

func (identity videoIdentity) valid() bool {
	return identity.objectID != "" && identity.objectNonceID != ""
}

type replayBody struct {
	io.Reader
	io.Closer
}

func (b *Bridge) observeRequest(req *http.Request) (videoIdentity, bool) {
	if req == nil {
		return videoIdentity{}, false
	}
	b.mu.Lock()
	b.captureStats.HTTPRequests++
	b.mu.Unlock()
	if !isChannelsAPIRequest(req) {
		return videoIdentity{}, false
	}
	b.mu.Lock()
	b.captureStats.CandidateRequests++
	b.mu.Unlock()
	if !isJSONRequest(req) {
		return videoIdentity{}, false
	}
	b.mu.Lock()
	b.captureStats.JSONRequests++
	b.mu.Unlock()
	if req.Body == nil || req.ContentLength > maxCapturedBody {
		return videoIdentity{}, false
	}
	originalBody := req.Body
	body, err := io.ReadAll(io.LimitReader(originalBody, maxCapturedBody+1))
	// Inspection is optional, but forwarding is not. Restore every byte read
	// even when the source returned a partial-read error.
	req.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(body), originalBody), Closer: originalBody}
	if err != nil {
		b.mu.Lock()
		b.captureStats.RequestReadFailures++
		b.mu.Unlock()
		return videoIdentity{}, false
	}
	if len(body) > maxCapturedBody {
		return videoIdentity{}, false
	}
	identity, ok := videoIdentityFromRequest(body)
	if !ok {
		return videoIdentity{}, false
	}
	b.mu.Lock()
	b.captureStats.IdentityRequests++
	b.mu.Unlock()
	return identity, true
}

func isChannelsAPIRequest(req *http.Request) bool {
	host := requestHost(req)
	if host != "qq.com" && !strings.HasSuffix(host, ".qq.com") {
		return false
	}
	path := strings.ToLower(req.URL.Path)
	return strings.Contains(path, "finder") || strings.Contains(path, "channels")
}

func isJSONResponse(resp *http.Response) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json")
}

func isJSONRequest(req *http.Request) bool {
	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	return strings.Contains(contentType, "application/json") || strings.Contains(contentType, "+json")
}

func activeVideoFromJSON(raw []byte) (activeVideoResponse, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return activeVideoResponse{}, false
	}
	candidates := make(map[videoIdentity]activeVideoResponse)
	visitJSON(value, func(object map[string]any) {
		candidate, ok := activeVideoFromObject(object)
		if ok {
			identity := videoIdentity{objectID: candidate.ObjectID, objectNonceID: candidate.ObjectNonceID}
			candidates[identity] = candidate
		}
	})
	if len(candidates) != 1 {
		return activeVideoResponse{}, false
	}
	for _, candidate := range candidates {
		return candidate, true
	}
	return activeVideoResponse{}, false
}

func videoIdentityFromRequest(raw []byte) (videoIdentity, bool) {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return videoIdentity{}, false
	}
	identity := videoIdentity{
		objectID:      firstString(object, "object_id", "objectId", "objectid", "id"),
		objectNonceID: firstString(object, "object_nonce_id", "objectNonceId", "objectnonceid", "nonce_id", "nonceId"),
	}
	if !identity.valid() {
		return videoIdentity{}, false
	}
	return identity, true
}

func visitJSON(value any, visit func(map[string]any)) {
	switch current := value.(type) {
	case map[string]any:
		visit(current)
		for _, child := range current {
			visitJSON(child, visit)
		}
	case []any:
		for _, child := range current {
			visitJSON(child, visit)
		}
	}
}

func activeVideoFromObject(object map[string]any) (activeVideoResponse, bool) {
	id := firstString(object, "object_id", "objectId", "objectid", "id")
	nonce := firstString(object, "object_nonce_id", "objectNonceId", "objectnonceid", "nonce_id", "nonceId")
	if id == "" || nonce == "" {
		return activeVideoResponse{}, false
	}
	desc, ok := object["objectDesc"].(map[string]any)
	if !ok {
		return activeVideoResponse{}, false
	}
	mediaType, ok := desc["mediaType"].(float64)
	if !ok || mediaType != 4 {
		return activeVideoResponse{}, false
	}
	active := activeVideoResponse{
		ObjectID:      id,
		ObjectNonceID: nonce,
		Title:         firstString(object, "title", "description"),
		Description:   firstString(object, "description", "title"),
		PublishedAt:   firstString(object, "published_at", "publishTime", "createtime", "createTime"),
	}
	if contact, ok := object["contact"].(map[string]any); ok {
		active.AuthorID = firstString(contact, "username", "id")
		active.AuthorName = firstString(contact, "nickname", "name")
	}
	if active.Title == "" {
		active.Title = firstString(desc, "description", "title")
	}
	if active.Description == "" {
		active.Description = firstString(desc, "description", "title")
	}
	if media, ok := desc["media"].([]any); ok && len(media) > 0 {
		if first, ok := media[0].(map[string]any); ok {
			active.CoverURL = firstString(first, "coverUrl", "cover_url")
		}
	}
	return active, true
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			switch typed := value.(type) {
			case string:
				if value := strings.TrimSpace(typed); value != "" {
					return value
				}
			case json.Number:
				return typed.String()
			case float64:
				return strconv.FormatInt(int64(typed), 10)
			}
		}
	}
	return ""
}

func (b *Bridge) resolveService(ctx context.Context) (string, error) {
	if service := strings.TrimSpace(b.options.NetworkService); service != "" {
		return service, nil
	}
	for _, key := range []string{"State:/Network/Global/IPv4", "State:/Network/Global/IPv6"} {
		serviceID, err := b.scutilField(ctx, key, "PrimaryService")
		if err != nil {
			return "", err
		}
		if serviceID == "" {
			continue
		}
		if !isServiceID(serviceID) {
			return "", fmt.Errorf("identify active network service: unexpected service identifier")
		}
		service, err := b.scutilField(ctx, "Setup:/Network/Service/"+serviceID, "UserDefinedName")
		if err != nil {
			return "", err
		}
		if service != "" {
			return service, nil
		}
	}
	return "", fmt.Errorf("identify active network service: no primary service; pass --network-service")
}

func (b *Bridge) scutilField(ctx context.Context, key, field string) (string, error) {
	output, err := b.env.run.RunInput(ctx, "scutil", "show "+key+"\n")
	if err != nil {
		return "", fmt.Errorf("identify active network service: %w", commandError("scutil show", output, err))
	}
	for _, line := range strings.Split(string(output), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(name) == field {
			return strings.TrimSpace(value), nil
		}
	}
	return "", nil
}

func isServiceID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, runeValue := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if runeValue != '-' {
				return false
			}
			continue
		}
		if !('0' <= runeValue && runeValue <= '9') && !('a' <= runeValue && runeValue <= 'f') && !('A' <= runeValue && runeValue <= 'F') {
			return false
		}
	}
	return true
}

// preparePAC preserves the existing manual proxy selection for every
// non-qq.com request. An existing PAC cannot be composed safely, so it is
// deliberately left untouched and the bridge refuses to start.
func (b *Bridge) preparePAC(ctx context.Context, service string) (string, autoProxyConfig, error) {
	auto, err := b.readAutoProxy(ctx, service)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	if auto.Enabled {
		return "", autoProxyConfig{}, fmt.Errorf("refusing to replace an existing proxy auto-configuration on %q", service)
	}
	discoveryEnabled, err := b.proxyAutoDiscoveryEnabled(ctx, service)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	if discoveryEnabled {
		return "", autoProxyConfig{}, fmt.Errorf("refusing to replace existing proxy auto-discovery on %q", service)
	}
	web, err := b.readProxy(ctx, "-getwebproxy", service)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	secure, err := b.readProxy(ctx, "-getsecurewebproxy", service)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	socks, err := b.readProxy(ctx, "-getsocksfirewallproxy", service)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	if (web.Enabled && web.Authenticated) || (secure.Enabled && secure.Authenticated) || (socks.Enabled && socks.Authenticated) {
		return "", autoProxyConfig{}, fmt.Errorf("refusing to bridge through an authenticated manual proxy; credentials are never read or requested")
	}
	bypass, err := b.readProxyBypassDomains(ctx, service)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	pac, err := makePAC(b.options.Port, web, secure, socks, bypass)
	if err != nil {
		return "", autoProxyConfig{}, err
	}
	b.webProxy = web
	b.secureProxy = secure
	b.socksProxy = socks
	b.bypassDomains = append([]string(nil), bypass...)
	return pac, auto, nil
}

// readProxyBypassDomains captures the current manual-proxy exclusions before
// switching to PAC mode. PAC is only safe here when it can reproduce every
// configured exclusion; otherwise Start refuses rather than changing routing.
func (b *Bridge) readProxyBypassDomains(ctx context.Context, service string) ([]string, error) {
	output, err := b.env.run.Run(ctx, "networksetup", "-getproxybypassdomains", service)
	if err != nil {
		return nil, fmt.Errorf("read system proxy bypass domains: %w", commandError("networksetup -getproxybypassdomains", output, err))
	}
	var domains []string
	for _, line := range strings.Split(string(output), "\n") {
		value := strings.TrimSpace(line)
		if value != "" {
			domains = append(domains, value)
		}
	}
	return domains, nil
}

func (b *Bridge) readProxy(ctx context.Context, command, service string) (proxyConfig, error) {
	output, err := b.env.run.Run(ctx, "networksetup", command, service)
	if err != nil {
		return proxyConfig{}, fmt.Errorf("read system proxy: %w", commandError("networksetup "+command, output, err))
	}
	return parseProxyConfig(string(output))
}

func parseProxyConfig(output string) (proxyConfig, error) {
	var config proxyConfig
	var portSeen bool
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "enabled":
			config.Enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "authenticated proxy enabled":
			config.Authenticated = strings.EqualFold(strings.TrimSpace(value), "yes") || strings.TrimSpace(value) == "1"
		case "server":
			config.Server = strings.TrimSpace(value)
		case "port":
			if value := strings.TrimSpace(value); value != "" {
				port, err := strconv.Atoi(value)
				if err != nil || port < 0 || port > 65535 {
					return proxyConfig{}, fmt.Errorf("invalid proxy port %q", value)
				}
				config.Port = port
				portSeen = true
			}
		}
	}
	if config.Enabled && (!portSeen || config.Port < 1) {
		return proxyConfig{}, fmt.Errorf("enabled proxy has no valid port")
	}
	return config, nil
}

func (b *Bridge) outboundProxy(req *http.Request) (*url.URL, error) {
	if req != nil && req.URL != nil && matchesProxyBypass(req.URL.Hostname(), b.bypassDomains) {
		return nil, nil
	}
	config := b.webProxy
	if req != nil && req.URL != nil && strings.EqualFold(req.URL.Scheme, "https") {
		config = b.secureProxy
	}
	useSOCKS := !config.Enabled
	if useSOCKS {
		config = b.socksProxy
	}
	if !config.Enabled {
		return nil, nil
	}
	if config.Authenticated || config.Port < 1 || !safePACHost(config.Server) {
		return nil, fmt.Errorf("invalid original manual proxy configuration")
	}
	scheme := "http"
	if useSOCKS {
		scheme = "socks5"
	}
	return &url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.TrimSpace(config.Server), strconv.Itoa(config.Port))}, nil
}

func matchesProxyBypass(host string, domains []string) bool {
	host = strings.ToLower(strings.TrimSpace(strings.Trim(host, "[]")))
	if host == "" {
		return false
	}
	for _, raw := range domains {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			ip, network, err := net.ParseCIDR(value)
			candidate := net.ParseIP(host)
			if err == nil && ip.To4() != nil && candidate != nil && network.Contains(candidate) {
				return true
			}
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			if candidate := net.ParseIP(host); candidate != nil && candidate.Equal(ip) {
				return true
			}
			continue
		}
		if strings.HasPrefix(value, "*.") {
			if strings.HasSuffix(host, strings.TrimPrefix(value, "*")) {
				return true
			}
			continue
		}
		if host == value || strings.HasSuffix(host, "."+value) {
			return true
		}
	}
	return false
}

func (b *Bridge) readAutoProxy(ctx context.Context, service string) (autoProxyConfig, error) {
	output, err := b.env.run.Run(ctx, "networksetup", "-getautoproxyurl", service)
	if err != nil {
		return autoProxyConfig{}, fmt.Errorf("read proxy auto-configuration: %w", commandError("networksetup -getautoproxyurl", output, err))
	}
	return parseAutoProxyConfig(string(output))
}

func parseAutoProxyConfig(output string) (autoProxyConfig, error) {
	var config autoProxyConfig
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "enabled":
			config.Enabled = strings.EqualFold(strings.TrimSpace(value), "yes")
		case "url":
			config.URL = strings.TrimSpace(value)
		}
	}
	if config.Enabled && config.URL == "" {
		return autoProxyConfig{}, fmt.Errorf("enabled proxy auto-configuration has no URL")
	}
	return config, nil
}

func (b *Bridge) proxyAutoDiscoveryEnabled(ctx context.Context, service string) (bool, error) {
	output, err := b.env.run.Run(ctx, "networksetup", "-getproxyautodiscovery", service)
	if err != nil {
		return false, fmt.Errorf("read proxy auto-discovery: %w", commandError("networksetup -getproxyautodiscovery", output, err))
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "enabled":
			return strings.EqualFold(strings.TrimSpace(value), "yes"), nil
		case "auto proxy discovery":
			return strings.EqualFold(strings.TrimSpace(value), "on"), nil
		}
	}
	return false, nil
}

func (b *Bridge) enablePAC(ctx context.Context, service string) error {
	output, err := b.env.run.Run(ctx, "networksetup", "-setautoproxyurl", service, bridgePACURL(b.options.Port))
	if err != nil {
		return fmt.Errorf("enable proxy auto-configuration: %w", commandError("networksetup -setautoproxyurl", output, err))
	}
	return nil
}

func (b *Bridge) restoreProxy(ctx context.Context, state bridgeState) error {
	if state.ProxyMode == "pac" {
		return b.restorePAC(ctx, state)
	}
	return b.disableOwnProxy(ctx, state.Service)
}

func (b *Bridge) restorePAC(ctx context.Context, state bridgeState) error {
	current, err := b.readAutoProxy(ctx, state.Service)
	if err != nil {
		return err
	}
	if !current.Enabled || current.URL != bridgePACURL(state.Port) {
		return nil
	}
	if state.AutoProxyEnabled {
		output, err := b.env.run.Run(ctx, "networksetup", "-setautoproxyurl", state.Service, state.AutoProxyURL)
		if err != nil {
			return fmt.Errorf("restore proxy auto-configuration: %w", commandError("networksetup -setautoproxyurl", output, err))
		}
		return nil
	}
	output, err := b.env.run.Run(ctx, "networksetup", "-setautoproxystate", state.Service, "off")
	if err != nil {
		return fmt.Errorf("disable proxy auto-configuration: %w", commandError("networksetup -setautoproxystate", output, err))
	}
	return nil
}

func (b *Bridge) disableOwnProxy(ctx context.Context, service string) error {
	var errs []error
	for _, item := range []struct{ get, disable string }{{"-getwebproxy", "-setwebproxystate"}, {"-getsecurewebproxy", "-setsecurewebproxystate"}} {
		proxy, err := b.readProxy(ctx, item.get, service)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !proxy.Enabled || !isOwnProxy(proxy, b.options.Port) {
			continue
		}
		output, err := b.env.run.Run(ctx, "networksetup", item.disable, service, "off")
		if err != nil {
			errs = append(errs, fmt.Errorf("disable system proxy: %w", commandError("networksetup "+item.disable, output, err)))
		}
	}
	return errors.Join(errs...)
}

func isOwnProxy(config proxyConfig, port int) bool {
	return config.Enabled && config.Port == port && net.ParseIP(strings.TrimSpace(config.Server)) != nil && net.ParseIP(strings.TrimSpace(config.Server)).IsLoopback()
}

func bridgePACURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + bridgePACPath
}

func makePAC(port int, web, secure, socks proxyConfig, bypassDomains []string) (string, error) {
	webDirective, err := pacDirective("PROXY", web)
	if err != nil {
		return "", fmt.Errorf("use existing HTTP proxy in PAC: %w", err)
	}
	secureDirective, err := pacDirective("PROXY", secure)
	if err != nil {
		return "", fmt.Errorf("use existing HTTPS proxy in PAC: %w", err)
	}
	socksDirective, err := pacDirective("SOCKS", socks)
	if err != nil {
		return "", fmt.Errorf("use existing SOCKS proxy in PAC: %w", err)
	}
	if webDirective == "DIRECT" {
		webDirective = socksDirective
	}
	if secureDirective == "DIRECT" {
		secureDirective = socksDirective
	}
	bypassCondition, err := pacBypassCondition(bypassDomains)
	if err != nil {
		return "", fmt.Errorf("use existing proxy bypass domains in PAC: %w", err)
	}
	return fmt.Sprintf(`function FindProxyForURL(url, host) {
  host = host.toLowerCase();
	if (host === "localhost" || host === "127.0.0.1" || host === "::1") {
	  return "DIRECT";
	}
  if (host === "qq.com" || shExpMatch(host, "*.qq.com")) {
    return "PROXY 127.0.0.1:%d";
  }
	if (%s) {
	  return "DIRECT";
	}
  if (url.substring(0, 6).toLowerCase() === "https:") {
    return "%s";
  }
  return "%s";
}
`, port, bypassCondition, secureDirective, webDirective), nil
}

func pacBypassCondition(domains []string) (string, error) {
	conditions := make([]string, 0, len(domains))
	for _, raw := range domains {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			ip, network, err := net.ParseCIDR(value)
			if err != nil || ip.To4() == nil || network.IP.To4() == nil {
				return "", fmt.Errorf("unsupported bypass network %q", raw)
			}
			conditions = append(conditions, fmt.Sprintf("isInNet(host, %q, %q)", network.IP.String(), net.IP(network.Mask).String()))
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			conditions = append(conditions, fmt.Sprintf("host === %q", ip.String()))
			continue
		}
		if strings.HasPrefix(value, "*.") && safePACHost(strings.TrimPrefix(value, "*.")) {
			conditions = append(conditions, fmt.Sprintf("shExpMatch(host, %q)", value))
			continue
		}
		if !safePACHost(value) {
			return "", fmt.Errorf("unsupported bypass domain %q", raw)
		}
		conditions = append(conditions, fmt.Sprintf("host === %q || dnsDomainIs(host, %q)", value, "."+value))
	}
	if len(conditions) == 0 {
		return "false", nil
	}
	return strings.Join(conditions, " || "), nil
}

func pacDirective(kind string, config proxyConfig) (string, error) {
	if !config.Enabled {
		return "DIRECT", nil
	}
	if config.Port < 1 || !safePACHost(config.Server) {
		return "", fmt.Errorf("unsupported proxy address")
	}
	host := strings.TrimSpace(config.Server)
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return kind + " " + host + ":" + strconv.Itoa(config.Port), nil
}

func safePACHost(raw string) bool {
	host := strings.TrimSpace(raw)
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, runeValue := range host {
		if !('a' <= runeValue && runeValue <= 'z') && !('A' <= runeValue && runeValue <= 'Z') && !('0' <= runeValue && runeValue <= '9') && runeValue != '-' && runeValue != '.' {
			return false
		}
	}
	return true
}

func (b *Bridge) installCertificate(ctx context.Context) error {
	certPath := filepath.Join(b.options.StateDir, bridgeCACertFile)
	keychain := defaultLoginKeychain()
	// Omit -d: it targets the admin trust store. The bridge is explicitly a
	// per-user capability, so trust settings stay in the user's login keychain.
	output, err := b.env.run.Run(ctx, "security", "add-trusted-cert", "-r", "trustRoot", "-k", keychain, certPath)
	if err != nil {
		return fmt.Errorf("install local bridge certificate: %w", commandError("security add-trusted-cert", output, err))
	}
	return nil
}

func (b *Bridge) removeCertificate(ctx context.Context) error {
	b.mu.RLock()
	fingerprint := b.state.CertificateSHA1
	b.mu.RUnlock()
	if fingerprint == "" {
		return nil
	}
	output, err := b.env.run.Run(ctx, "security", "delete-certificate", "-Z", fingerprint, "-t", defaultLoginKeychain())
	if err != nil {
		return fmt.Errorf("remove local bridge certificate: %w", commandError("security delete-certificate", output, err))
	}
	return nil
}

func (b *Bridge) certificatePresent(ctx context.Context) (bool, error) {
	b.mu.RLock()
	fingerprint := b.state.CertificateSHA1
	b.mu.RUnlock()
	output, err := b.env.run.Run(ctx, "security", "find-certificate", "-a", "-Z", "-c", bridgeCACommonName, defaultLoginKeychain())
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "could not be found") {
			return false, nil
		}
		return false, fmt.Errorf("check local bridge certificate: %w", commandError("security find-certificate", output, err))
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "SHA-1 hash") && strings.EqualFold(strings.TrimSpace(value), fingerprint) {
			return true, nil
		}
	}
	return false, nil
}

func defaultLoginKeychain() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "login.keychain-db"
	}
	return filepath.Join(home, "Library", "Keychains", "login.keychain-db")
}

func openWeChat(ctx context.Context, appPath string) error {
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("locate WeChat app %q: %w", appPath, err)
	}
	if output, err := exec.CommandContext(ctx, "open", "-a", appPath).CombinedOutput(); err != nil {
		return fmt.Errorf("open WeChat: %w", commandError("open -a", output, err))
	}
	return nil
}

func commandError(command string, output []byte, err error) error {
	if message := strings.TrimSpace(string(output)); message != "" {
		return fmt.Errorf("%s: %w", message, err)
	}
	return fmt.Errorf("%s: %w", command, err)
}

func writeBridgeState(path string, state bridgeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func readBridgeState(path string) (bridgeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bridgeState{}, err
	}
	var state bridgeState
	if err := json.Unmarshal(data, &state); err != nil {
		return bridgeState{}, err
	}
	if state.Service == "" || state.Port < 1 || state.Port > 65535 {
		return bridgeState{}, fmt.Errorf("invalid bridge recovery state")
	}
	if state.StateMachine {
		if state.ProxyMode != "pac" || state.ControlToken == "" || state.OwnerPID < 1 {
			return bridgeState{}, fmt.Errorf("invalid bridge recovery state")
		}
		return state, nil
	}
	if state.LegacyVersion == 1 {
		state.ProxyMode = "manual"
	}
	if state.LegacyVersion != 1 && state.LegacyVersion != 2 {
		return bridgeState{}, fmt.Errorf("invalid bridge recovery state")
	}
	if state.LegacyVersion == 2 && state.ProxyMode != "pac" {
		return bridgeState{}, fmt.Errorf("invalid bridge recovery state")
	}
	return state, nil
}

func newControlToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func loadOrCreateCA(stateDir string) (*tls.Certificate, string, error) {
	certPath := filepath.Join(stateDir, bridgeCACertFile)
	keyPath := filepath.Join(stateDir, bridgeCAKeyFile)
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		fingerprint, err := certificateFingerprint(cert.Certificate)
		if err != nil {
			return nil, "", err
		}
		return &cert, fingerprint, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("load local bridge certificate: %w", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", fmt.Errorf("generate local bridge certificate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", fmt.Errorf("generate local bridge certificate serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: bridgeCACommonName}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, "", fmt.Errorf("create local bridge certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, "", fmt.Errorf("write local bridge certificate: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, "", fmt.Errorf("write local bridge certificate key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, "", fmt.Errorf("load generated local bridge certificate: %w", err)
	}
	fingerprint, err := certificateFingerprint(cert.Certificate)
	if err != nil {
		return nil, "", err
	}
	return &cert, fingerprint, nil
}

func certificateFingerprint(chain [][]byte) (string, error) {
	if len(chain) == 0 {
		return "", fmt.Errorf("local bridge certificate has no leaf")
	}
	if _, err := x509.ParseCertificate(chain[0]); err != nil {
		return "", fmt.Errorf("parse local bridge certificate: %w", err)
	}
	sum := sha1.Sum(chain[0])
	return strings.ToUpper(fmt.Sprintf("%X", sum[:])), nil
}
