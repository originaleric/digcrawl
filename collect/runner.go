package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/originaleric/digcrawl/protocol"
	browserprov "github.com/originaleric/digcrawl/provider/browser"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/executor"
)

// RunOptions configures a single collect execution.
type RunOptions struct {
	URL             string
	Formats         []string
	IncludeMedia    bool
	Match           Match
	TimeoutMs       int
	UseCookieDomain string
}

// Run executes a tool via gateway runtime and returns parsed output.
func Run(ctx context.Context, rt *runtime.Runtime, opts RunOptions) (map[string]any, *protocol.ToolResult, error) {
	match := opts.Match
	if match.Tool == "" {
		var err error
		match, err = Detect(opts.URL)
		if err != nil {
			return nil, nil, err
		}
	}

	input, err := buildToolInput(match, opts)
	if err != nil {
		return nil, nil, err
	}
	inBytes, err := json.Marshal(input)
	if err != nil {
		return nil, nil, err
	}

	call := &protocol.ToolCall{
		Type:  protocol.TypeToolCall,
		ID:    "collect_" + uuid.NewString(),
		Tool:  match.Tool,
		Input: inBytes,
	}
	if opts.TimeoutMs > 0 {
		call.Policy.TimeoutMs = opts.TimeoutMs
	}
	result := rt.Execute(executor.WithCollectExecution(ctx), call)
	if result.Status != "success" {
		msg := "tool execution failed"
		if result.Error != nil {
			msg = result.Error.Message
		}
		return nil, result, fmt.Errorf("%s", msg)
	}

	var out map[string]any
	if len(result.Output) > 0 {
		if err := json.Unmarshal(result.Output, &out); err != nil {
			return nil, result, fmt.Errorf("decode tool output: %w", err)
		}
	}
	if out == nil {
		out = map[string]any{}
	}
	if code, message := structuredPlatformError(out); code != "" {
		if message == "" {
			message = code
		}
		return out, result, fmt.Errorf("%s", message)
	}
	out["_collect_platform"] = match.Platform
	if _, ok := out["source_url"]; !ok {
		out["source_url"] = opts.URL
	}
	return out, result, nil
}

func buildToolInput(match Match, opts RunOptions) (map[string]any, error) {
	if match.Tool == browserprov.ToolBrowserBrowse {
		return map[string]any{
			"url":               opts.URL,
			"action":            "read",
			"mode":              "full",
			"use_cookie_domain": opts.UseCookieDomain,
		}, nil
	}
	return map[string]any{
		"url":               opts.URL,
		"format":            opts.Formats,
		"include_media":     opts.IncludeMedia,
		"use_cookie_domain": opts.UseCookieDomain,
	}, nil
}

func structuredPlatformError(out map[string]any) (string, string) {
	if out == nil {
		return "", ""
	}
	meta, _ := out["platform_metadata"].(map[string]any)
	if len(meta) == 0 {
		return "", ""
	}
	code, _ := meta["error_code"].(string)
	code = strings.TrimSpace(code)
	if code == "" {
		return "", ""
	}
	message, _ := meta["error_message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		message = code
	}
	return code, message
}
