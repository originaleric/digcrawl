package notify

import (
	"context"
	"encoding/json"
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

func TestFeishuNotifierSendsAppMessage(t *testing.T) {
	var gotAuth string
	var gotReceiveIDType string
	var gotPayload struct {
		ReceiveID string `json:"receive_id"`
		MsgType   string `json:"msg_type"`
		Content   string `json:"content"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tenant-token"}`))
		case "/open-apis/im/v1/messages":
			gotAuth = r.Header.Get("Authorization")
			gotReceiveIDType = r.URL.Query().Get("receive_id_type")
			if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	n := NewFeishu(config.FeishuNotificationConfig{
		DefaultChannel: "c1",
		APIBaseURL:     srv.URL,
		AppID:          "app-id",
		AppSecret:      "app-secret",
		Channels: []config.FeishuChannel{{
			ID: "c1", Mode: "app", ReceiveIDType: "chat_id", ReceiveID: "oc_x", MaxMessageChars: 200,
		}},
	})
	res := n.Send(context.Background(), "", Message{Text: "hello"})
	if res.Status != "notified" || res.ChannelID != "c1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if gotAuth != "Bearer tenant-token" || gotReceiveIDType != "chat_id" {
		t.Fatalf("unexpected auth/query: auth=%q type=%q", gotAuth, gotReceiveIDType)
	}
	if gotPayload.ReceiveID != "oc_x" || gotPayload.MsgType != "text" || !strings.Contains(gotPayload.Content, "hello") {
		t.Fatalf("unexpected payload: %+v", gotPayload)
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
