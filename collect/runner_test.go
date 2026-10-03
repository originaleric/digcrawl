package collect

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/protocol"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/registry"
)

func TestRunTreatsStructuredPlatformErrorAsFailure(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "wechat.article.read"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{
				"platform": "wechat",
				"platform_metadata": map[string]any{
					"error_code":    "SELECTOR_STALE",
					"error_message": "未提取到真实文章标题",
				},
			}, nil, nil
		},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})

	out, result, err := Run(context.Background(), rt, RunOptions{
		URL:   "https://mp.weixin.qq.com/s/test",
		Match: Match{Platform: "wechat", Tool: "wechat.article.read"},
	})
	if err == nil {
		t.Fatal("expected structured platform error to fail collect.Run")
	}
	var readErr *platform.ReadError
	if !errors.As(err, &readErr) || readErr.Code != platform.ErrSelectorStale {
		t.Fatalf("expected structured ReadError, got %T %v", err, err)
	}
	if result == nil || result.Status != "success" {
		t.Fatalf("expected tool call itself to succeed with structured output, got %+v", result)
	}
	if code, msg := structuredPlatformError(out); code != "SELECTOR_STALE" || msg != "未提取到真实文章标题" {
		t.Fatalf("unexpected structured error parse: code=%q msg=%q out=%#v", code, msg, out)
	}
}

func TestStructuredPlatformErrorIgnoresNormalOutput(t *testing.T) {
	t.Parallel()
	code, msg := structuredPlatformError(map[string]any{
		"platform": "wechat",
		"title":    "真实文章标题",
		"text":     "正文",
	})
	if code != "" || msg != "" {
		t.Fatalf("expected no structured error, got code=%q msg=%q", code, msg)
	}
}

func TestRunSetsToolCallTimeoutPolicy(t *testing.T) {
	t.Parallel()

	var gotTimeout int
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "toutiao.article.read"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			gotTimeout = call.Policy.TimeoutMs
			return map[string]any{
				"platform":     "toutiao",
				"content_type": "article",
				"title":        "标题",
				"text":         "正文",
			}, nil, nil
		},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})

	_, _, err := Run(context.Background(), rt, RunOptions{
		URL:       "https://www.toutiao.com/group/123/",
		Match:     Match{Platform: "toutiao", Tool: "toutiao.article.read"},
		TimeoutMs: 120000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotTimeout != 120000 {
		t.Fatalf("expected timeout policy 120000, got %d", gotTimeout)
	}
}

func TestRunPassesCookieDomainToToolInput(t *testing.T) {
	t.Parallel()

	var gotCookieDomain string
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: "threads.post.read"},
		Handler: func(ctx context.Context, call *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			var input map[string]any
			if err := json.Unmarshal(call.Input, &input); err != nil {
				t.Fatal(err)
			}
			gotCookieDomain, _ = input["use_cookie_domain"].(string)
			return map[string]any{
				"platform":     "threads",
				"content_type": "thread",
				"title":        "thread",
				"text":         "body",
			}, nil, nil
		},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})

	_, _, err := Run(context.Background(), rt, RunOptions{
		URL:             "https://www.threads.net/@zuck/post/ABC_123",
		Match:           Match{Platform: "threads", Tool: "threads.post.read"},
		UseCookieDomain: "www.threads.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotCookieDomain != "www.threads.net" {
		t.Fatalf("use_cookie_domain=%q", gotCookieDomain)
	}
}

func TestRunPreservesSuccessfulContentAfterProviderVideoRetentionFailure(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: platformprov.ToolToutiaoVideoRead},
		Handler: func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{
				"platform":     "toutiao",
				"content_type": "video",
				"title":        "已读取的视频",
				"text":         "正文仍可归档",
				"platform_metadata": map[string]any{
					"video_download_status":         "failed",
					"video_download_failure_reason": "download_failed",
				},
			}, nil, providerVideoRetentionTestError{}
		},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})

	out, result, err := Run(context.Background(), rt, RunOptions{
		URL:   "https://www.toutiao.com/video/7667505035089101366/",
		Match: Match{Platform: "toutiao", Tool: platformprov.ToolToutiaoVideoRead},
	})
	if err != nil {
		t.Fatalf("provider retention failure must retain successful content for Collect: %v", err)
	}
	if result == nil || result.Status != "error" || result.Error == nil || result.Error.Code != platformprov.ProviderVideoDownloadFailedCode {
		t.Fatalf("expected typed partial failure result, got %+v", result)
	}
	if out["text"] != "正文仍可归档" || out["_collect_platform"] != "toutiao" {
		t.Fatalf("successful content was not preserved: %#v", out)
	}
}

func TestRunDoesNotTreatOtherPartialErrorsAsRecoverable(t *testing.T) {
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: platformprov.ToolToutiaoVideoRead},
		Handler: func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{
				"platform_metadata": map[string]any{"video_download_status": "saved"},
			}, nil, providerVideoRetentionTestError{}
		},
	})
	rt := runtime.New(reg, runtime.Options{InstanceID: "test"})

	_, _, err := Run(context.Background(), rt, RunOptions{
		URL:   "https://www.toutiao.com/video/7667505035089101366/",
		Match: Match{Platform: "toutiao", Tool: platformprov.ToolToutiaoVideoRead},
	})
	if err == nil {
		t.Fatal("only an explicit failed retention status may be recovered")
	}
}

type providerVideoRetentionTestError struct{}

func (providerVideoRetentionTestError) Error() string {
	return "provider video download failed: download_failed"
}

func (providerVideoRetentionTestError) ToolErrorCode() string {
	return platformprov.ProviderVideoDownloadFailedCode
}

func (providerVideoRetentionTestError) ToolErrorMessage() string {
	return "provider video download failed: download_failed"
}
