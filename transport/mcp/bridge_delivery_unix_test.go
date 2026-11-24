//go:build !windows

package mcpgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
	"github.com/originaleric/digcrawl/transport/bridge"
	httpgw "github.com/originaleric/digcrawl/transport/http"
)

// Block in the existing FileStore.Open boundary, after execution has finished
// and headers have been flushed, to exercise the actual delivery drain barrier.
type delayedDeliveryStore struct {
	artifact.FileStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *delayedDeliveryStore) Open(ctx context.Context, id string) (io.ReadCloser, string, error) {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	return s.FileStore.Open(ctx, id)
}

func TestBridgeDrainsCompleteMCPArtifactDelivery(t *testing.T) {
	for _, phase := range []string{"execution", "delivery", "large_delivery", "partial_error", "delivery_error"} {
		t.Run(phase, func(t *testing.T) {
			home, err := os.MkdirTemp("/tmp", "dcd-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(home)
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			profile := filepath.Join(home, "profile.yaml")
			if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			socket, err := bridge.SocketPath(profile)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
				t.Fatal(err)
			}
			disk, err := artifact.NewDiskStore(filepath.Join(home, "artifacts"), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			payload := []byte("image bytes")
			if phase == "large_delivery" {
				payload = bytes.Repeat([]byte("x"), 7<<20)
			}
			ref, err := disk.Put(context.Background(), "image", "image/png", "fixture", payload)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "delivery_error" {
				ref.ID = "missing"
			}
			empty, err := disk.Put(context.Background(), "empty", "application/octet-stream", "empty", nil)
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			finish := func() { once.Do(func() { close(release) }) }
			defer finish()
			var store artifact.Store = disk
			if phase != "execution" && phase != "partial_error" {
				store = &delayedDeliveryStore{FileStore: disk, entered: entered, release: release}
			}
			reg := registry.New()
			reg.Register(registry.Entry{Descriptor: protocol.ToolDescriptor{Name: "fixture.read"}, Handler: func(ctx context.Context, _ *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
				if phase == "execution" || phase == "partial_error" {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return nil, nil, ctx.Err()
					}
				}
				var err error
				if phase == "partial_error" {
					err = fmt.Errorf("original fixture failure")
				}
				return map[string]any{"completed": true}, []protocol.Artifact{ref, empty}, err
			}})
			rt := runtime.New(reg, runtime.Options{ArtifactStore: store})
			gateway := httpgw.NewServer(rt, store, "")
			gateway.InlineArtifacts = true
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/artifacts/") {
					t.Error("accepted result issued a separate attachment request")
				}
				gateway.ServeHTTP(w, r)
			})}
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			go server.Serve(listener)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			executor, err := bridge.NewExecutor(ctx, "unused", profile, &config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan *protocol.ToolResult, 1)
			go func() {
				results <- executor.Execute(ctx, &protocol.ToolCall{ID: "accepted", Tool: "fixture.read", Input: json.RawMessage(`{}`)})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			control, err := bridge.AcquireControl()
			if err != nil {
				t.Fatal(err)
			}
			defer control.Close()
			clear, err := bridge.HoldStop(profile)
			if err != nil {
				t.Fatal(err)
			}
			defer clear()
			drained := make(chan error, 1)
			go func() { drained <- server.Shutdown(ctx) }()
			select {
			case err := <-drained:
				t.Fatalf("shutdown passed unfinished delivery: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			finish()
			var result *protocol.ToolResult
			select {
			case result = <-results:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := <-drained; err != nil {
				t.Fatal(err)
			}
			// Convert only after the socket is closed, with maintenance still active.
			converted, err := toolResultToMCPWithInlineLimit(ctx, result, executor.ArtifactStore(), artifact.DefaultInlineBytes)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "partial_error" {
				if !converted.IsError || !strings.Contains(fmt.Sprint(converted.StructuredContent), "original fixture failure") {
					t.Fatalf("original error lost: %+v", converted)
				}
			} else if phase == "delivery_error" {
				if !converted.IsError || !strings.Contains(fmt.Sprint(converted.StructuredContent), "ARTIFACT_UNAVAILABLE") {
					t.Fatalf("attachment error was not preserved: %+v", converted)
				}
				return
			} else if converted.IsError {
				t.Fatalf("accepted call became MCP error: %+v", converted)
			}
			if len(converted.Content) != 3 {
				t.Fatalf("missing completed attachments: %+v", converted)
			}
			img, ok := converted.Content[1].(mcp.ImageContent)
			if !ok || len(img.Data) == 0 {
				t.Fatalf("missing image: %+v", converted.Content[1])
			}
			metadata, err := json.Marshal(converted.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(metadata, []byte("inline_data")) || bytes.Contains(metadata, []byte("inline_error")) {
				t.Fatal("transport payload leaked into MCP metadata")
			}
			if len(result.Artifacts) != 2 || result.Artifacts[1].InlineData == nil || len(*result.Artifacts[1].InlineData) != 0 {
				t.Fatalf("empty attachment lost: %+v", result.Artifacts)
			}
		})
	}
}
