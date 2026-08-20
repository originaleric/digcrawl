package wechatchannels

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
)

func TestActiveVideoFromJSONSelectsVideoMetadata(t *testing.T) {
	video, ok := activeVideoFromJSON([]byte(`{
  "data":{"feeds":[
    {"id":"first","objectNonceId":"n-first","description":"first title"},
    {"id":"Current-ID","objectNonceId":"Current-Nonce","description":"current title","createtime":123,"contact":{"username":"author-id","nickname":"Author"},"objectDesc":{"media":[{"coverUrl":"https://cover.example/image"}]}}
  ]}
}`))
	if !ok {
		t.Fatal("expected video metadata")
	}
	if video.ObjectID != "Current-ID" || video.ObjectNonceID != "Current-Nonce" {
		t.Fatalf("unexpected identity: %#v", video)
	}
	if video.Title != "current title" || video.AuthorID != "author-id" || video.CoverURL != "https://cover.example/image" {
		t.Fatalf("unexpected metadata: %#v", video)
	}
}

func TestActiveVideoFromJSONRequiresIDAndNonceTogether(t *testing.T) {
	if _, ok := activeVideoFromJSON([]byte(`{"id":"only-id","objectNonceId":""}`)); ok {
		t.Fatal("expected incomplete metadata to be ignored")
	}
}

func TestActiveVideoFromJSONForIdentityRejectsOtherFeed(t *testing.T) {
	raw := []byte(`{"data":{"feeds":[{"id":"other","objectNonceId":"other-nonce"},{"id":"wanted","objectNonceId":"wanted-nonce","description":"wanted title"}]}}`)
	video, ok := activeVideoFromJSONForIdentity(raw, videoIdentity{objectID: "wanted", objectNonceID: "wanted-nonce"})
	if !ok || video.ObjectID != "wanted" || video.Title != "wanted title" {
		t.Fatalf("expected requested feed, got %#v found=%v", video, ok)
	}
	if _, ok := activeVideoFromJSONForIdentity(raw, videoIdentity{objectID: "missing", objectNonceID: "missing-nonce"}); ok {
		t.Fatal("unexpected non-matching feed")
	}
}

func TestVideoIdentityFromRequest(t *testing.T) {
	identity, ok := videoIdentityFromRequest([]byte(`{"objectid":"Current-ID","objectNonceId":"Current-Nonce"}`))
	if !ok || identity.objectID != "Current-ID" || identity.objectNonceID != "Current-Nonce" {
		t.Fatalf("unexpected request identity: %#v found=%v", identity, ok)
	}
}

func TestCaptureResponseMatchesRequestAndPreservesBody(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	requestBody := `{"objectid":"wanted","objectNonceId":"wanted-nonce"}`
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "channels.weixin.qq.com", Path: "/finder/feed/detail"}, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(requestBody)), ContentLength: int64(len(requestBody))}
	ctx := &goproxy.ProxyCtx{Req: req}
	bridge.observeRequest(req, ctx)
	replayedRequest, err := io.ReadAll(req.Body)
	if err != nil || string(replayedRequest) != requestBody {
		t.Fatalf("request body changed: %q err=%v", replayedRequest, err)
	}
	responseBody := `{"feeds":[{"id":"other","objectNonceId":"other-nonce"},{"id":"wanted","objectNonceId":"wanted-nonce","description":"wanted title"}]}`
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}, "Transfer-Encoding": []string{"chunked"}}, Body: io.NopCloser(strings.NewReader(responseBody)), TransferEncoding: []string{"chunked"}}
	bridge.captureResponse(resp, ctx)
	gotBody, err := io.ReadAll(resp.Body)
	if err != nil || string(gotBody) != responseBody {
		t.Fatalf("response body changed: %q err=%v", gotBody, err)
	}
	if resp.Header.Get("Transfer-Encoding") != "" || len(resp.TransferEncoding) != 0 {
		t.Fatalf("transfer encoding must be normalized: %#v %#v", resp.Header, resp.TransferEncoding)
	}
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "wanted" || active.Title != "wanted title" {
		t.Fatalf("unexpected captured video: %#v", active)
	}
}

func TestCaptureResponseReplaysOversizedBody(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	responseBody := `{"id":"wanted","objectNonceId":"wanted-nonce","description":"` + strings.Repeat("a", maxCapturedBody) + `"}`
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody)), ContentLength: int64(len(responseBody))}
	ctx := &goproxy.ProxyCtx{Req: &http.Request{URL: &url.URL{Host: "channels.weixin.qq.com", Path: "/finder/feed/detail"}}, UserData: videoIdentity{objectID: "wanted", objectNonceID: "wanted-nonce"}}
	bridge.captureResponse(resp, ctx)
	gotBody, err := io.ReadAll(resp.Body)
	if err != nil || string(gotBody) != responseBody {
		t.Fatalf("oversized response body changed: len=%d want=%d err=%v", len(gotBody), len(responseBody), err)
	}
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "" {
		t.Fatalf("oversized response must not become active video: %#v", active)
	}
}

func TestParseProxyConfig(t *testing.T) {
	config, err := parseProxyConfig("Enabled: Yes\nServer: 127.0.0.1\nPort: 20261\n")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if !isOwnProxy(config, 20261) {
		t.Fatalf("expected own proxy: %#v", config)
	}
	if _, err := parseProxyConfig("Port: invalid\n"); err == nil {
		t.Fatal("expected invalid port error")
	}
	authenticated, err := parseProxyConfig("Enabled: Yes\nServer: proxy.example\nPort: 8080\nAuthenticated Proxy Enabled: 1\n")
	if err != nil || !authenticated.Authenticated {
		t.Fatalf("expected authenticated proxy to be detected: %#v err=%v", authenticated, err)
	}
}

func TestBridgeOutboundProxyUsesOriginalSelection(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	bridge.webProxy = proxyConfig{Enabled: true, Server: "web-proxy.example", Port: 8080}
	bridge.secureProxy = proxyConfig{Enabled: true, Server: "secure-proxy.example", Port: 8443}
	bridge.socksProxy = proxyConfig{Enabled: true, Server: "socks-proxy.example", Port: 1080}
	for _, test := range []struct {
		url  string
		want string
	}{
		{"http://channels.weixin.qq.com/finder", "http://web-proxy.example:8080"},
		{"https://channels.weixin.qq.com/finder", "http://secure-proxy.example:8443"},
	} {
		req, err := http.NewRequest(http.MethodGet, test.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := bridge.outboundProxy(req)
		if err != nil || got == nil || got.String() != test.want {
			t.Fatalf("outbound proxy for %s = %v, %v; want %s", test.url, got, err, test.want)
		}
	}
	bridge.webProxy = proxyConfig{}
	bridge.secureProxy = proxyConfig{}
	req, err := http.NewRequest(http.MethodGet, "https://channels.weixin.qq.com/finder", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := bridge.outboundProxy(req)
	if err != nil || got == nil || got.String() != "socks5://socks-proxy.example:1080" {
		t.Fatalf("SOCKS fallback = %v, %v", got, err)
	}
	bridge.bypassDomains = []string{"*.qq.com"}
	got, err = bridge.outboundProxy(req)
	if err != nil || got != nil {
		t.Fatalf("bypassed QQ request must go direct, got %v err=%v", got, err)
	}
}

func TestStateMachineDoesNotEmitSequentialVersionAndReadsLegacyState(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, bridgeStateFile)
	state := bridgeState{StateMachine: true, Service: "Wi-Fi", Port: 20261, ProxyMode: "pac", ControlToken: "token", OwnerPID: 1}
	if err := writeBridgeState(path, state); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"version"`) || !strings.Contains(string(raw), `"state_machine":true`) {
		t.Fatalf("new state must identify the state machine without a version: %s", raw)
	}
	legacyPath := filepath.Join(stateDir, "legacy-state.json")
	if err := os.WriteFile(legacyPath, []byte(`{"version":2,"service":"Wi-Fi","port":20261,"proxy_mode":"pac"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := readBridgeState(legacyPath)
	if err != nil || legacy.LegacyVersion != 2 {
		t.Fatalf("legacy recovery state must remain readable: %#v err=%v", legacy, err)
	}
}

func TestBridgeEndpointsRequireControlTokenAndProxyOnlyAllowsQQ(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	bridge.state.ControlToken = "token"
	bridge.stopRequested = make(chan struct{})
	handler := bridge.handler(&tls.Certificate{})

	for _, test := range []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"active without token", http.MethodGet, activeVideoAPIPath, "", http.StatusNotFound},
		{"active with token", http.MethodGet, activeVideoAPIPath, "token", http.StatusNotFound},
		{"stop without token", http.MethodPost, bridgeStopPath, "", http.StatusNotFound},
		{"non QQ proxy", http.MethodGet, "http://example.com/", "", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, nil)
			if strings.HasPrefix(test.path, "http") {
				req = httptest.NewRequest(test.method, test.path, nil)
			} else {
				req.Host = "127.0.0.1"
			}
			req.Header.Set(bridgeTokenHeader, test.token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != test.want {
				t.Fatalf("status=%d want=%d", recorder.Code, test.want)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, bridgeStopPath, nil)
	req.Host = "127.0.0.1"
	req.Header.Set(bridgeTokenHeader, "token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("stop status=%d", recorder.Code)
	}
	select {
	case <-bridge.StopRequested():
	default:
		t.Fatal("authorized stop must notify owning bridge")
	}
}

func TestExternalStopRequestTargetsRunningOwner(t *testing.T) {
	owner := newBridge(BridgeOptions{}, bridgeEnvironment{})
	owner.state.ControlToken = "token"
	owner.stopRequested = make(chan struct{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: owner.handler(&tls.Certificate{})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	external := newBridge(BridgeOptions{}, bridgeEnvironment{})
	if err := external.requestRunningBridgeStop(context.Background(), bridgeState{Port: port, ControlToken: "token"}); err != nil {
		t.Fatalf("request running bridge stop: %v", err)
	}
	select {
	case <-owner.StopRequested():
	case <-time.After(time.Second):
		t.Fatal("owner did not receive external stop request")
	}
}

func TestExternalStopWaitsForOwningBridgeCleanup(t *testing.T) {
	stateDir := t.TempDir()
	env := bridgeEnvironment{run: &fakeRunner{}, readState: readBridgeState, writeState: writeBridgeState, remove: os.Remove}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	state := bridgeState{StateMachine: true, Service: "Wi-Fi", Port: port, ProxyMode: "pac", ControlToken: "token", OwnerPID: os.Getpid()}
	if err := writeBridgeState(filepath.Join(stateDir, bridgeStateFile), state); err != nil {
		t.Fatal(err)
	}
	owner := newBridge(BridgeOptions{StateDir: stateDir}, env)
	owner.state = state
	owner.stopRequested = make(chan struct{})
	owner.server = &http.Server{Handler: owner.handler(&tls.Certificate{})}
	go func() { _ = owner.server.Serve(listener) }()

	ownerDone := make(chan error, 1)
	go func() {
		<-owner.StopRequested()
		ownerDone <- owner.Stop(context.Background())
	}()

	external := newBridge(BridgeOptions{StateDir: stateDir}, env)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := external.Stop(ctx); err != nil {
		t.Fatalf("external stop: %v", err)
	}
	if err := <-ownerDone; err != nil {
		t.Fatalf("owner cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, bridgeStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state must be removed by owner cleanup, got %v", err)
	}
}

func TestMakePACOnlyDivertsQQAndKeepsManualProxies(t *testing.T) {
	pac, err := makePAC(20261,
		proxyConfig{Enabled: true, Server: "http-proxy.example", Port: 8080},
		proxyConfig{Enabled: true, Server: "https-proxy.example", Port: 8443},
		proxyConfig{},
		[]string{"localhost", "127.0.0.0/8", "*.internal.example"},
	)
	if err != nil {
		t.Fatalf("makePAC: %v", err)
	}
	for _, expected := range []string{"host === \"qq.com\"", "*.qq.com", "PROXY 127.0.0.1:20261", "PROXY https-proxy.example:8443", "PROXY http-proxy.example:8080", "host === \"localhost\"", "isInNet(host, \"127.0.0.0\", \"255.0.0.0\")", "shExpMatch(host, \"*.internal.example\")"} {
		if !strings.Contains(pac, expected) {
			t.Fatalf("PAC missing %q:\n%s", expected, pac)
		}
	}
}

func TestMakePACFormatsIPv6ProxyAddress(t *testing.T) {
	pac, err := makePAC(20261, proxyConfig{}, proxyConfig{Enabled: true, Server: "2001:db8::1", Port: 8443}, proxyConfig{}, nil)
	if err != nil {
		t.Fatalf("makePAC: %v", err)
	}
	if !strings.Contains(pac, "PROXY [2001:db8::1]:8443") {
		t.Fatalf("PAC did not bracket IPv6 proxy: %s", pac)
	}
}

func TestMakePACRefusesUnrepresentableBypassDomain(t *testing.T) {
	if _, err := makePAC(20261, proxyConfig{}, proxyConfig{}, proxyConfig{}, []string{"2001:db8::/32"}); err == nil || !strings.Contains(err.Error(), "unsupported bypass network") {
		t.Fatalf("expected IPv6 CIDR safety refusal, got %v", err)
	}
}

func TestPreparePACRefusesExistingPAC(t *testing.T) {
	bridge := newBridge(BridgeOptions{Port: 20261}, bridgeEnvironment{run: &fakeRunner{autoEnabled: true, autoURL: "http://existing.example/proxy.pac"}})
	_, _, err := bridge.preparePAC(context.Background(), "Wi-Fi")
	if err == nil || !strings.Contains(err.Error(), "existing proxy auto-configuration") {
		t.Fatalf("expected PAC safety refusal, got %v", err)
	}
}

func TestPreparePACRefusesExistingAutoDiscovery(t *testing.T) {
	bridge := newBridge(BridgeOptions{Port: 20261}, bridgeEnvironment{run: &fakeRunner{autoDiscoveryEnabled: true}})
	_, _, err := bridge.preparePAC(context.Background(), "Wi-Fi")
	if err == nil || !strings.Contains(err.Error(), "existing proxy auto-discovery") {
		t.Fatalf("expected auto-discovery safety refusal, got %v", err)
	}
}

func TestPreparePACRefusesAuthenticatedProxy(t *testing.T) {
	bridge := newBridge(BridgeOptions{Port: 20261}, bridgeEnvironment{run: &fakeRunner{webProxy: "Enabled: Yes\nServer: proxy.example\nPort: 8080\nAuthenticated Proxy Enabled: Yes\n"}})
	_, _, err := bridge.preparePAC(context.Background(), "Wi-Fi")
	if err == nil || !strings.Contains(err.Error(), "authenticated manual proxy") {
		t.Fatalf("expected credential-safety refusal, got %v", err)
	}
}

func TestBridgeStopRestoresManualProxyPathByDisablingBridgePAC(t *testing.T) {
	runner := &fakeRunner{autoEnabled: true, autoURL: bridgePACURL(20261)}
	stateDir := t.TempDir()
	state := bridgeState{LegacyVersion: 2, Service: "Wi-Fi", Port: 20261, ProxyEnabled: true, ProxyMode: "pac"}
	if err := writeBridgeState(filepath.Join(stateDir, bridgeStateFile), state); err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{StateDir: stateDir}, bridgeEnvironment{run: runner, readState: readBridgeState, writeState: writeBridgeState, remove: os.Remove})
	if err := bridge.Stop(context.Background()); err != nil {
		t.Fatalf("stop bridge: %v", err)
	}
	runner.requireCall(t, "networksetup -setautoproxystate Wi-Fi off")
	runner.forbidCall(t, "networksetup -setwebproxystate Wi-Fi off")
	runner.forbidCall(t, "networksetup -setsecurewebproxystate Wi-Fi off")
}

func TestBridgeStartRollsBackWhenListenerFails(t *testing.T) {
	runner := &fakeRunner{}
	stateDir := t.TempDir()
	bridge := newBridge(BridgeOptions{Port: 20261, StateDir: stateDir}, bridgeEnvironment{
		run:        runner,
		listen:     func(string, string) (net.Listener, error) { return nil, errors.New("occupied") },
		startApp:   func(context.Context, string) error { return nil },
		goos:       "darwin",
		loadCA:     func(string) (*tls.Certificate, string, error) { return &tls.Certificate{}, "FINGERPRINT", nil },
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	})
	err := bridge.Start(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "listen on local bridge port") {
		t.Fatalf("unexpected start error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, bridgeStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery state must be removed after rollback, got %v", err)
	}
	runner.forbidCall(t, "networksetup -setwebproxystate Wi-Fi off")
	runner.forbidCall(t, "networksetup -setsecurewebproxystate Wi-Fi off")
	runner.requireCall(t, "security find-certificate -a -Z -c DigCrawl WeChat Channels Local Bridge CA")
	runner.requireCall(t, "security add-trusted-cert -r trustRoot -k")
	runner.forbidCall(t, "security add-trusted-cert -d")
	runner.requireCall(t, "security delete-certificate -Z FINGERPRINT -t")
}

func TestBridgeStartRollbackSurvivesCancelledStartContext(t *testing.T) {
	runner := &fakeRunner{rejectCancelledContext: true}
	stateDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge := newBridge(BridgeOptions{Port: 20261, StateDir: stateDir}, bridgeEnvironment{
		run: runner,
		listen: func(string, string) (net.Listener, error) {
			cancel()
			return nil, errors.New("occupied")
		},
		startApp:   func(context.Context, string) error { return nil },
		goos:       "darwin",
		loadCA:     func(string) (*tls.Certificate, string, error) { return &tls.Certificate{}, "FINGERPRINT", nil },
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	})
	if err := bridge.Start(ctx, false); err == nil || !strings.Contains(err.Error(), "listen on local bridge port") {
		t.Fatalf("unexpected start error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, bridgeStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery state must be removed after cancelled rollback, got %v", err)
	}
	runner.requireCall(t, "security delete-certificate -Z FINGERPRINT -t")
}

func TestBridgeStopDoesNotOverwriteNewUserProxy(t *testing.T) {
	runner := &fakeRunner{webOwn: true, secureProxy: "Enabled: Yes\nServer: proxy.example\nPort: 8443\n"}
	stateDir := t.TempDir()
	state := bridgeState{LegacyVersion: 1, Service: "Wi-Fi", Port: 20261, CertificateSHA1: "FINGERPRINT", CertificateInstalled: true, ProxyEnabled: true}
	if err := writeBridgeState(filepath.Join(stateDir, bridgeStateFile), state); err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{Port: 9, StateDir: stateDir}, bridgeEnvironment{
		run:        runner,
		goos:       "darwin",
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	})
	if err := bridge.Stop(context.Background()); err != nil {
		t.Fatalf("stop bridge: %v", err)
	}
	runner.requireCall(t, "networksetup -setwebproxystate Wi-Fi off")
	runner.forbidCall(t, "networksetup -setsecurewebproxystate Wi-Fi off")
	runner.requireCall(t, "security delete-certificate -Z FINGERPRINT -t")
}

func TestBridgeDoesNotRemovePreexistingCertificate(t *testing.T) {
	runner := &fakeRunner{findResult: "SHA-1 hash: FINGERPRINT\n"}
	bridge := newBridge(BridgeOptions{Port: 20261, StateDir: t.TempDir()}, bridgeEnvironment{
		run:        runner,
		listen:     func(string, string) (net.Listener, error) { return nil, errors.New("occupied") },
		startApp:   func(context.Context, string) error { return nil },
		goos:       "darwin",
		loadCA:     func(string) (*tls.Certificate, string, error) { return &tls.Certificate{}, "FINGERPRINT", nil },
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	})
	if err := bridge.Start(context.Background(), false); err == nil {
		t.Fatal("expected listener error")
	}
	runner.forbidCall(t, "security add-trusted-cert")
	runner.forbidCall(t, "security delete-certificate")
}

type fakeRunner struct {
	mu                     sync.Mutex
	calls                  []string
	webOwn                 bool
	webProxy               string
	secureOwn              bool
	secureProxy            string
	findResult             string
	autoEnabled            bool
	autoURL                string
	autoDiscoveryEnabled   bool
	rejectCancelledContext bool
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.rejectCancelledContext && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	call := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.mu.Lock()
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "-setwebproxy Wi-Fi 127.0.0.1"):
		r.webOwn = true
	case strings.Contains(call, "-setsecurewebproxy Wi-Fi 127.0.0.1"):
		r.secureOwn = true
	case strings.Contains(call, "-setwebproxystate Wi-Fi off"):
		r.webOwn = false
	case strings.Contains(call, "-setsecurewebproxystate Wi-Fi off"):
		r.secureOwn = false
	case strings.Contains(call, "-setautoproxyurl Wi-Fi"):
		r.autoEnabled = true
		r.autoURL = args[len(args)-1]
	case strings.Contains(call, "-setautoproxystate Wi-Fi off"):
		r.autoEnabled = false
	}
	webOwn, secureOwn, secureProxy, autoEnabled, autoURL, autoDiscoveryEnabled := r.webOwn, r.secureOwn, r.secureProxy, r.autoEnabled, r.autoURL, r.autoDiscoveryEnabled
	r.mu.Unlock()
	switch {
	case strings.Contains(call, "find-certificate"):
		return []byte(r.findResult), nil
	case strings.Contains(call, "-getautoproxyurl"):
		if autoEnabled {
			return []byte("URL: " + autoURL + "\nEnabled: Yes\n"), nil
		}
		return []byte("URL: \nEnabled: No\n"), nil
	case strings.Contains(call, "-getproxyautodiscovery"):
		if autoDiscoveryEnabled {
			return []byte("Auto Proxy Discovery: On\n"), nil
		}
		return []byte("Auto Proxy Discovery: Off\n"), nil
	case strings.Contains(call, "-getproxybypassdomains"):
		return []byte("localhost\n127.0.0.0/8\n::1\n"), nil
	case strings.Contains(call, "-getwebproxy"):
		if r.webProxy != "" {
			return []byte(r.webProxy), nil
		}
		if !webOwn {
			return []byte("Enabled: No\nServer: \nPort: 0\n"), nil
		}
		return []byte("Enabled: Yes\nServer: 127.0.0.1\nPort: 20261\n"), nil
	case strings.Contains(call, "-getsecurewebproxy"):
		if secureProxy != "" {
			return []byte(secureProxy), nil
		}
		if !secureOwn {
			return []byte("Enabled: No\nServer: \nPort: 0\n"), nil
		}
		return []byte("Enabled: Yes\nServer: 127.0.0.1\nPort: 20261\n"), nil
	}
	return nil, nil
}

func (r *fakeRunner) RunInput(ctx context.Context, _ string, input string, _ ...string) ([]byte, error) {
	if r.rejectCancelledContext && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	r.mu.Lock()
	r.calls = append(r.calls, "scutil "+strings.TrimSpace(input))
	r.mu.Unlock()
	if strings.Contains(input, "Global/IPv4") {
		return []byte("PrimaryService : 12345678-1234-1234-1234-1234567890AB\n"), nil
	}
	return []byte("UserDefinedName : Wi-Fi\n"), nil
}

func (r *fakeRunner) requireCall(t *testing.T, expected string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		if strings.HasPrefix(call, expected) {
			return
		}
	}
	t.Fatalf("expected command %q, calls: %v", expected, r.calls)
}

func (r *fakeRunner) forbidCall(t *testing.T, unwanted string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		if strings.HasPrefix(call, unwanted) {
			t.Fatalf("unexpected command %q, calls: %v", unwanted, r.calls)
		}
	}
}
