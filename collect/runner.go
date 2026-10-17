package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/protocol"
	browserprov "github.com/originaleric/digcrawl/provider/browser"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
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

	var out map[string]any
	if len(result.Output) > 0 {
		if err := json.Unmarshal(result.Output, &out); err != nil {
			return nil, result, fmt.Errorf("decode tool output: %w", err)
		}
	}
	if out == nil {
		out = map[string]any{}
	}

	if result.Status != "success" && !isRecoverableProviderRetentionFailure(result, out) {
		if code, message := structuredPlatformError(out); code != "" {
			return out, result, platform.NewReadError(code, message, match.Platform, nil)
		}
		msg := "tool execution failed"
		if result.Error != nil {
			msg = result.Error.Message
		}
		return nil, result, fmt.Errorf("%s", msg)
	}
	if code, message := structuredPlatformError(out); code != "" {
		if message == "" {
			message = code
		}
		return out, result, platform.NewReadError(code, message, match.Platform, nil)
	}
	out["_collect_platform"] = match.Platform
	if _, ok := out["source_url"]; !ok {
		out["source_url"] = opts.URL
	}
	return out, result, nil
}

// isRecoverableProviderRetentionFailure recognizes only the explicit
// provider-retention boundary. It deliberately does not turn generic handler
// errors, platform errors, or malformed partial output into successful Collect
// executions.
func isRecoverableProviderRetentionFailure(result *protocol.ToolResult, out map[string]any) bool {
	if result == nil || result.Error == nil {
		return false
	}
	switch result.Error.Code {
	case platformprov.ProviderVideoDownloadFailedCode:
		return providerVideoDownloadStatus(out) == "failed"
	case platformprov.ProviderImageDownloadFailedCode:
		return providerImageDownloadStatus(out) == "failed"
	default:
		return false
	}
}

func providerVideoDownloadStatus(out map[string]any) string {
	meta, _ := out["platform_metadata"].(map[string]any)
	status, _ := meta["video_download_status"].(string)
	return strings.ToLower(strings.TrimSpace(status))
}

func providerImageDownloadStatus(out map[string]any) string {
	meta, _ := out["platform_metadata"].(map[string]any)
	status, _ := meta["image_download_status"].(string)
	return strings.ToLower(strings.TrimSpace(status))
}

func buildToolInput(match Match, opts RunOptions) (map[string]any, error) {
	if match.Tool == browserprov.ToolBrowserBrowse {
		return map[string]any{
			"url":               opts.URL,
			"action":            "read",
			"mode":              "full",
			"include_media":     opts.IncludeMedia,
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
