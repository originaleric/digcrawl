package collect

import (
	"context"
	"testing"

	"github.com/originaleric/digcrawl/protocol"
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
