package wechatchannels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const desktopInspectTimeout = 2 * time.Second

// ErrDesktopDebugUnavailable means that the running desktop client did not
// expose a standard, loopback-only Chrome DevTools Protocol endpoint. DigCrawl
// never starts WeChat with remote-debugging flags as a fallback.
var ErrDesktopDebugUnavailable = errors.New("WeChat desktop does not expose a Chrome DevTools Protocol endpoint")

// DesktopTarget is the deliberately small, non-secret view of a debuggable
// browser target. It excludes the WebSocket endpoint, cookies, headers, and
// all response bodies.
type DesktopTarget struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type cdpTarget struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// InspectDesktop returns Channels targets exposed by a running WeChat desktop
// client through a pre-existing, loopback-only CDP endpoint. endpoint may be
// supplied for an explicit local endpoint; otherwise the function inspects
// only WeChat's listening loopback ports. It has no write or automation path.
func InspectDesktop(ctx context.Context, endpoint string) ([]DesktopTarget, error) {
	var endpoints []string
	var err error
	if strings.TrimSpace(endpoint) != "" {
		normalized, err := normalizeCDPEndpoint(endpoint)
		if err != nil {
			return nil, err
		}
		endpoints = []string{normalized}
	} else {
		endpoints, err = discoverDesktopCDPEndpoints(ctx)
		if err != nil {
			return nil, err
		}
	}

	for _, candidate := range endpoints {
		targets, supported, err := inspectCDPEndpoint(ctx, candidate)
		if err != nil || !supported {
			continue
		}
		return channelsTargets(targets), nil
	}
	return nil, ErrDesktopDebugUnavailable
}

func discoverDesktopCDPEndpoints(ctx context.Context) ([]string, error) {
	// lsof reads the current user's socket table. Restricting it to the WeChat
	// process and LISTEN sockets avoids a host-wide port scan.
	command := exec.CommandContext(ctx, "lsof", "-nP", "-a", "-c", "WeChat", "-iTCP", "-sTCP:LISTEN")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) == 0 {
			return nil, ErrDesktopDebugUnavailable
		}
		return nil, fmt.Errorf("inspect WeChat loopback listeners: %w", err)
	}
	ports := parseLoopbackListenPorts(string(output))
	endpoints := make([]string, 0, len(ports))
	for _, port := range ports {
		endpoints = append(endpoints, "http://127.0.0.1:"+strconv.Itoa(port))
	}
	return endpoints, nil
}

func parseLoopbackListenPorts(output string) []int {
	seen := make(map[int]struct{})
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasSuffix(fields[len(fields)-1], "(LISTEN)") {
			continue
		}
		address := strings.TrimSuffix(fields[len(fields)-2], "")
		host, portText, err := net.SplitHostPort(address)
		if err != nil || !isLoopbackHost(host) {
			continue
		}
		port, err := strconv.Atoi(portText)
		if err == nil && port > 0 && port <= 65535 {
			seen[port] = struct{}{}
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func normalizeCDPEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid desktop CDP endpoint: %w", err)
	}
	if u.Scheme != "http" || u.User != nil || !isLoopbackHost(u.Hostname()) || u.Port() == "" {
		return "", fmt.Errorf("desktop CDP endpoint must be an http loopback address with an explicit port")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("desktop CDP endpoint has an invalid port")
	}
	u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

func inspectCDPEndpoint(ctx context.Context, endpoint string) ([]cdpTarget, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, desktopInspectTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint+"/json/list", nil)
	if err != nil {
		return nil, false, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		return nil, false, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false, nil
	}
	var targets []cdpTarget
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&targets); err != nil {
		return nil, false, nil
	}
	return targets, true, nil
}

func channelsTargets(raw []cdpTarget) []DesktopTarget {
	targets := make([]DesktopTarget, 0, len(raw))
	for _, target := range raw {
		u, err := url.Parse(target.URL)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || !isChannelsHost(u.Hostname()) {
			continue
		}
		targets = append(targets, DesktopTarget{
			Type:  strings.TrimSpace(target.Type),
			Title: strings.TrimSpace(target.Title),
			URL:   u.String(),
		})
	}
	return targets
}

func isChannelsHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "channels.weixin.qq.com" || strings.HasSuffix(host, ".channels.weixin.qq.com")
}
