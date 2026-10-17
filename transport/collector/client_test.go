package collector

import (
	"context"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/registry"
)

type postReadFailure struct{}

func (postReadFailure) Error() string                    { return "media processing failed" }
func (postReadFailure) ToolErrorCode() string            { return "BROWSER_TIMEOUT" }
func (postReadFailure) ToolErrorDetails() map[string]any { return map[string]any{"retryable": true} }

func TestCollectorDoesNotReplayHandlerAfterBrowserPostprocessingFailure(t *testing.T) {
	for _, name := range []string{"browser.browse", "wechat.article.read", "zhihu.content.read", "reddit.post.read"} {
		t.Run(name, func(t *testing.T) {
			reg := registry.New()
			calls, replies := 0, 0
			reg.Register(registry.Entry{Descriptor: protocol.ToolDescriptor{Name: name}, Handler: func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
				calls++
				return nil, nil, postReadFailure{}
			}})
			client := NewClient(Options{RetryMaxAttempts: 3}, runtime.New(reg, runtime.Options{}))
			client.executeAndReply(context.Background(), func(protocol.Envelope) error { replies++; return nil }, protocol.ToolCall{ID: "test", Tool: name, Input: []byte(`{}`)})
			if calls != 1 || replies != 1 {
				t.Fatalf("calls=%d replies=%d", calls, replies)
			}
		})
	}
}

func TestReserveQueueSlot(t *testing.T) {
	t.Parallel()
	c := &Client{opts: Options{CallQueueSize: 2}}
	if !c.reserveQueueSlot() {
		t.Fatal("expected first slot")
	}
	if !c.reserveQueueSlot() {
		t.Fatal("expected second slot")
	}
	if c.reserveQueueSlot() {
		t.Fatal("expected queue full")
	}
	c.releaseQueueSlot()
	if !c.reserveQueueSlot() {
		t.Fatal("expected slot after release")
	}
}

func TestOptionsFromConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := config.Get()
	opts := OptionsFromConfig(cfg)
	if opts.CallQueueSize != 32 {
		t.Fatalf("queue size: %d", opts.CallQueueSize)
	}
	if opts.RateLimitCooldown != time.Second {
		t.Fatalf("cooldown: %s", opts.RateLimitCooldown)
	}
	if opts.CallQueueWait != 30*time.Second {
		t.Fatalf("wait: %s", opts.CallQueueWait)
	}
}
