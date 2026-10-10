package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Rod Launcher.ResolveURL uses http.Get without a context. Limit only its
// loopback CDP discovery contract, including response-body reads. Installation
// happens at package initialization, before browser work or concurrent calls.
func init() {
	client := *http.DefaultClient
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &boundedCDPDiscoveryTransport{base: base, limit: cdpDiscoveryTimeout}
	redirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && isLoopbackCDPDiscovery(via[0]) {
			return http.ErrUseLastResponse
		}
		if redirect != nil {
			return redirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	http.DefaultClient = &client
}

type boundedCDPDiscoveryTransport struct {
	base  http.RoundTripper
	limit time.Duration
}

func (t *boundedCDPDiscoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isLoopbackCDPDiscovery(req) {
		return t.base.RoundTrip(req)
	}
	ctx, cancel := context.WithTimeout(req.Context(), t.limit)
	resp, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &discoveryResponseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type discoveryResponseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func isLoopbackCDPDiscovery(req *http.Request) bool {
	host := req.URL.Hostname()
	return req.Method == http.MethodGet && req.URL.Scheme == "http" && req.URL.Path == "/json/version" && (host == "127.0.0.1" || host == "localhost" || host == "::1")
}

func (b *discoveryResponseBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}
