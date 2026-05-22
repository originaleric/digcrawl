package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/originaleric/digcrawl/protocol"
)

// Client 调用远端 DigCrawl HTTP 工具网关（供宿主项目使用）。
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// New 创建 HTTP 网关客户端。
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Manifest 请求 GET /manifest 获取工具清单。
func (c *Client) Manifest(ctx context.Context) (protocol.ToolManifest, error) {
	var m protocol.ToolManifest
	if err := c.getJSON(ctx, "/manifest", &m); err != nil {
		return m, err
	}
	return m, nil
}

// Call 执行 POST /tools/call。
func (c *Client) Call(ctx context.Context, call protocol.ToolCall) (protocol.ToolResult, error) {
	if call.Type == "" {
		call.Type = protocol.TypeToolCall
	}
	body, err := json.Marshal(call)
	if err != nil {
		return protocol.ToolResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/tools/call", bytes.NewReader(body))
	if err != nil {
		return protocol.ToolResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return protocol.ToolResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return protocol.ToolResult{}, err
	}
	if resp.StatusCode >= 400 {
		return protocol.ToolResult{}, fmt.Errorf("gateway http %d: %s", resp.StatusCode, string(data))
	}
	var result protocol.ToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		return protocol.ToolResult{}, err
	}
	return result, nil
}

// FetchArtifact 请求 GET /artifacts/{id} 下载 Artifact 内容。
func (c *Client) FetchArtifact(ctx context.Context, id string) ([]byte, string, error) {
	id = strings.TrimPrefix(id, "digcrawl-artifact://")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/artifacts/"+id, nil)
	if err != nil {
		return nil, "", err
	}
	c.applyAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("artifact http %d", resp.StatusCode)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	c.applyAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gateway http %d: %s", resp.StatusCode, string(data))
	}
	return json.Unmarshal(data, out)
}

func (c *Client) applyAuth(req *http.Request) {
	if c.Token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
}
