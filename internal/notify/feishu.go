package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/config"
)

type FeishuNotifier struct {
	cfg        config.FeishuNotificationConfig
	channels   map[string]config.FeishuChannel
	defaultID  string
	httpClient *http.Client
}

func NewFeishu(cfg config.FeishuNotificationConfig) *FeishuNotifier {
	n := &FeishuNotifier{cfg: cfg, channels: map[string]config.FeishuChannel{}, defaultID: strings.TrimSpace(cfg.DefaultChannel), httpClient: http.DefaultClient}
	for _, ch := range cfg.Channels {
		id := strings.TrimSpace(ch.ID)
		if id == "" {
			continue
		}
		n.channels[id] = ch
		if n.defaultID == "" {
			n.defaultID = id
		}
	}
	return n
}

func (n *FeishuNotifier) Send(ctx context.Context, channelID string, msg Message) Result {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		channelID = n.defaultID
	}
	ch, ok := n.channels[channelID]
	if !ok {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: "feishu channel not found", Retryable: false}
	}
	if feishuChannelMode(ch) == "app" {
		return n.sendAppMessage(ctx, channelID, ch, msg)
	}
	webhook := os.ExpandEnv(strings.TrimSpace(ch.WebhookURL))
	if webhook == "" {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: "feishu webhook url is required", Retryable: false}
	}
	maxChars := ch.MaxMessageChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = msg.Title
	}
	text = trimRunes(text, maxChars)
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	}
	if secret := os.ExpandEnv(strings.TrimSpace(ch.Secret)); secret != "" {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		payload["timestamp"] = ts
		payload["sign"] = feishuSign(ts, secret)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: false}
	}
	timeout := time.Duration(ch.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, webhook, bytes.NewReader(data))
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: false}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: fmt.Sprintf("feishu http %d: %s", resp.StatusCode, string(body)), Retryable: resp.StatusCode >= 500 || resp.StatusCode == 429}
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if len(body) > 0 && json.Unmarshal(body, &out) == nil && out.Code != 0 {
		retry := out.Code == 9499 || out.Code == 19002
		return Result{ChannelID: channelID, Status: "notify_failed", Error: fmt.Sprintf("feishu code %d: %s", out.Code, out.Msg), Retryable: retry}
	}
	return Result{ChannelID: channelID, Status: "notified"}
}

func (n *FeishuNotifier) sendAppMessage(ctx context.Context, channelID string, ch config.FeishuChannel, msg Message) Result {
	appID := os.ExpandEnv(strings.TrimSpace(n.cfg.AppID))
	appSecret := os.ExpandEnv(strings.TrimSpace(n.cfg.AppSecret))
	if appID == "" || appSecret == "" {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: "feishu app_id and app_secret are required", Retryable: false}
	}
	receiveIDType := strings.TrimSpace(ch.ReceiveIDType)
	if receiveIDType == "" {
		receiveIDType = "chat_id"
	}
	receiveID := os.ExpandEnv(strings.TrimSpace(ch.ReceiveID))
	if receiveID == "" {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: "feishu receive_id is required", Retryable: false}
	}
	maxChars := ch.MaxMessageChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = msg.Title
	}
	text = trimRunes(text, maxChars)
	timeout := time.Duration(ch.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	token, err := n.tenantAccessToken(cctx, appID, appSecret)
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: true}
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	payload := map[string]any{
		"receive_id": receiveID,
		"msg_type":   "text",
		"content":    string(content),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: false}
	}
	url := fmt.Sprintf("%s/open-apis/im/v1/messages?receive_id_type=%s", n.cfg.APIBaseURLOrDefault(), receiveIDType)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: false}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{ChannelID: channelID, Status: "notify_failed", Error: fmt.Sprintf("feishu app message http %d: %s", resp.StatusCode, string(body)), Retryable: resp.StatusCode >= 500 || resp.StatusCode == 429}
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if len(body) > 0 && json.Unmarshal(body, &out) == nil && out.Code != 0 {
		retry := out.Code == 9499 || out.Code == 19002
		return Result{ChannelID: channelID, Status: "notify_failed", Error: fmt.Sprintf("feishu app message code %d: %s", out.Code, out.Msg), Retryable: retry}
	}
	return Result{ChannelID: channelID, Status: "notified"}
}

func (n *FeishuNotifier) tenantAccessToken(ctx context.Context, appID, appSecret string) (string, error) {
	payload := map[string]string{"app_id": appID, "app_secret": appSecret}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	url := n.cfg.APIBaseURLOrDefault() + "/open-apis/auth/v3/tenant_access_token/internal"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("feishu tenant token http %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", fmt.Errorf("feishu tenant token code %d: %s", out.Code, out.Msg)
	}
	if strings.TrimSpace(out.TenantAccessToken) == "" {
		return "", fmt.Errorf("feishu tenant token response missing token")
	}
	return out.TenantAccessToken, nil
}

func feishuChannelMode(ch config.FeishuChannel) string {
	mode := strings.ToLower(strings.TrimSpace(ch.Mode))
	if mode == "app" || mode == "bot" || mode == "openapi" {
		return "app"
	}
	return "webhook"
}

func feishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func trimRunes(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	if max < 20 {
		return string(r[:max])
	}
	return string(r[:max-12]) + "\n...(truncated)"
}

type NoopNotifier struct{}

func (NoopNotifier) Send(ctx context.Context, channelID string, msg Message) Result {
	return Result{ChannelID: strings.TrimSpace(channelID), Status: "notified"}
}
