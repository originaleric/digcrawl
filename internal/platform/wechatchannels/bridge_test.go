package wechatchannels

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func setBridgeRunning(bridge *Bridge) {
	bridge.mu.Lock()
	bridge.phase = bridgeRunning
	bridge.mu.Unlock()
}

func TestActiveVideoFromJSONSelectsVideoMetadata(t *testing.T) {
	video, ok := activeVideoFromJSON([]byte(`{
  "data":{"feeds":[
    {"id":"first","objectNonceId":"n-first","description":"first title","objectDesc":{"mediaType":2}},
    {"id":"Current-ID","objectNonceId":"Current-Nonce","description":"current title","createtime":123,"contact":{"username":"author-id","nickname":"Author"},"objectDesc":{"mediaType":4,"media":[{"coverUrl":"https://cover.example/image"}]}}
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

func TestBridgeDownloadActiveVideoRejectsSizeLimitWithoutPublishingOutput(t *testing.T) {
	for _, test := range []struct {
		name         string
		flushHeaders bool
	}{
		{name: "declared content length"},
		{name: "streamed content length", flushHeaders: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "finder.video.qq.com" {
					t.Fatalf("request host=%q", r.Host)
				}
				w.Header().Set("Content-Type", "video/mp4")
				if test.flushHeaders {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, "1234")
			}))
			defer server.Close()

			bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
			transport := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- the listener is this test server.
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				},
			}
			bridge.transport = transport
			defer transport.CloseIdleConnections()

			outputPath := filepath.Join(t.TempDir(), "video.mp4")
			_, err := bridge.downloadActiveVideo(context.Background(), activeDownloadResource{
				ObjectID:      "object",
				ObjectNonceID: "nonce",
				URL:           "https://finder.video.qq.com/video.mp4",
			}, outputPath, 3)
			if err == nil || !strings.Contains(err.Error(), "3-byte limit") {
				t.Fatalf("download error=%v", err)
			}
			if _, err := os.Lstat(outputPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("output should not be published, err=%v", err)
			}
			parts, err := filepath.Glob(filepath.Join(filepath.Dir(outputPath), ".digcrawl-wechat-channels-*.part"))
			if err != nil || len(parts) != 0 {
				t.Fatalf("temporary files=%v err=%v", parts, err)
			}
		})
	}
}

func TestNormalizeDownloadMaxBytesKeepsLegacyClientsBounded(t *testing.T) {
	maxBytes, err := normalizeDownloadMaxBytes(0)
	if err != nil || maxBytes != DefaultDownloadMaxBytes {
		t.Fatalf("legacy max bytes=%d err=%v", maxBytes, err)
	}
	for _, invalid := range []int64{-1, maxInt64} {
		if _, err := normalizeDownloadMaxBytes(invalid); err == nil {
			t.Fatalf("max bytes=%d should be rejected", invalid)
		}
	}
}

func TestActiveVideoFromJSONRequiresIDAndNonceTogether(t *testing.T) {
	if _, ok := activeVideoFromJSON([]byte(`{"id":"only-id","objectNonceId":""}`)); ok {
		t.Fatal("expected incomplete metadata to be ignored")
	}
}

func TestActiveVideoFromJSONRejectsAmbiguousOrNonVideoFeeds(t *testing.T) {
	if _, ok := activeVideoFromJSON([]byte(`{"feeds":[{"id":"picture","objectNonceId":"picture-nonce","objectDesc":{"mediaType":2}},{"id":"live","objectNonceId":"live-nonce","objectDesc":{"mediaType":9}}]}`)); ok {
		t.Fatal("non-video feeds must be ignored")
	}
	if _, ok := activeVideoFromJSON([]byte(`{"feeds":[{"id":"first","objectNonceId":"first-nonce","objectDesc":{"mediaType":4}},{"id":"second","objectNonceId":"second-nonce","objectDesc":{"mediaType":4}}]}`)); ok {
		t.Fatal("a multi-video response must not select an arbitrary feed")
	}
}

func TestPageCaptureInjectionOnlyChangesEligibleChannelsResources(t *testing.T) {
	html, injected := pageCaptureInjection(`<html>测试<HEAD data-page="video"><script src="https://res.wx.qq.com/web/js/app.js"></script><title>Video</title></HEAD><body></body></html>`)
	if !injected || !strings.Contains(html, bridgePageScriptPath) {
		t.Fatalf("page script was not injected: %q", html)
	}
	if !strings.Contains(html, "app.js?digcrawl_bridge="+bridgeModuleVersion) {
		t.Fatalf("page module graph must be versioned: %q", html)
	}
	if _, injected := pageCaptureInjection(html); injected {
		t.Fatal("page script must not be injected twice")
	}
	if updated, injected := pageCaptureInjection(`<html><body></body></html>`); injected || updated != `<html><body></body></html>` {
		t.Fatal("page without a head must remain unchanged")
	}
}

func TestPageModuleInjectionVersionsEntireViteGraph(t *testing.T) {
	page := `<html><head><script type="module" crossorigin src="//res.wx.qq.com/t/finder/index.publishBj0l5xS_Cz.js"></script><link rel="modulepreload" crossorigin href="//res.wx.qq.com/t/finder/virtual_svg-icons-register.publishCwe-fFAJiv.js"><script src="//res.wx.qq.com/t/finder/unrelated.js"></script></head></html>`
	updated, injected := pageCaptureInjection(page)
	if !injected || !strings.Contains(updated, bridgePageScriptPath) {
		t.Fatalf("page was not injected: %q", updated)
	}
	for _, expected := range []string{
		`index.publishBj0l5xS_Cz.js?digcrawl_bridge=2`,
		`virtual_svg-icons-register.publishCwe-fFAJiv.js?digcrawl_bridge=2`,
		`unrelated.js?digcrawl_bridge=2`,
	} {
		expected = strings.ReplaceAll(expected, "=2", "="+bridgeModuleVersion)
		if !strings.Contains(updated, expected) {
			t.Fatalf("required Vite module was not versioned %q: %q", expected, updated)
		}
	}

	entry := `import{a}from"./global.publishBF6b-4OHVp.js";import{b}from"./virtual_svg-icons-register.publishCwe-fFAJiv.js";`
	updated, injected = pageModuleGraphInjection(entry)
	if !injected || !strings.Contains(updated, `from"./virtual_svg-icons-register.publishCwe-fFAJiv.js?digcrawl_bridge=`+bridgeModuleVersion+`"`) || !strings.Contains(updated, `from"./global.publishBF6b-4OHVp.js?digcrawl_bridge=`+bridgeModuleVersion+`"`) {
		t.Fatalf("entry module graph was not fully versioned: %q", updated)
	}
}

func TestPageDetailModuleInjectionWrapsOnlyDetailReturn(t *testing.T) {
	source := `class Service{async finderGetCommentDetail(t){return this.post({name:"FinderGetCommentDetail",data:{...t}})}async finderLike(t){return this.post(t)}}`
	updated, injected := pageDetailModuleInjection(source)
	if !injected || !strings.Contains(updated, "__digcrawlWechatChannelsReport") || !strings.Contains(updated, `async finderLike(t){return this.post(t)}`) {
		t.Fatalf("detail module rewrite is not isolated: %q", updated)
	}
	node, err := exec.LookPath("node")
	if err == nil {
		path := filepath.Join(t.TempDir(), "detail-module.js")
		if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
			t.Fatalf("rewritten detail module syntax error: %v: %s", err, output)
		}
	}
}

func TestPageReportKeepsCompletePlaybackQueryOnlyInBridgeMemory(t *testing.T) {
	video, resource, ok := activeVideoFromPageReport(pageVideoReport{
		ObjectID:      "object",
		ObjectNonceID: "nonce",
		MediaType:     4,
		MediaURL:      "https://finder.video.qq.com/path/video.mp4?encfilekey=key&token=token&expires=1710000000&signature=keep-this&X-snsvideoflag=low",
		DecodeKey:     "123",
	})
	if !ok || video.ObjectID != "object" || resource.DecodeKey != 123 {
		t.Fatalf("page report=%#v resource=%#v ok=%v", video, resource, ok)
	}
	if resource.URL != "https://finder.video.qq.com/path/video.mp4?encfilekey=key&token=token&expires=1710000000&signature=keep-this&X-snsvideoflag=low" {
		t.Fatalf("playback URL query was changed: %q", resource.URL)
	}
	_, resource, ok = activeVideoFromPageReport(pageVideoReport{ObjectID: "object", ObjectNonceID: "nonce", MediaType: 4})
	if !ok || resource.URL != "" {
		t.Fatalf("metadata-only report must stay importable: %#v ok=%v", resource, ok)
	}
	if _, _, ok := activeVideoFromPageReport(pageVideoReport{ObjectID: "object", ObjectNonceID: "nonce", MediaType: 4, MediaURL: "https://example.com/video.mp4"}); !ok {
		t.Fatal("malformed download field must not discard valid metadata")
	}
}

func TestBridgeDownloadActiveVideoKeepsPlaybackQueryAndRedactsUpstreamFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "encfilekey=key&token=token&expires=1710000000&signature=keep-this" {
			t.Fatalf("playback query=%q", r.URL.RawQuery)
		}
		http.Error(w, "upstream-body-must-not-escape", http.StatusBadRequest)
	}))
	defer server.Close()

	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- the listener is this test server.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	bridge.transport = transport
	defer transport.CloseIdleConnections()

	_, err := bridge.downloadActiveVideo(context.Background(), activeDownloadResource{
		ObjectID:      "object",
		ObjectNonceID: "nonce",
		URL:           "https://finder.video.qq.com/video.mp4?encfilekey=key&token=token&expires=1710000000&signature=keep-this",
	}, filepath.Join(t.TempDir(), "video.mp4"), 1024)
	if err == nil || !strings.Contains(err.Error(), "QQ video source rejected the captured playback request (HTTP 400)") {
		t.Fatalf("download error=%v", err)
	}
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "upstream-body-must-not-escape") {
		t.Fatalf("download error leaked sensitive upstream material: %v", err)
	}
}

func TestBridgeCapturesExplicitDiagnosticPageHTMLBeforeInjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channels-page.html")
	bridge := newBridge(BridgeOptions{PageHTMLCapturePath: path}, bridgeEnvironment{})
	original := `<html><head><title>Channels</title></head><body>original</body></html>`
	response := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(original)),
	}
	request := httptest.NewRequest(http.MethodGet, "https://channels.weixin.qq.com/web/pages/feed", nil)
	updated := bridge.injectPageCapture(response, request)
	view, err := io.ReadAll(updated.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(view) != original {
		t.Fatalf("diagnostic mode changed the returned page: %q", view)
	}
	captured, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(captured) != original || strings.Contains(string(captured), bridgePageScriptPath) {
		t.Fatalf("diagnostic capture is not the original page: %q", captured)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("diagnostic capture permissions=%#o; must not expose group/world access", info.Mode().Perm())
	}
}

func TestBridgeLeavesNormalChannelsPageUnchanged(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	original := `<html><head><title>Channels</title></head><body>original</body></html>`
	response := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(original)),
	}
	request := httptest.NewRequest(http.MethodGet, "https://channels.weixin.qq.com/web/pages/feed", nil)
	updated := bridge.injectPageCapture(response, request)
	view, err := io.ReadAll(updated.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(view) != original {
		t.Fatalf("normal bridge modified a Channels page: %q", view)
	}
}

func TestBridgeRejectsRelativeDiagnosticPageHTMLPath(t *testing.T) {
	if _, err := NewBridge(BridgeOptions{PageHTMLCapturePath: "channels-page.html"}); err == nil {
		t.Fatal("relative diagnostic path must be rejected")
	}
}

func TestBridgeRejectsRelativeDiagnosticMetadataPath(t *testing.T) {
	if _, err := NewBridge(BridgeOptions{DiagnosticMetadataPath: "channels-metadata.ndjson"}); err == nil {
		t.Fatal("relative diagnostic metadata path must be rejected")
	}
}

func TestBridgeRejectsExistingDiagnosticMetadataPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channels-metadata.ndjson")
	if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBridge(BridgeOptions{DiagnosticMetadataPath: path}); err == nil {
		t.Fatal("existing diagnostic metadata path must be rejected before bridge start")
	}
}

func TestBridgeCapturesOnlySafeCandidateMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channels-metadata.ndjson")
	bridge := newBridge(BridgeOptions{DiagnosticMetadataPath: path}, bridgeEnvironment{})
	request := httptest.NewRequest(http.MethodPost, "https://finder.qq.com/finder/feed/detail?token=must-not-be-written", strings.NewReader("request-body-must-not-be-written"))
	request.Header.Set("Cookie", "session=must-not-be-written")
	response := &http.Response{
		StatusCode:    http.StatusCreated,
		Proto:         "HTTP/2.0",
		ContentLength: 321,
		Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
		Body:          io.NopCloser(strings.NewReader("response-body-must-not-be-written")),
	}
	bridge.captureResponse(response, request)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-be-written") {
		t.Fatalf("diagnostic output contains prohibited data: %q", raw)
	}
	var record diagnosticRequestMetadata
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &record); err != nil {
		t.Fatal(err)
	}
	if record.Host != "finder.qq.com" || record.Path != "/finder/feed/detail" || record.Method != http.MethodPost || record.ResponseStatus != http.StatusCreated || record.ContentType != "application/octet-stream" || record.ContentLength != 321 || record.TransportProtocol != "HTTP/2.0" {
		t.Fatalf("unexpected diagnostic metadata: %#v", record)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("diagnostic metadata permissions=%#o; must not expose group/world access", info.Mode().Perm())
	}
}

func TestBridgeDoesNotWriteMetadataForNonCandidateRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channels-metadata.ndjson")
	bridge := newBridge(BridgeOptions{DiagnosticMetadataPath: path}, bridgeEnvironment{})
	request := httptest.NewRequest(http.MethodGet, "https://www.qq.com/other?token=must-not-be-written", nil)
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("body"))}
	bridge.captureResponse(response, request)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-candidate request wrote diagnostic output: %v", err)
	}
}

func TestBridgePageReportAcceptsOnlyDedicatedQQCapability(t *testing.T) {
	bridge := newBridge(BridgeOptions{EnablePageAdapter: true}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.mu.Lock()
	bridge.pageReportToken = "report-token"
	bridge.mu.Unlock()
	handler := bridge.handler(&tls.Certificate{})
	body := `{"object_id":"wanted","object_nonce_id":"wanted-nonce","media_type":4,"title":"wanted title","description":"wanted description","media_url":"https://finder.video.qq.com/video.mp4?encfilekey=key&token=token","decode_key":"123"}`

	unauthorized := httptest.NewRequest(http.MethodPost, "https://channels.weixin.qq.com"+bridgePageReportPath, strings.NewReader(body))
	unauthorized.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthorized)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unauthorized report status=%d", response.Code)
	}

	nonChannels := httptest.NewRequest(http.MethodPost, "https://www.qq.com"+bridgePageReportPath, strings.NewReader(body))
	nonChannels.Header.Set("Content-Type", "application/json")
	nonChannels.Header.Set(bridgeReportHeader, "report-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, nonChannels)
	if response.Code == http.StatusNoContent {
		t.Fatal("only channels.weixin.qq.com may submit a page report")
	}

	accepted := httptest.NewRequest(http.MethodPost, "https://channels.weixin.qq.com"+bridgePageReportPath, strings.NewReader(body))
	accepted.Header.Set("Content-Type", "application/json")
	accepted.Header.Set(bridgeReportHeader, "report-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, accepted)
	if response.Code != http.StatusNoContent {
		t.Fatalf("accepted report status=%d body=%q", response.Code, response.Body.String())
	}
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "wanted" || active.ObjectNonceID != "wanted-nonce" || active.Title != "wanted title" || active.CoverURL != "" {
		t.Fatalf("active page report=%#v", active)
	}

	unknownField := httptest.NewRequest(http.MethodPost, "https://channels.weixin.qq.com"+bridgePageReportPath, strings.NewReader(body[:len(body)-1]+`,"cookie":"must-not-be-accepted"}`))
	unknownField.Header.Set("Content-Type", "application/json")
	unknownField.Header.Set(bridgeReportHeader, "report-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unknownField)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown-field report status=%d", response.Code)
	}
}

func TestBridgePageEndpointRejectsRequestHostDifferentFromTunnelAuthority(t *testing.T) {
	bridge := newBridge(BridgeOptions{EnablePageAdapter: true}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	request := httptest.NewRequest(http.MethodGet, "https://channels.weixin.qq.com"+bridgePageScriptPath, nil)
	response := httptest.NewRecorder()
	if bridge.serveBridgePageEndpoint(response, request, "res.wx.qq.com:443") {
		t.Fatal("page endpoint must be bound to the CONNECT authority")
	}
}

func TestBridgePageEndpointsTerminateInsideInterceptedHTTP1(t *testing.T) {
	bridge := newBridge(BridgeOptions{EnablePageAdapter: true}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.mu.Lock()
	bridge.pageReportToken = "report-token"
	bridge.mu.Unlock()
	client, server := net.Pipe()
	defer client.Close()
	go bridge.serveHTTP1(server, "channels.weixin.qq.com:443", context.Background())
	reader := bufio.NewReader(client)

	scriptRequest := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: bridgePageScriptPath}, Host: "channels.weixin.qq.com"}
	if err := scriptRequest.Write(client); err != nil {
		t.Fatal(err)
	}
	scriptResponse, err := http.ReadResponse(reader, scriptRequest)
	if err != nil {
		t.Fatal(err)
	}
	script, err := io.ReadAll(scriptResponse.Body)
	scriptResponse.Body.Close()
	if err != nil || scriptResponse.StatusCode != http.StatusOK || !strings.Contains(string(script), "__digcrawlWechatChannelsReport") {
		t.Fatalf("script response=%d body=%q err=%v", scriptResponse.StatusCode, script, err)
	}
	bridge.mu.RLock()
	scriptServes := bridge.captureStats.PageScriptServes
	bridge.mu.RUnlock()
	if scriptServes != 1 {
		t.Fatalf("script serves=%d want=1", scriptServes)
	}

	reportBody := `{"object_id":"wanted","object_nonce_id":"wanted-nonce","media_type":4,"media_url":"https://finder.video.qq.com/video.mp4?encfilekey=key&token=token"}`
	reportRequest := &http.Request{
		Method:        http.MethodPost,
		URL:           &url.URL{Path: bridgePageReportPath},
		Host:          "channels.weixin.qq.com",
		Header:        http.Header{"Content-Type": []string{"application/json"}, bridgeReportHeader: []string{"report-token"}},
		Body:          io.NopCloser(strings.NewReader(reportBody)),
		ContentLength: int64(len(reportBody)),
		Close:         true,
	}
	if err := reportRequest.Write(client); err != nil {
		t.Fatal(err)
	}
	reportResponse, err := http.ReadResponse(reader, reportRequest)
	if err != nil {
		t.Fatal(err)
	}
	reportResponse.Body.Close()
	if reportResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("report status=%d", reportResponse.StatusCode)
	}
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "wanted" || active.ObjectNonceID != "wanted-nonce" {
		t.Fatalf("HTTP/1 endpoint was forwarded instead of captured: %#v", active)
	}
}

func TestBridgePageEndpointsTerminateInsideInterceptedHTTP2(t *testing.T) {
	bridge := newBridge(BridgeOptions{EnablePageAdapter: true}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.mu.Lock()
	bridge.pageReportToken = "report-token"
	bridge.mu.Unlock()
	clientConn, serverConn := net.Pipe()
	go bridge.serveHTTP2(serverConn, "channels.weixin.qq.com:443", context.Background())
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(context.Context, string, string, *tls.Config) (net.Conn, error) {
			return clientConn, nil
		},
	}
	client := &http.Client{Transport: transport}
	defer client.CloseIdleConnections()
	defer clientConn.Close()

	scriptResponse, err := client.Get("http://channels.weixin.qq.com" + bridgePageScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	script, err := io.ReadAll(scriptResponse.Body)
	scriptResponse.Body.Close()
	if err != nil || scriptResponse.StatusCode != http.StatusOK || !strings.Contains(string(script), "__digcrawlWechatChannelsReport") {
		t.Fatalf("script response=%d body=%q err=%v", scriptResponse.StatusCode, script, err)
	}
	bridge.mu.RLock()
	scriptServes := bridge.captureStats.PageScriptServes
	bridge.mu.RUnlock()
	if scriptServes != 1 {
		t.Fatalf("script serves=%d want=1", scriptServes)
	}

	reportBody := `{"object_id":"wanted","object_nonce_id":"wanted-nonce","media_type":4,"media_url":"https://finder.video.qq.com/video.mp4?encfilekey=key&token=token"}`
	reportRequest, err := http.NewRequest(http.MethodPost, "http://channels.weixin.qq.com"+bridgePageReportPath, strings.NewReader(reportBody))
	if err != nil {
		t.Fatal(err)
	}
	reportRequest.Header.Set("Content-Type", "application/json")
	reportRequest.Header.Set(bridgeReportHeader, "report-token")
	reportResponse, err := client.Do(reportRequest)
	if err != nil {
		t.Fatal(err)
	}
	reportResponse.Body.Close()
	if reportResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("report status=%d", reportResponse.StatusCode)
	}
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "wanted" || active.ObjectNonceID != "wanted-nonce" {
		t.Fatalf("HTTP/2 endpoint was forwarded instead of captured: %#v", active)
	}
}

func TestPageCaptureScriptOnlyReportsDetailResultWithoutPatchingRuntime(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	bridge.mu.Lock()
	bridge.pageReportToken = "report-token"
	bridge.mu.Unlock()
	script := bridge.pageCaptureScript()
	if !strings.Contains(script, `credentials:"omit"`) {
		t.Fatalf("page report must omit cookies: %q", script)
	}
	for _, required := range []string{"window.__digcrawlWechatChannelsReport=report", "media_url", "decode_key", `credentials:"omit"`} {
		if !strings.Contains(script, required) {
			t.Fatalf("page report script does not enforce runtime capture boundary %q: %q", required, script)
		}
	}
	for _, forbidden := range []string{"XMLHttpRequest.prototype.send", "originalFetch", "document.cookie"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("page report script contains forbidden data source %q", forbidden)
		}
	}
}

func TestPageCaptureScriptParsesInJavaScriptEngine(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	bridge.mu.Lock()
	bridge.pageReportToken = "report-token"
	bridge.mu.Unlock()
	path := filepath.Join(t.TempDir(), "capture.js")
	if err := os.WriteFile(path, []byte(bridge.pageCaptureScript()), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("page capture script syntax error: %v: %s", err, output)
	}
}

func TestVideoIdentityCountFromJSONCountsDistinctValuesOnly(t *testing.T) {
	raw := []byte(`{"feeds":[{"id":"first","objectNonceId":"first-nonce","objectDesc":{"mediaType":4}},{"id":"first","objectNonceId":"first-nonce","objectDesc":{"mediaType":4}},{"id":"second","objectNonceId":"second-nonce","objectDesc":{"mediaType":4}}]}`)
	if got := videoIdentityCountFromJSON(raw); got != 2 {
		t.Fatalf("identity count=%d", got)
	}
}

func TestVideoIdentityFromRequest(t *testing.T) {
	identity, ok := videoIdentityFromRequest([]byte(`{"objectid":"Current-ID","objectNonceId":"Current-Nonce"}`))
	if !ok || identity.objectID != "Current-ID" || identity.objectNonceID != "Current-Nonce" {
		t.Fatalf("unexpected request identity: %#v found=%v", identity, ok)
	}
}

func TestCaptureResponseUsesSingleVideoFeedAndPreservesBody(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "channels.weixin.qq.com", Path: "/opaque/internal/route"}}
	responseBody := `{"feeds":[{"id":"wanted","objectNonceId":"wanted-nonce","description":"wanted title","objectDesc":{"mediaType":4}}]}`
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}, "Transfer-Encoding": []string{"chunked"}}, Body: io.NopCloser(strings.NewReader(responseBody)), TransferEncoding: []string{"chunked"}}
	bridge.captureResponse(resp, req)
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

func TestCaptureResponseDoesNotChooseFromVideoList(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	responseBody := `{"feeds":[{"id":"first","objectNonceId":"first-nonce","objectDesc":{"mediaType":4}},{"id":"second","objectNonceId":"second-nonce","objectDesc":{"mediaType":4}}]}`
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody))}
	bridge.captureResponse(resp, &http.Request{URL: &url.URL{Host: "channels.weixin.qq.com"}})
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "" {
		t.Fatalf("video list must not choose an active video: %#v", active)
	}
}

func TestCaptureResponseReplaysOversizedBody(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	responseBody := `{"id":"wanted","objectNonceId":"wanted-nonce","description":"` + strings.Repeat("a", maxCapturedBody) + `"}`
	resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseBody)), ContentLength: int64(len(responseBody))}
	req := &http.Request{URL: &url.URL{Host: "channels.weixin.qq.com", Path: "/finder/feed/detail"}}
	bridge.captureResponse(resp, req)
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

type partialReadBody struct {
	data   []byte
	offset int
	failed bool
}

func (body *partialReadBody) Read(p []byte) (int, error) {
	if body.offset == len(body.data) {
		return 0, io.EOF
	}
	if !body.failed {
		body.failed = true
		n := copy(p, body.data[body.offset:body.offset+3])
		body.offset += n
		return n, errors.New("interrupted inspection read")
	}
	n := copy(p, body.data[body.offset:])
	body.offset += n
	return n, nil
}

func (body *partialReadBody) Close() error { return nil }

func TestObserveRequestReplaysPartialReadBeforeForwarding(t *testing.T) {
	raw := `{"objectid":"wanted","objectNonceId":"wanted-nonce"}`
	req := &http.Request{
		URL:           &url.URL{Host: "channels.weixin.qq.com", Path: "/finder/feed/detail"},
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          &partialReadBody{data: []byte(raw)},
		ContentLength: int64(len(raw)),
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	if _, ok := bridge.observeRequest(req); ok {
		t.Fatal("partial inspection read must not produce an identity")
	}
	got, err := io.ReadAll(req.Body)
	if err != nil || string(got) != raw {
		t.Fatalf("forwarded request changed: %q err=%v", got, err)
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

func TestCloneURLForOriginPreservesHTTPAndHTTPS(t *testing.T) {
	for _, raw := range []string{"http://channels.weixin.qq.com/finder?a=1", "https://channels.weixin.qq.com/finder?a=1"} {
		source, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		got := cloneURLForOrigin(source, "channels.weixin.qq.com")
		if got.String() != raw {
			t.Fatalf("clone=%q want=%q", got, raw)
		}
	}
}

func TestConnectAuthorityPreservesExplicitPort(t *testing.T) {
	request := httptest.NewRequest(http.MethodConnect, "https://channels.weixin.qq.com:8443", nil)
	request.Host = "channels.weixin.qq.com:8443"
	if got := connectAuthority(request); got != "channels.weixin.qq.com:8443" {
		t.Fatalf("authority=%q", got)
	}
	if got := authorityHost(connectAuthority(request)); got != "channels.weixin.qq.com" {
		t.Fatalf("host=%q", got)
	}
}

func TestRequestAuthorityPreservesProxyRequestPort(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://channels.weixin.qq.com:8080/finder", nil)
	if got := requestAuthority(request); got != "channels.weixin.qq.com:8080" {
		t.Fatalf("authority=%q", got)
	}
}

func TestRemoveHopHeadersRemovesConnectionNamedHeaders(t *testing.T) {
	headers := http.Header{
		"Connection":   []string{"X-Bridge-Hop, Keep-Alive"},
		"X-Bridge-Hop": []string{"must-not-forward"},
		"Keep-Alive":   []string{"timeout=5"},
		"X-Keep":       []string{"forward"},
	}
	removeHopHeaders(headers)
	if headers.Get("Connection") != "" || headers.Get("X-Bridge-Hop") != "" || headers.Get("Keep-Alive") != "" {
		t.Fatalf("hop headers remain: %#v", headers)
	}
	if headers.Get("X-Keep") != "forward" {
		t.Fatalf("end-to-end header was removed: %#v", headers)
	}
}

func TestBridgeAuthorizationIsSafeDuringStateReplacement(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	bridge.mu.Lock()
	bridge.state = bridgeState{ControlToken: "first"}
	bridge.mu.Unlock()
	request := httptest.NewRequest(http.MethodGet, activeVideoAPIPath, nil)
	request.Header.Set(bridgeTokenHeader, "first")

	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		for range 1_000 {
			_ = bridge.authorized(request)
		}
	}()
	go func() {
		defer group.Done()
		for range 1_000 {
			bridge.mu.Lock()
			bridge.state = bridgeState{ControlToken: "first", CertificateSHA1: "FINGERPRINT"}
			bridge.mu.Unlock()
		}
	}()
	group.Wait()
}

func TestBridgeTLSConfigCachesCertificateForSession(t *testing.T) {
	ca, _, err := loadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	first, err := bridge.tlsConfigForHost(ca, "channels.weixin.qq.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := bridge.tlsConfigForHost(ca, "channels.weixin.qq.com")
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Certificates[0].Certificate[0]) != string(second.Certificates[0].Certificate[0]) {
		t.Fatal("repeated TLS connections must reuse the in-memory host certificate")
	}
}

func TestBridgeTLSConfigEvictsOldestCertificateAtCacheLimit(t *testing.T) {
	ca, _, err := loadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	for i := range maxBridgeCertificates {
		host := "host" + strconv.Itoa(i) + ".qq.com"
		bridge.certificates[host] = tls.Certificate{}
		bridge.certificateHosts = append(bridge.certificateHosts, host)
	}
	if _, err := bridge.tlsConfigForHost(ca, "channels.weixin.qq.com"); err != nil {
		t.Fatal(err)
	}
	if _, retained := bridge.certificates["host0.qq.com"]; len(bridge.certificates) != maxBridgeCertificates || retained {
		t.Fatalf("certificate cache was not bounded with oldest eviction: %#v", bridge.certificateHosts)
	}
}

func TestBridgeRejectsAmbiguousActiveVideo(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.state.ControlToken = "token"
	bridge.recordActiveVideo(activeVideoResponse{ObjectID: "first", ObjectNonceID: "first-nonce"})
	bridge.recordActiveVideo(activeVideoResponse{ObjectID: "second", ObjectNonceID: "second-nonce"})
	handler := bridge.handler(&tls.Certificate{})
	request := httptest.NewRequest(http.MethodGet, activeVideoAPIPath, nil)
	request.Host = "127.0.0.1"
	request.Header.Set(bridgeTokenHeader, "token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestBridgeDoesNotServeActiveVideoOutsideRunningPhase(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	bridge.state.ControlToken = "token"
	bridge.active = activeVideoResponse{ObjectID: "wanted", ObjectNonceID: "wanted-nonce"}
	handler := bridge.handler(&tls.Certificate{})
	request := httptest.NewRequest(http.MethodGet, activeVideoAPIPath, nil)
	request.Host = "127.0.0.1"
	request.Header.Set(bridgeTokenHeader, "token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestBridgeConnectionCancellationStopsInFlightForwarding(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	left, right := net.Pipe()
	defer right.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if !bridge.trackConnection(left, cancel) {
		t.Fatal("expected connection to be tracked")
	}
	bridge.closeConnections()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("closeConnections did not cancel the forwarding context")
	}
	if _, err := right.Write([]byte("x")); err == nil {
		t.Fatal("closeConnections did not close the intercepted connection")
	}
}

func TestBridgeRequestContextCancelsWhenConnectionStops(t *testing.T) {
	connectionCtx, cancelConnection := context.WithCancel(context.Background())
	ctx, cancel := bridgeRequestContext(context.Background(), connectionCtx)
	defer cancel()
	cancelConnection()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("connection cancellation did not reach HTTP/2 request context")
	}
}

func TestBridgeForwardReturnsWhenConnectionContextIsCancelled(t *testing.T) {
	started := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.transport = &http.Transport{Proxy: nil}
	request, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := bridge.forward(ctx, request, originURL.Host)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("bridge did not reach the delayed upstream")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("forward error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("forward did not return after connection cancellation")
	}
}

func TestStateMachineDoesNotEmitSequentialVersionAndReadsLegacyState(t *testing.T) {
	stateDir := t.TempDir()
	path := filepath.Join(stateDir, bridgeStateFile)
	state := bridgeState{StateMachine: true, Service: "Wi-Fi", Port: 20261, ProxyMode: "pac", ControlToken: "token", DownloadProtocolVersion: downloadProtocolVersion, OwnerPID: 1}
	if err := writeBridgeState(path, state); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"version":`) || !strings.Contains(string(raw), `"state_machine":true`) || !strings.Contains(string(raw), `"download_protocol_version":1`) {
		t.Fatalf("new state must identify its state machine and download capability without a sequential version: %s", raw)
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
	setBridgeRunning(bridge)
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

func TestDownloadReadinessEndpointRevealsNoMediaSecrets(t *testing.T) {
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.state.ControlToken = "token"
	handler := bridge.handler(&tls.Certificate{})

	request := httptest.NewRequest(http.MethodGet, bridgeDownloadReadyPath, nil)
	request.Host = "127.0.0.1"
	request.Header.Set(bridgeTokenHeader, "token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"ready":false}` {
		t.Fatalf("empty readiness status/body=%d/%q", recorder.Code, recorder.Body.String())
	}

	bridge.mu.Lock()
	bridge.activeDownload = activeDownloadResource{
		URL:           "https://media.example.invalid/private-url",
		ObjectID:      "object-id",
		ObjectNonceID: "nonce-id",
		DecodeKey:     1,
	}
	bridge.mu.Unlock()
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"ready":true}` {
		t.Fatalf("ready status/body=%d/%q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "private-url") || strings.Contains(recorder.Body.String(), "object-id") || strings.Contains(recorder.Body.String(), "nonce-id") {
		t.Fatalf("readiness response leaked capture material: %q", recorder.Body.String())
	}

	bridge.mu.Lock()
	bridge.activeAmbiguous = true
	bridge.mu.Unlock()
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("ambiguous readiness status=%d", recorder.Code)
	}
}

func TestBridgeCapturesHTTP2AndUsesOriginalSecureProxy(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("origin protocol=%s; want HTTP/2", r.Proto)
		}
		if r.URL.Path != "/finder/feed/detail" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"feeds":[{"id":"wanted","objectNonceId":"wanted-nonce","description":"wanted title","objectDesc":{"mediaType":4}}]}`); err != nil {
			t.Error(err)
		}
	}))
	origin.EnableHTTP2 = true
	origin.StartTLS()
	defer origin.Close()

	var proxied bool
	originalProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		proxied = true
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("proxy response writer cannot hijack")
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer upstream.Close()
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, client); close(done) }()
		_, _ = io.Copy(client, upstream)
		<-done
	}))
	defer originalProxy.Close()

	proxyURL, err := url.Parse(originalProxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxyHost, proxyPort, err := net.SplitHostPort(proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(proxyPort)
	if err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	ca, _, err := loadOrCreateCA(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.secureProxy = proxyConfig{Enabled: true, Server: proxyHost, Port: port}
	bridge.transport = &http.Transport{
		Proxy:              bridge.outboundProxy,
		DisableCompression: true,
		DisableKeepAlives:  true,
		ForceAttemptHTTP2:  true,
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true}, // test origin only
	}
	bridgeServer := httptest.NewServer(bridge.handler(ca))
	defer bridgeServer.Close()
	defer bridge.closeConnections()

	bridgeURL, err := url.Parse(bridgeServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := x509.ParseCertificate(ca.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parent)
	client := &http.Client{Transport: &http.Transport{
		Proxy:             http.ProxyURL(bridgeURL),
		TLSClientConfig:   &tls.Config{RootCAs: roots},
		ForceAttemptHTTP2: true,
		DisableKeepAlives: true,
	}}
	request, err := http.NewRequest(http.MethodPost, "https://channels.weixin.qq.com/finder/feed/detail", strings.NewReader(`{"objectid":"wanted","objectNonceId":"wanted-nonce"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 || response.StatusCode != http.StatusOK {
		t.Fatalf("response protocol/status=%s/%d", response.Proto, response.StatusCode)
	}
	if body, err := io.ReadAll(response.Body); err != nil || !strings.Contains(string(body), "wanted title") {
		t.Fatalf("response body=%q err=%v", body, err)
	}
	if !proxied {
		t.Fatal("outbound request bypassed the original secure proxy")
	}
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "wanted" || active.ObjectNonceID != "wanted-nonce" {
		t.Fatalf("active video=%#v", active)
	}
}

func TestBridgeStartOwnsTransportUntilStop(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	bridge := newBridge(BridgeOptions{Port: 20261, StateDir: stateDir}, bridgeEnvironment{
		run:        &fakeRunner{},
		listen:     func(string, string) (net.Listener, error) { return listener, nil },
		startApp:   func(context.Context, string) error { return nil },
		goos:       "darwin",
		loadCA:     func(string) (*tls.Certificate, string, error) { return &tls.Certificate{}, "FINGERPRINT", nil },
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	})
	if err := bridge.Start(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	bridge.mu.RLock()
	phase, transport, reportToken := bridge.phase, bridge.transport, bridge.pageReportToken
	bridge.mu.RUnlock()
	if phase != bridgeRunning || transport == nil || reportToken == "" || transport.DisableCompression || !transport.ForceAttemptHTTP2 || !transport.DisableKeepAlives {
		t.Fatalf("running bridge must own an isolated HTTP/2 transport: phase=%s transport=%#v", phase, transport)
	}
	if err := bridge.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	bridge.mu.RLock()
	phase, transport, reportToken = bridge.phase, bridge.transport, bridge.pageReportToken
	bridge.mu.RUnlock()
	if phase != bridgeIdle || transport != nil || reportToken != "" {
		t.Fatalf("stopped bridge retained runtime transport: phase=%s transport=%#v", phase, transport)
	}
}

func TestBridgeForwardDecodesGzipForVideoCapture(t *testing.T) {
	const body = `{"id":"wanted","objectNonceId":"wanted-nonce","objectDesc":{"mediaType":4}}`
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("Accept-Encoding=%q; want gzip", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		if _, err := io.WriteString(writer, body); err != nil {
			t.Error(err)
		}
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	bridge := newBridge(BridgeOptions{}, bridgeEnvironment{})
	setBridgeRunning(bridge)
	bridge.transport = &http.Transport{Proxy: nil}
	request, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "br")
	response, err := bridge.forward(context.Background(), request, originURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Encoding") != "" {
		t.Fatalf("response remained encoded: %#v", response.Header)
	}
	bridge.captureResponse(response, request)
	bridge.mu.RLock()
	active := bridge.active
	bridge.mu.RUnlock()
	if active.ObjectID != "wanted" || active.ObjectNonceID != "wanted-nonce" {
		t.Fatalf("gzip response was not captured: %#v", active)
	}
}

func TestBridgeStopCleansOwnedRuntimeWhenRecoveryStateIsMissing(t *testing.T) {
	bridge := newBridge(BridgeOptions{StateDir: t.TempDir()}, bridgeEnvironment{
		readState: func(string) (bridgeState, error) { return bridgeState{}, os.ErrNotExist },
		remove:    func(string) error { return os.ErrNotExist },
	})
	bridge.mu.Lock()
	bridge.phase = bridgeRunning
	bridge.state = bridgeState{StateMachine: true, Service: "Wi-Fi", Port: 20261, ProxyMode: "pac", ControlToken: "token", OwnerPID: os.Getpid()}
	bridge.transport = &http.Transport{}
	bridge.mu.Unlock()
	if err := bridge.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	bridge.mu.RLock()
	phase, transport := bridge.phase, bridge.transport
	bridge.mu.RUnlock()
	if phase != bridgeIdle || transport != nil {
		t.Fatalf("owned runtime survived missing-state stop: phase=%s transport=%#v", phase, transport)
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

func TestBridgeStartRollsBackWhenOpeningWeChatFails(t *testing.T) {
	runner := &fakeRunner{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	bridge := newBridge(BridgeOptions{Port: 20261, StateDir: stateDir}, bridgeEnvironment{
		run:        runner,
		listen:     func(string, string) (net.Listener, error) { return listener, nil },
		startApp:   func(context.Context, string) error { return errors.New("WeChat unavailable") },
		goos:       "darwin",
		loadCA:     func(string) (*tls.Certificate, string, error) { return &tls.Certificate{}, "FINGERPRINT", nil },
		writeState: writeBridgeState,
		readState:  readBridgeState,
		remove:     os.Remove,
	})
	if err := bridge.Start(context.Background(), true); err == nil || !strings.Contains(err.Error(), "WeChat unavailable") {
		t.Fatalf("unexpected start error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, bridgeStateFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery state must be removed after app launch rollback, got %v", err)
	}
	runner.requireCall(t, "networksetup -setautoproxystate Wi-Fi off")
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
