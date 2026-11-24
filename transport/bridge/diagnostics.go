package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime/version"
)

var ErrBuildMismatch = errors.New("local Bridge build mismatch: close old MCP clients, run the installed digcrawl bridge restart --config <actual profile>, then reconnect")

func checkBuild(m protocol.ToolManifest) error {
	if m.BuildID == "" || m.BuildID == "unavailable" || m.BuildID != version.BuildIdentity() || m.RuntimeVersion != version.RuntimeVersion || m.Runtime != version.RuntimeName {
		return ErrBuildMismatch
	}
	return nil
}

// Wrap only the private Bridge. The public HTTP server never registers this
// diagnostic path; ownership and normal acquisition remain inside the Bridge.
func privateBridgeHandler(gateway http.Handler, stop func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/stop" {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			stop()
			return
		}
		if r.URL.Path != "/internal/browser-check" {
			gateway.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		err := browser.CheckOperation(ctx)
		result := map[string]any{"build_id": version.BuildIdentity(), "status": "success"}
		if err != nil {
			result["status"] = "error"
			result["message"] = "browser operation check failed; run doctor and inspect the reported operation"
			var e *browser.BrowseError
			if errors.As(err, &e) {
				result["details"] = e.ToolErrorDetails()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

// A shutdown acknowledgement means "draining", never "all work finished".
// The controller still waits for the lifetime lock and browser release.
func requestBridgeStop(ctx context.Context, socket string) (bool, error) {
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(check, http.MethodPost, "http://local/internal/stop", nil)
	if err != nil {
		return false, err
	}
	response, err := newSocketClient(socket).HTTPClient.Do(request)
	if err != nil {
		return false, nil
	} // unreachable: native identity-based fallback
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		return true, nil
	}
	return false, fmt.Errorf("Bridge does not support draining shutdown; wait for its tasks to finish and stop the old version before updating (HTTP %d)", response.StatusCode)
}

// CheckBridge only connects. It never starts a Bridge or a competing browser.
func CheckBridge(ctx context.Context, profile string) (protocol.ToolManifest, error) {
	socket, err := SocketPath(profile)
	if err != nil {
		return protocol.ToolManifest{}, err
	}
	_, m, err := probe(ctx, socket)
	return m, err
}
func CheckBridgeBuild(ctx context.Context, profile string) error {
	m, err := CheckBridge(ctx, profile)
	if err != nil {
		return err
	}
	return checkBuild(m)
}
func CheckBrowserOperation(ctx context.Context, profile string) error {
	socket, err := SocketPath(profile)
	if err != nil {
		return err
	}
	c, m, err := probe(ctx, socket)
	if err != nil {
		return fmt.Errorf("browser operation not executed: Bridge unreachable; start/restart the matching Bridge")
	}
	if err = checkBuild(m); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://digcrawl-bridge/internal/browser-check", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("browser operation check incomplete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("browser operation not executed: old Bridge does not support diagnostics; restart explicitly")
	}
	var result struct {
		Status  string         `json:"status"`
		BuildID string         `json:"build_id"`
		Details map[string]any `json:"details"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("invalid Bridge diagnostic response")
	}
	if result.BuildID != version.BuildIdentity() {
		return ErrBuildMismatch
	}
	if resp.StatusCode != http.StatusOK || result.Status != "success" {
		return fmt.Errorf("browser operation failed: %v", result.Details)
	}
	return nil
}
