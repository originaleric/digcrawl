package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestFeishuNotifierSendsAndTrims(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		got = string(buf)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()
	n := NewFeishu(config.FeishuNotificationConfig{
		DefaultChannel: "c1",
		Channels:       []config.FeishuChannel{{ID: "c1", WebhookURL: srv.URL, MaxMessageChars: 20}},
	})
	res := n.Send(context.Background(), "", Message{Title: "t", Text: strings.Repeat("x", 100)})
	if res.Status != "notified" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected trimmed payload, got %s", got)
	}
}

func TestFeishuNotifierClassifiesHTTP500Retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	n := NewFeishu(config.FeishuNotificationConfig{
		DefaultChannel: "c1",
		Channels:       []config.FeishuChannel{{ID: "c1", WebhookURL: srv.URL}},
	})
	res := n.Send(context.Background(), "", Message{Text: "hello"})
	if !res.Retryable || res.Status != "notify_failed" {
		t.Fatalf("expected retryable failure, got %+v", res)
	}
}

func TestFeishuNotifierNormalizesChannelID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()
	n := NewFeishu(config.FeishuNotificationConfig{
		DefaultChannel: " c1 ",
		Channels:       []config.FeishuChannel{{ID: " c1 ", WebhookURL: srv.URL}},
	})
	res := n.Send(context.Background(), " c1 ", Message{Text: "hello"})
	if res.Status != "notified" || res.ChannelID != "c1" {
		t.Fatalf("expected normalized channel success, got %+v", res)
	}
}
