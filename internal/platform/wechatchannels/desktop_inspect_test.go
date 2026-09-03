package wechatchannels

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseLoopbackListenPorts(t *testing.T) {
	output := `COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
WeChat 1 dig 3u IPv4 0x1 0t0 TCP 127.0.0.1:14013 (LISTEN)
WeChat 1 dig 4u IPv6 0x1 0t0 TCP [::1]:14014 (LISTEN)
WeChat 1 dig 5u IPv4 0x1 0t0 TCP *:14015 (LISTEN)
WeChat 1 dig 6u IPv4 0x1 0t0 TCP 127.0.0.1:14013 (LISTEN)`
	ports := parseLoopbackListenPorts(output)
	if want := []int{14013, 14014}; !reflect.DeepEqual(ports, want) {
		t.Fatalf("ports = %v, want %v", ports, want)
	}
}

func TestNormalizeCDPEndpoint(t *testing.T) {
	got, err := normalizeCDPEndpoint("http://localhost:9222/ignored?x=y")
	if err != nil || got != "http://127.0.0.1:9222" {
		t.Fatalf("normalizeCDPEndpoint() = %q, %v", got, err)
	}
	if _, err := normalizeCDPEndpoint("https://example.com:9222"); err == nil {
		t.Fatal("non-loopback endpoint was accepted")
	}
}

func TestInspectDesktopFiltersToChannelsTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[
			{"type":"page","title":"Video","url":"https://channels.weixin.qq.com/finder/live"},
			{"type":"page","title":"Other","url":"https://example.com/"},
			{"type":"page","title":"Bad","url":"not-a-url"}
		]`))
	}))
	defer server.Close()

	targets, err := InspectDesktop(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("InspectDesktop() error = %v", err)
	}
	if len(targets) != 1 || targets[0].Title != "Video" || targets[0].URL != "https://channels.weixin.qq.com/finder/live" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestInspectDesktopReportsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	_, err := InspectDesktop(context.Background(), server.URL)
	if !errors.Is(err, ErrDesktopDebugUnavailable) {
		t.Fatalf("InspectDesktop() error = %v, want ErrDesktopDebugUnavailable", err)
	}
}
