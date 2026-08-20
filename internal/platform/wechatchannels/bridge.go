package wechatchannels

import (
	"bytes"
	"context"
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

	"github.com/elazarl/goproxy"
)

const (
	DefaultBridgePort     = 20261
	defaultBridgeStateDir = "storage/digcrawl/wechat-channels-bridge"
	bridgeStateFile       = "bridge-state.json"
	bridgeCACertFile      = "bridge-ca.pem"
	bridgeCAKeyFile       = "bridge-ca-key.pem"
	bridgeCACommonName    = "DigCrawl WeChat Channels Local Bridge CA"
	bridgePACPath         = "/v1/wechat-channels/proxy.pac"
	bridgeStopPath        = "/v1/wechat-channels/stop"
	bridgeTokenHeader     = "X-DigCrawl-Bridge-Token"
	maxCapturedBody       = 2 << 20
	remoteStopTimeout     = 10 * time.Second
)

// BridgeOptions controls the local macOS bridge. The bridge only listens on
// loopback, intercepts TLS for qq.com, and retains only the current video's
// metadata in memory.
type BridgeOptions struct {
	Port           int
	StateDir       string
	NetworkService string
	WeChatAppPath  string
}

// Bridge is the explicit-lifecycle local proxy and active-video endpoint used
// by the import command. It never writes WeChat request or response payloads.
type Bridge struct {
	options BridgeOptions
	env     bridgeEnvironment

	mu     sync.RWMutex
	active activeVideoResponse

	server        *http.Server
	listener      net.Listener
	state         bridgeState
	pac           string
	webProxy      proxyConfig
	secureProxy   proxyConfig
	socksProxy    proxyConfig
	bypassDomains []string

	stopRequested     chan struct{}
	stopRequestedOnce sync.Once
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
	LegacyVersion        int    `json:"version,omitempty"`
	StateMachine         bool   `json:"state_machine,omitempty"`
	Service              string `json:"service"`
	Port                 int    `json:"port"`
	CertificateSHA1      string `json:"certificate_sha1"`
	CertificateInstalled bool   `json:"certificate_installed"`
	ProxyEnabled         bool   `json:"proxy_enabled"`
	ProxyMode            string `json:"proxy_mode,omitempty"`
	AutoProxyEnabled     bool   `json:"auto_proxy_enabled_before,omitempty"`
	AutoProxyURL         string `json:"auto_proxy_url_before,omitempty"`
	ControlToken         string `json:"control_token,omitempty"`
	OwnerPID             int    `json:"owner_pid,omitempty"`
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
	return newBridge(options, defaultBridgeEnvironment()), nil
}

func newBridge(options BridgeOptions, env bridgeEnvironment) *Bridge {
	return &Bridge{options: options, env: env}
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
	if b.server != nil {
		return fmt.Errorf("wechat channels bridge is already running")
	}
	statePath := b.statePath()
	if _, err := b.env.readState(statePath); err == nil {
		return fmt.Errorf("a previous bridge session is still recorded; run `digcrawl wechat-channels bridge stop` first")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read bridge state: %w", err)
	}

	if err := os.MkdirAll(b.options.StateDir, 0o700); err != nil {
		return fmt.Errorf("create bridge state directory: %w", err)
	}
	cert, fingerprint, err := b.env.loadCA(b.options.StateDir)
	if err != nil {
		return err
	}
	service, err := b.resolveService(ctx)
	if err != nil {
		return err
	}
	pac, autoProxy, err := b.preparePAC(ctx, service)
	if err != nil {
		return err
	}
	b.pac = pac
	controlToken, err := newControlToken()
	if err != nil {
		return fmt.Errorf("generate bridge control token: %w", err)
	}
	b.stopRequested = make(chan struct{})
	b.stopRequestedOnce = sync.Once{}
	b.state = bridgeState{StateMachine: true, Service: service, Port: b.options.Port, CertificateSHA1: fingerprint, ProxyMode: "pac", AutoProxyEnabled: autoProxy.Enabled, AutoProxyURL: autoProxy.URL, ControlToken: controlToken, OwnerPID: os.Getpid()}
	if err := b.env.writeState(statePath, b.state); err != nil {
		return fmt.Errorf("write bridge recovery state: %w", err)
	}

	certificatePresent, err := b.certificatePresent(ctx)
	if err != nil {
		_ = b.env.remove(statePath)
		return err
	}
	if !certificatePresent {
		// Persist cleanup intent before changing the keychain. If the process
		// stops during security(1), bridge stop can still remove this exact CA.
		b.state.CertificateInstalled = true
		if err := b.env.writeState(statePath, b.state); err != nil {
			_ = b.env.remove(statePath)
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
	b.listener = listener
	server := &http.Server{Handler: b.handler(cert), ReadHeaderTimeout: 5 * time.Second}
	b.server = server
	go func() { _ = server.Serve(listener) }()

	b.state.ProxyEnabled = true
	if err := b.env.writeState(statePath, b.state); err != nil {
		return b.cleanupStartFailure(ctx, fmt.Errorf("write bridge recovery state: %w", err))
	}
	if err := b.enablePAC(ctx, service); err != nil {
		return b.cleanupStartFailure(ctx, err)
	}
	if b.server == nil {
		_ = b.Stop(ctx)
		return fmt.Errorf("local bridge server stopped before activation")
	}
	if openApp {
		if err := b.env.startApp(ctx, b.options.WeChatAppPath); err != nil {
			_ = b.Stop(ctx)
			return err
		}
	}
	return nil
}

// StopRequested is closed by an authenticated local stop request. The owning
// process remains the only writer of system proxy and certificate state.
func (b *Bridge) StopRequested() <-chan struct{} {
	return b.stopRequested
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
	state, err := b.env.readState(b.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read bridge recovery state: %w", err)
	}
	b.state = state
	b.options.Port = state.Port
	if b.server == nil && b.stopRequested == nil && state.ControlToken != "" {
		if err := b.requestRunningBridgeStop(ctx, state); err == nil {
			return b.waitForOwnerCleanup(ctx)
		} else if !isBridgeUnavailable(err) {
			return fmt.Errorf("request running bridge stop: %w", err)
		} else if ownerProcessAlive(state.OwnerPID) {
			return fmt.Errorf("bridge owner process %d is starting or unavailable; wait and retry", state.OwnerPID)
		}
	}
	var errs []error
	if state.ProxyEnabled {
		if err := b.restoreProxy(ctx, state); err != nil {
			errs = append(errs, err)
		}
	}
	if state.CertificateInstalled {
		if err := b.removeCertificate(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if b.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		errs = append(errs, b.server.Shutdown(shutdownCtx))
		cancel()
		b.server = nil
		b.listener = nil
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("stop wechat channels bridge: %w", err)
	}
	return b.env.remove(b.statePath())
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
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (b *Bridge) handler(cert *tls.Certificate) http.Handler {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Logger = logDiscarder{}
	// The local PAC changes only the first hop for qq.com. The bridge keeps the
	// original manual proxy selection for its outbound hop.
	proxy.Tr = &http.Transport{Proxy: b.outboundProxy, DisableCompression: true}
	proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile(`(?i)(^|\.)qq\.com:443$`))).HandleConnectFunc(func(host string, _ *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		return &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(cert)}, host
	})
	proxy.OnRequest().DoFunc(b.observeRequest)
	proxy.OnResponse().DoFunc(b.captureResponse)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLocalPACRequest(r) {
			b.servePAC(w)
			return
		}
		if isLocalBridgeRequest(r) {
			b.serveActiveVideo(w, r)
			return
		}
		if isLocalBridgeStopRequest(r) {
			b.serveStopRequest(w, r)
			return
		}
		if !isQQProxyRequest(r) {
			http.Error(w, "bridge proxy accepts qq.com only", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
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

type logDiscarder struct{}

func (logDiscarder) Printf(string, ...any) {}

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
	host := r.URL.Hostname()
	if host == "" {
		host, _, _ = net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
	}
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "qq.com" || strings.HasSuffix(host, ".qq.com")
}

func (b *Bridge) serveActiveVideo(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.NotFound(w, r)
		return
	}
	b.mu.RLock()
	active := b.active
	b.mu.RUnlock()
	if strings.TrimSpace(active.ObjectID) == "" || strings.TrimSpace(active.ObjectNonceID) == "" {
		http.Error(w, "open and play a WeChat Channels video first", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(active)
}

func (b *Bridge) serveStopRequest(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		http.NotFound(w, r)
		return
	}
	b.stopRequestedOnce.Do(func() { close(b.stopRequested) })
	w.WriteHeader(http.StatusNoContent)
}

func (b *Bridge) authorized(r *http.Request) bool {
	return b.state.ControlToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(bridgeTokenHeader)), []byte(b.state.ControlToken)) == 1
}

func (b *Bridge) captureResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil || ctx == nil || ctx.Req == nil || !isChannelsAPIRequest(ctx.Req) || !isJSONResponse(resp) || resp.Body == nil {
		return resp
	}
	identity, ok := ctx.UserData.(videoIdentity)
	if !ok {
		return resp
	}
	originalBody := resp.Body
	body, err := io.ReadAll(io.LimitReader(originalBody, maxCapturedBody+1))
	if err != nil || len(body) > maxCapturedBody {
		// The proxy must remain transparent when a response cannot be safely
		// inspected. Replay the bytes already consumed, then continue from the
		// original stream instead of returning a closed/truncated response.
		resp.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(body), originalBody), Closer: originalBody}
		return resp
	}
	_ = originalBody.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.TransferEncoding = nil
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	if video, ok := activeVideoFromJSONForIdentity(body, identity); ok {
		b.mu.Lock()
		b.active = video
		b.mu.Unlock()
	}
	return resp
}

type videoIdentity struct {
	objectID      string
	objectNonceID string
}

type replayBody struct {
	io.Reader
	io.Closer
}

func (b *Bridge) observeRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if req == nil || ctx == nil || !isChannelsAPIRequest(req) || !isJSONRequest(req) || req.Body == nil || req.ContentLength > maxCapturedBody {
		return req, nil
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, maxCapturedBody+1))
	if err != nil {
		return req, nil
	}
	req.Body = replayBody{Reader: io.MultiReader(bytes.NewReader(body), req.Body), Closer: req.Body}
	if len(body) > maxCapturedBody {
		return req, nil
	}
	identity, ok := videoIdentityFromRequest(body)
	if !ok {
		return req, nil
	}
	ctx.UserData = identity
	return req, nil
}

func isChannelsAPIRequest(req *http.Request) bool {
	host := strings.ToLower(req.URL.Hostname())
	if host == "" {
		host, _, _ = net.SplitHostPort(req.Host)
		host = strings.ToLower(host)
	}
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
	var selected activeVideoResponse
	found := false
	visitJSON(value, func(object map[string]any) {
		candidate, ok := activeVideoFromObject(object)
		if ok {
			selected, found = candidate, true
		}
	})
	return selected, found
}

func activeVideoFromJSONForIdentity(raw []byte, identity videoIdentity) (activeVideoResponse, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return activeVideoResponse{}, false
	}
	var selected activeVideoResponse
	visitJSON(value, func(object map[string]any) {
		candidate, ok := activeVideoFromObject(object)
		if ok && candidate.ObjectID == identity.objectID && candidate.ObjectNonceID == identity.objectNonceID {
			selected = candidate
		}
	})
	return selected, selected.ObjectID != ""
}

func videoIdentityFromRequest(raw []byte) (videoIdentity, bool) {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return videoIdentity{}, false
	}
	video, ok := activeVideoFromObject(object)
	if !ok {
		return videoIdentity{}, false
	}
	return videoIdentity{objectID: video.ObjectID, objectNonceID: video.ObjectNonceID}, true
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
	if desc, ok := object["objectDesc"].(map[string]any); ok {
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
	if b.state.CertificateSHA1 == "" {
		return nil
	}
	output, err := b.env.run.Run(ctx, "security", "delete-certificate", "-Z", b.state.CertificateSHA1, "-t", defaultLoginKeychain())
	if err != nil {
		return fmt.Errorf("remove local bridge certificate: %w", commandError("security delete-certificate", output, err))
	}
	return nil
}

func (b *Bridge) certificatePresent(ctx context.Context) (bool, error) {
	output, err := b.env.run.Run(ctx, "security", "find-certificate", "-a", "-Z", "-c", bridgeCACommonName, defaultLoginKeychain())
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "could not be found") {
			return false, nil
		}
		return false, fmt.Errorf("check local bridge certificate: %w", commandError("security find-certificate", output, err))
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "SHA-1 hash") && strings.EqualFold(strings.TrimSpace(value), b.state.CertificateSHA1) {
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
