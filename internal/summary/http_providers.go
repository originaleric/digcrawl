package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type DigFlowProvider struct {
	BaseURL      string
	Endpoint     string
	Agent        string
	AuthToken    string
	ProviderName string
	Timeout      time.Duration
	Budget       InputBudget
	Fallback     Provider
}

func (p DigFlowProvider) Summarize(ctx context.Context, req Request) (*Result, error) {
	req = ApplyBudget(req, p.Budget)
	endpoint := strings.TrimSpace(p.Endpoint)
	if endpoint == "" {
		endpoint = "/api/v1/invoke"
	}
	base := strings.TrimRight(os.ExpandEnv(strings.TrimSpace(p.BaseURL)), "/")
	if base == "" {
		return p.fallback(ctx, req, fmt.Errorf("digflow base url is required"))
	}
	agent := os.ExpandEnv(strings.TrimSpace(p.Agent))
	if agent == "" {
		return p.fallback(ctx, req, fmt.Errorf("digflow agent is required"))
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	body := map[string]any{
		"agent": agent,
		"messages": []map[string]string{{
			"role":    "user",
			"content": string(reqData),
		}},
		"metadata": map[string]string{
			"source":   "digcrawl_watchlist",
			"watch_id": req.WatchID,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(cctx, http.MethodPost, base+endpoint, bytes.NewReader(payload))
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token := os.ExpandEnv(strings.TrimSpace(p.AuthToken)); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return p.fallback(ctx, req, safeProviderError("digflow", resp.StatusCode, data))
	}
	res, err := parseSummaryResult(data)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	res.Provider = p.providerName()
	return res, nil
}

func (p DigFlowProvider) fallback(ctx context.Context, req Request, cause error) (*Result, error) {
	if p.Fallback == nil {
		return nil, cause
	}
	res, err := p.Fallback.Summarize(ctx, req)
	if res != nil {
		res.Error = safeFallbackError(cause)
	}
	return res, err
}

func (p DigFlowProvider) providerName() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return strings.TrimSpace(p.ProviderName)
	}
	return "digflow_http"
}

type LLMProvider struct {
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
	Budget   InputBudget
	Fallback Provider
}

func (p LLMProvider) Summarize(ctx context.Context, req Request) (*Result, error) {
	req = ApplyBudget(req, p.Budget)
	base := strings.TrimRight(os.ExpandEnv(strings.TrimSpace(p.BaseURL)), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	apiKey := os.ExpandEnv(strings.TrimSpace(p.APIKey))
	if apiKey == "" {
		return p.fallback(ctx, req, fmt.Errorf("llm api key is required"))
	}
	model := os.ExpandEnv(strings.TrimSpace(p.Model))
	if model == "" {
		model = "gpt-4o-mini"
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是内容监控摘要 Agent。基于用户提供的 JSON 事实输入输出严格 JSON，不要编造。"},
			{"role": "user", "content": string(reqData)},
		},
		"temperature": 0.2,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return p.fallback(ctx, req, safeProviderError("llm", resp.StatusCode, data))
	}
	res, err := parseOpenAIChatSummary(data)
	if err != nil {
		return p.fallback(ctx, req, err)
	}
	res.Provider = "llm_direct"
	return res, nil
}

func (p LLMProvider) fallback(ctx context.Context, req Request, cause error) (*Result, error) {
	if p.Fallback == nil {
		return nil, cause
	}
	res, err := p.Fallback.Summarize(ctx, req)
	if res != nil {
		res.Error = safeFallbackError(cause)
	}
	return res, err
}

func parseOpenAIChatSummary(data []byte) (*Result, error) {
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("llm response has no content")
	}
	return parseSummaryResult([]byte(envelope.Choices[0].Message.Content))
}

func parseSummaryResult(data []byte) (*Result, error) {
	var res Result
	if err := json.Unmarshal(data, &res); err == nil && validateResult(res) == nil {
		return &res, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("summary response is not json: %w", err)
	}
	for _, key := range []string{"content", "message", "output", "result"} {
		if v, ok := envelope[key].(string); ok && strings.TrimSpace(v) != "" {
			var nested Result
			if err := json.Unmarshal([]byte(v), &nested); err != nil {
				continue
			}
			if err := validateResult(nested); err != nil {
				continue
			}
			return &nested, nil
		}
	}
	if msg, ok := envelope["message"].(map[string]any); ok {
		if content, ok := msg["content"].(string); ok && strings.TrimSpace(content) != "" {
			var nested Result
			if err := json.Unmarshal([]byte(content), &nested); err == nil && validateResult(nested) == nil {
				return &nested, nil
			}
		}
	}
	return nil, fmt.Errorf("summary response does not match SummaryResult schema")
}

func validateResult(res Result) error {
	if strings.TrimSpace(res.DigestTitle) == "" && strings.TrimSpace(res.OverallSummary) == "" && len(res.Items) == 0 {
		return fmt.Errorf("empty summary result")
	}
	return nil
}

func safeProviderError(provider string, status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("%s http %d", provider, status)
	}
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	if len([]rune(msg)) > 160 {
		msg = string([]rune(msg)[:160]) + "...(truncated)"
	}
	for _, marker := range []string{"api_key", "apikey", "authorization", "bearer", "token", "secret"} {
		if strings.Contains(strings.ToLower(msg), marker) {
			return fmt.Errorf("%s http %d: response body redacted", provider, status)
		}
	}
	return fmt.Errorf("%s http %d: %s", provider, status, msg)
}

func safeFallbackError(cause error) string {
	if cause == nil {
		return ""
	}
	msg := strings.ToLower(cause.Error())
	switch {
	case strings.Contains(msg, "schema"):
		return "summary response schema invalid"
	case strings.Contains(msg, "json"):
		return "summary response json invalid"
	case strings.Contains(msg, "api key") || strings.Contains(msg, "apikey") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden"):
		return "summary provider credential invalid"
	case strings.Contains(msg, "manifest") || strings.Contains(msg, "config") || strings.Contains(msg, "agent"):
		return "summary provider configuration invalid"
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "dial tcp"):
		return "summary provider unavailable"
	case strings.Contains(msg, "empty summary"):
		return "summary provider returned empty result"
	default:
		return "summary provider failed"
	}
}
