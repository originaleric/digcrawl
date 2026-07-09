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
	channels  map[string]config.FeishuChannel
	defaultID string
}

func NewFeishu(cfg config.FeishuNotificationConfig) *FeishuNotifier {
	n := &FeishuNotifier{channels: map[string]config.FeishuChannel{}, defaultID: strings.TrimSpace(cfg.DefaultChannel)}
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
	resp, err := http.DefaultClient.Do(req)
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
