// Package wechatchannels imports the video currently exposed by a
// user-authorized, local WeChat desktop bridge. It intentionally does not
// automate WeChat or retain the bridge's session material.
package wechatchannels

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName       = "wechat_channels"
	ContentType        = "video"
	DefaultBridgeURL   = "http://127.0.0.1:20261"
	activeVideoAPIPath = "/v1/wechat-channels/active-video"
)

// ErrActiveDownloadUnavailable means the local page adapter has not yet
// reported one downloadable video. It is safe for a foreground workflow to
// retry this condition; all other bridge errors remain terminal.
var ErrActiveDownloadUnavailable = errors.New("local WeChat bridge has not captured a downloadable video")

const (
	bridgeRequestTimeout = 5 * time.Second
	maxBridgeResponse    = 1 << 20
)

type activeVideoResponse struct {
	ObjectID      string `json:"object_id"`
	ObjectNonceID string `json:"object_nonce_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	AuthorID      string `json:"author_id"`
	AuthorName    string `json:"author_name"`
	PublishedAt   string `json:"published_at"`
	CoverURL      string `json:"cover_url"`
}

// ImportActive reads the current video from the local bridge and converts it
// to DigCrawl's existing platform.Content model. bridgeURL must be a loopback
// HTTP(S) endpoint; this keeps a local companion from becoming an arbitrary
// network proxy.
func ImportActive(ctx context.Context, bridgeURL string) (*platform.Content, error) {
	return importActive(ctx, bridgeURL, "")
}

// ImportActiveFromState reads the capability token from the bridge's protected
// recovery state before accessing the active-video endpoint.
func ImportActiveFromState(ctx context.Context, bridgeURL, stateDir string) (*platform.Content, error) {
	state, err := readLocalBridgeState(stateDir)
	if err != nil {
		return nil, err
	}
	return importActive(ctx, bridgeURL, state.ControlToken)
}

// CheckDownloadBridgeCompatibility verifies that the running bridge recorded
// the bounded-download protocol before the CLI asks the user for confirmation.
// Old bridge state deliberately fails closed: retrying without max_bytes would
// remove the download-size safety boundary.
func CheckDownloadBridgeCompatibility(stateDir string) error {
	state, err := readLocalBridgeState(stateDir)
	if err != nil {
		return err
	}
	if state.DownloadProtocolVersion < downloadProtocolVersion {
		return fmt.Errorf("local WeChat bridge does not support bounded downloads; stop it and start a new bridge before downloading")
	}
	return nil
}

// DownloadActiveFromState asks the running, local bridge to download the
// current page-adapter capture to outputPath. The playback URL and optional
// decoding key stay inside the bridge process and are never returned here.
func DownloadActiveFromState(ctx context.Context, bridgeURL, stateDir, outputPath string, maxBytes int64) (activeDownloadResponse, error) {
	if maxBytes <= 0 || maxBytes == maxInt64 {
		return activeDownloadResponse{}, fmt.Errorf("video download maximum size must be between 1 and %d bytes", maxInt64-1)
	}
	state, err := readLocalBridgeState(stateDir)
	if err != nil {
		return activeDownloadResponse{}, err
	}
	if state.DownloadProtocolVersion < downloadProtocolVersion {
		return activeDownloadResponse{}, fmt.Errorf("local WeChat bridge does not support bounded downloads; stop it and start a new bridge before downloading")
	}
	endpoint, err := bridgeEndpointForPath(bridgeURL, bridgeDownloadPath)
	if err != nil {
		return activeDownloadResponse{}, err
	}
	body, err := json.Marshal(activeDownloadRequest{OutputPath: outputPath, MaxBytes: maxBytes})
	if err != nil {
		return activeDownloadResponse{}, fmt.Errorf("encode local WeChat download request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return activeDownloadResponse{}, fmt.Errorf("build local WeChat download request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(bridgeTokenHeader, state.ControlToken)
	// The bridge deliberately responds only after it has atomically written
	// the complete video. Unlike metadata reads, a real video download may
	// take far longer than bridgeRequestTimeout; cancellation is supplied by
	// the caller's context instead of an unrelated five-second deadline.
	client := localBridgeDownloadClient()
	response, err := client.Do(request)
	if err != nil {
		return activeDownloadResponse{}, fmt.Errorf("request local WeChat video download: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		if response.StatusCode == http.StatusNotFound {
			if stats := strings.TrimSpace(response.Header.Get("X-DigCrawl-Bridge-Capture-Stats")); stats != "" {
				return activeDownloadResponse{}, fmt.Errorf("%w (%s)", ErrActiveDownloadUnavailable, stats)
			}
			return activeDownloadResponse{}, ErrActiveDownloadUnavailable
		}
		return activeDownloadResponse{}, fmt.Errorf("local WeChat video download returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	var result activeDownloadResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxBridgeResponse))
	if err := decoder.Decode(&result); err != nil {
		return activeDownloadResponse{}, fmt.Errorf("decode local WeChat video download response: %w", err)
	}
	if strings.TrimSpace(result.OutputPath) != filepath.Clean(outputPath) || result.Bytes < 1 {
		return activeDownloadResponse{}, fmt.Errorf("local WeChat bridge returned an invalid video download result")
	}
	return result, nil
}

// ActiveDownloadReadyFromState reports whether the running local bridge has
// one unambiguous, downloadable video. It is a short control-plane request;
// callers must use DownloadActiveFromState with their own context for the
// potentially long media transfer.
func ActiveDownloadReadyFromState(ctx context.Context, bridgeURL, stateDir string) (bool, error) {
	state, err := readLocalBridgeState(stateDir)
	if err != nil {
		return false, err
	}
	endpoint, err := bridgeEndpointForPath(bridgeURL, bridgeDownloadReadyPath)
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("build local WeChat download readiness request: %w", err)
	}
	request.Header.Set(bridgeTokenHeader, state.ControlToken)
	response, err := localBridgeClient().Do(request)
	if err != nil {
		return false, fmt.Errorf("read local WeChat download readiness: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return false, fmt.Errorf("local WeChat download readiness returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	var result activeDownloadReadyResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxBridgeResponse))
	if err := decoder.Decode(&result); err != nil {
		return false, fmt.Errorf("decode local WeChat download readiness response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return false, fmt.Errorf("decode local WeChat download readiness response: response must contain one JSON object")
		}
		return false, fmt.Errorf("decode local WeChat download readiness response: %w", err)
	}
	return result.Ready, nil
}

func readLocalBridgeState(stateDir string) (bridgeState, error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = defaultBridgeStateDir
	}
	state, err := readBridgeState(filepath.Join(stateDir, bridgeStateFile))
	if err != nil {
		return bridgeState{}, fmt.Errorf("read local WeChat bridge state: %w", err)
	}
	if state.ControlToken == "" {
		return bridgeState{}, fmt.Errorf("local WeChat bridge state has no control token; stop recovery then start a new bridge")
	}
	return state, nil
}

func importActive(ctx context.Context, bridgeURL, controlToken string) (*platform.Content, error) {
	endpoint, err := bridgeEndpoint(bridgeURL)
	if err != nil {
		return nil, err
	}

	client := localBridgeClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build local WeChat bridge request: %w", err)
	}
	if controlToken != "" {
		req.Header.Set(bridgeTokenHeader, controlToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read local WeChat bridge: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("local WeChat bridge observed multiple videos; restart the bridge and open only the video to import")
	}
	if resp.StatusCode == http.StatusNotFound {
		if stats := strings.TrimSpace(resp.Header.Get("X-DigCrawl-Bridge-Capture-Stats")); stats != "" {
			return nil, fmt.Errorf("local WeChat bridge has not captured a video (%s)", stats)
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local WeChat bridge returned HTTP %d", resp.StatusCode)
	}

	raw, err := decodeActiveVideo(resp.Body)
	if err != nil {
		return nil, err
	}
	return contentFromActiveVideo(raw)
}

func decodeActiveVideo(body io.Reader) (activeVideoResponse, error) {
	var raw activeVideoResponse
	decoder := json.NewDecoder(io.LimitReader(body, maxBridgeResponse))
	if err := decoder.Decode(&raw); err != nil {
		return activeVideoResponse{}, fmt.Errorf("decode local WeChat bridge response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return activeVideoResponse{}, fmt.Errorf("decode local WeChat bridge response: response must contain one JSON object")
		}
		return activeVideoResponse{}, fmt.Errorf("decode local WeChat bridge response: %w", err)
	}
	return raw, nil
}

func bridgeEndpoint(raw string) (string, error) {
	return bridgeEndpointForPath(raw, activeVideoAPIPath)
}

func bridgeEndpointForPath(raw, path string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultBridgeURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid local WeChat bridge URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("local WeChat bridge URL must use http or https")
	}
	if u.User != nil || !isLoopbackHost(u.Hostname()) {
		return "", fmt.Errorf("local WeChat bridge URL must use a loopback host")
	}
	// Resolve localhost to a literal loopback address before issuing the
	// request, so a modified hosts/DNS configuration cannot redirect it.
	if strings.EqualFold(u.Hostname(), "localhost") {
		port := u.Port()
		u.Host = "127.0.0.1"
		if port != "" {
			u.Host = net.JoinHostPort("127.0.0.1", port)
		}
	}
	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func localBridgeClient() *http.Client {
	return &http.Client{
		Timeout: bridgeRequestTimeout,
		Transport: &http.Transport{
			// The bridge responses describe private, currently opened content.
			// Never allow HTTP_PROXY/HTTPS_PROXY to forward loopback requests.
			Proxy: nil,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func localBridgeDownloadClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			// Never route the local bridge control call through a user proxy.
			Proxy: nil,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func contentFromActiveVideo(raw activeVideoResponse) (*platform.Content, error) {
	raw.ObjectID = strings.TrimSpace(raw.ObjectID)
	raw.ObjectNonceID = strings.TrimSpace(raw.ObjectNonceID)
	if raw.ObjectID == "" || raw.ObjectNonceID == "" {
		return nil, fmt.Errorf("local WeChat bridge did not return object_id and object_nonce_id; open and play a video card first")
	}

	title := strings.TrimSpace(raw.Title)
	description := strings.TrimSpace(raw.Description)
	if title == "" {
		title = description
	}
	if title == "" {
		title = "未命名视频号视频"
	}

	source := canonicalVideoURL(raw.ObjectID, raw.ObjectNonceID)
	meta := map[string]any{
		"object_id":       raw.ObjectID,
		"object_nonce_id": raw.ObjectNonceID,
		"import_channel":  "local_wechat_bridge",
	}
	if cover := strings.TrimSpace(raw.CoverURL); cover != "" {
		meta["cover_url"] = cover
	}
	content := &platform.Content{
		Platform:         PlatformName,
		ContentType:      ContentType,
		SourceURL:        source,
		CanonicalURL:     source,
		Title:            title,
		Text:             description,
		Markdown:         description,
		PublishedAt:      strings.TrimSpace(raw.PublishedAt),
		CapturedAt:       platform.NowRFC3339(),
		PlatformMetadata: meta,
	}
	if authorName := strings.TrimSpace(raw.AuthorName); authorName != "" || strings.TrimSpace(raw.AuthorID) != "" {
		content.Author = &platform.Author{ID: strings.TrimSpace(raw.AuthorID), Name: authorName}
	}
	return content, nil
}

func canonicalVideoURL(objectID, nonce string) string {
	// Collect's existing index and SQLite store normalize source URLs to lower
	// case. Hex keeps an opaque, potentially case-sensitive platform identity
	// collision-free without changing that shared URL behavior.
	return "wechat-channels://video/" + hex.EncodeToString([]byte(objectID)) + "?nonce=" + hex.EncodeToString([]byte(nonce))
}
