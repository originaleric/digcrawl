package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCDPDiscoveryResponseBodyIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &http.Client{Transport: &boundedCDPDiscoveryTransport{base: http.DefaultTransport, limit: 30 * time.Millisecond}}
	start := time.Now()
	resp, err := client.Get(server.URL + "/json/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err = io.ReadAll(resp.Body); err == nil || time.Since(start) > time.Second {
		t.Fatalf("body limit elapsed=%v err=%v", time.Since(start), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCDPDiscoveryWrapperLeavesOtherRequestsUnchanged(t *testing.T) {
	for _, target := range []string{"https://example.com/json/version", "http://127.0.0.1/article", "http://example.com/json/version"} {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		base := roundTripFunc(func(actual *http.Request) (*http.Response, error) {
			if actual != req {
				t.Fatal("unrelated request cloned or limited")
			}
			return nil, nil
		})
		wrapper := &boundedCDPDiscoveryTransport{base: base, limit: time.Millisecond}
		if _, err := wrapper.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCDPDiscoveryDoesNotFollowRedirectOutsideBoundedPath(t *testing.T) {
	visited := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			http.Redirect(w, r, "/unbounded", http.StatusFound)
			return
		}
		visited = true
	}))
	defer server.Close()
	resp, err := http.Get(server.URL + "/json/version")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || visited {
		t.Fatal("CDP discovery escaped its bounded contract")
	}
}
