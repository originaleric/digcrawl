//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	"github.com/originaleric/digcrawl/transport/bridge"
)

func TestMaintenanceDrainsAcceptedTaskWithoutCancelingIt(t *testing.T) {
	for _, cancelWait := range []bool{false, true} {
		name := "complete"
		if cancelWait {
			name = "cancel_wait"
		}
		t.Run(name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var released sync.Once
			finishTask := func() { released.Do(func() { close(release) }) }
			feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					t.Error("maintenance canceled the accepted task")
					return
				}
				w.Header().Set("Content-Type", "application/rss+xml")
				_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>fixture</title><link>https://example.com</link><description>fixture</description><item><title>completed task</title><link>https://example.com/entry</link><description>body</description></item></channel></rss>`))
			}))
			defer feed.Close()
			defer finishTask()
			profile := writeMCPPhaseOneProfile(t)
			cfg, err := config.Load(profile)
			if err != nil {
				t.Fatal(err)
			}
			executable := writeMCPBridgeExecutable(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			executor, err := bridge.NewExecutor(ctx, executable, profile, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				finishTask()
				cleanup, finish := context.WithTimeout(context.Background(), 5*time.Second)
				defer finish()
				_, _ = bridge.Stop(cleanup, profile)
			}()
			input, _ := json.Marshal(map[string]any{"url": feed.URL})
			result := make(chan *protocol.ToolResult, 1)
			go func() {
				result <- executor.Execute(ctx, &protocol.ToolCall{ID: "drain-task", Tool: "feed.read", Input: input})
			}()
			select {
			case <-entered:
			case value := <-result:
				t.Fatalf("task did not enter fixture: %+v", value)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var output bytes.Buffer
			stop := exec.CommandContext(ctx, executable, "bridge", "stop", "--config", profile)
			stop.Stdout, stop.Stderr = &output, &output
			if err := stop.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- stop.Wait() }()
			for {
				state, err := bridge.Inspect(ctx, profile)
				if err == nil && state.Paused {
					break
				}
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				time.Sleep(10 * time.Millisecond)
			}
			select {
			case err := <-done:
				t.Fatalf("stop returned before task completion: %v: %s", err, output.String())
			case <-time.After(150 * time.Millisecond):
			}
			competing := exec.CommandContext(ctx, executable, "bridge", "restart", "--config", profile)
			if out, err := competing.CombinedOutput(); err == nil || !strings.Contains(string(out), "maintenance") {
				t.Fatalf("concurrent control was accepted: %v: %s", err, out)
			}
			blocked := executor.Execute(ctx, &protocol.ToolCall{ID: "new-task", Tool: "gateway.ping", Input: []byte(`{}`)})
			if blocked.Status != "error" {
				t.Fatal("new task admitted during maintenance")
			}
			if cancelWait {
				if err := stop.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err == nil {
					t.Fatal("canceled wait reported completion")
				}
			}
			finishTask()
			select {
			case value := <-result:
				if value.Status != "success" {
					data, _ := json.Marshal(value)
					t.Fatalf("accepted task failed: %s", data)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !cancelWait {
				if err := <-done; err != nil {
					t.Fatalf("stop: %v: %s", err, output.String())
				}
			}
		})
	}
}
