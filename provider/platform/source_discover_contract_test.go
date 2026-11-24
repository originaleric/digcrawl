package platformprov_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/originaleric/digcrawl/protocol"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	crawlRuntime "github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
	"testing"
	"time"
)

func TestSourceDiscoverExpiredCodeSurvivesRuntimeBoundary(t *testing.T) {
	reg := registry.New()
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(platformprov.SourceDiscoverEntry(nil, store))
	cp := base64.RawURLEncoding.EncodeToString([]byte(`{"id":"gone","offset":0}`))
	raw, _ := json.Marshal(map[string]any{"url": "https://example.test/feed", "source_kind": "rss", "checkpoint": cp})
	result := crawlRuntime.New(reg, crawlRuntime.Options{}).Execute(context.Background(), &protocol.ToolCall{Type: protocol.TypeToolCall, ID: "expired", Tool: platformprov.ToolSourceDiscover, Input: raw})
	if result.Error == nil || result.Error.Code != "CHECKPOINT_EXPIRED" {
		t.Fatalf("host cannot recover checkpoint: %+v", result)
	}
}
