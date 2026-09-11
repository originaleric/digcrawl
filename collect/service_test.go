package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/collect/store"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/protocol"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/registry"
)

func TestPersistModeBranches(t *testing.T) {
	dir := t.TempDir()
	when, _ := time.Parse("2006-01-02", "2026-05-21")

	enabled := true
	cfg := config.Get()
	cfg.Collect.OutputDir = dir

	out := map[string]any{
		"title":        "Article",
		"platform":     "wechat",
		"content_type": "article",
		"captured_at":  "2026-05-21T00:00:00Z",
	}
	paths, err := BuildOutputPaths(dir, "Article", when)
	if err != nil {
		t.Fatal(err)
	}
	downloaded := []map[string]any{
		{"type": "image", "url": "https://mmbiz.qpic.cn/a.jpg", "local": "001.jpg"},
		{"type": "image", "url": "https://mmbiz.qpic.cn/b.jpg", "error": "http 403"},
	}
	match := Match{Platform: "wechat", Tool: "wechat.article.read"}
	url := "https://mp.weixin.qq.com/s/test"

	modes := []struct {
		mode      string
		wantIndex bool
		wantDB    bool
	}{
		{"files", false, false},
		{"index", true, false},
		{"sqlite", false, true},
		{"both", true, true},
	}
	for _, tc := range modes {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			root := filepath.Join(dir, tc.mode)
			dbPath := filepath.Join(t.TempDir(), "collect.db")
			cfg.Collect.PersistMode = tc.mode
			cfg.Collect.DBPath = dbPath
			cfg.Collect.IndexEnabled = &enabled

			s := &Service{cfg: cfg}
			if tc.wantDB {
				db, err := store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				s.collectDB = db
				t.Cleanup(func() { _ = db.Close() })
			}
			result := ExportResult{Paths: paths, Downloaded: downloaded}
			if err := s.persistCollect(context.Background(), url, match, out, &result, root); err != nil {
				t.Fatal(err)
			}
			_, indexOK, _ := FindByURL(root, url)
			if indexOK != tc.wantIndex {
				t.Fatalf("index present=%v want %v", indexOK, tc.wantIndex)
			}
			if tc.wantDB {
				db, err := store.OpenSQLite(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				detail, err := db.GetByURL(context.Background(), url)
				if err != nil {
					t.Fatal(err)
				}
				if detail == nil {
					t.Fatal("expected sqlite row")
				}
				if len(detail.Media) != 2 {
					t.Fatalf("expected 2 media rows, got %d", len(detail.Media))
				}
			}
		})
	}
}

func TestServiceExportReservesDistinctPathsForConcurrentSameTitle(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{cfg: &config.Config{}}
	outputs := []map[string]any{
		{"title": "same title", "text": "first"},
		{"title": "same title", "text": "second"},
	}
	results := make(chan ExportResult, len(outputs))
	errs := make(chan error, len(outputs))
	var workers sync.WaitGroup
	for _, out := range outputs {
		workers.Add(1)
		go func(out map[string]any) {
			defer workers.Done()
			result, err := svc.export(context.Background(), out, dir, []string{"md", "json"})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(out)
	}
	workers.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatal(err)
	}
	paths := map[string]struct{}{}
	for result := range results {
		paths[result.Paths.BaseName] = struct{}{}
		if _, err := os.Stat(result.Paths.JSON); err != nil {
			t.Fatalf("expected export JSON %q: %v", result.Paths.JSON, err)
		}
	}
	if len(paths) != len(outputs) {
		t.Fatalf("concurrent exports reused paths: %v", paths)
	}
}

func TestServiceOutputReservationDoesNotReuseAnInFlightPath(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{cfg: &config.Config{}}
	_, paths, err := prepareExport(map[string]any{"title": "same title"}, svc.exportOpts(dir, []string{"md", "json"}))
	if err != nil {
		t.Fatal(err)
	}
	first, releaseFirst, err := svc.reserveOutputPaths(paths, []string{"md", "json"})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	second, releaseSecond, err := svc.reserveOutputPaths(paths, []string{"md", "json"})
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()
	if first.BaseName == second.BaseName {
		t.Fatalf("in-flight exports reused output path %q", first.BaseName)
	}
}

func TestProviderVideoRetentionHandledRequiresThisRequestsStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  map[string]any
		want bool
	}{
		{name: "no retention status", out: map[string]any{}, want: false},
		{name: "saved", out: map[string]any{"platform_metadata": map[string]any{"video_download_status": "saved"}}, want: true},
		{name: "partial", out: map[string]any{"platform_metadata": map[string]any{"video_download_status": "partial"}}, want: true},
		{name: "not available", out: map[string]any{"platform_metadata": map[string]any{"video_download_status": "not_available"}}, want: true},
		{name: "failed", out: map[string]any{"platform_metadata": map[string]any{"video_download_status": "failed"}}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerVideoRetentionHandled(tc.out); got != tc.want {
				t.Fatalf("handled=%v want %v", got, tc.want)
			}
		})
	}
}

func TestCollectTimeoutIncludesOptedInProviderRetention(t *testing.T) {
	svc := &Service{cfg: &config.Config{
		LocalBrowser: config.LocalBrowserConfig{TotalTimeoutSec: 90},
		VideoDownload: config.VideoDownloadConfig{
			Providers:  map[string]bool{"toutiao": true},
			TimeoutSec: 300,
		},
	}}
	if got, want := svc.collectTimeoutMs("toutiao"), 420000; got != want {
		t.Fatalf("toutiao timeout=%d want %d", got, want)
	}
	if got, want := svc.collectTimeoutMs("douyin"), 120000; got != want {
		t.Fatalf("douyin timeout=%d want %d", got, want)
	}
}

func TestCollectURLExportsContentAndFallsBackAfterProviderVideoRetentionFailure(t *testing.T) {
	enabled := true
	disabled := false
	reg := registry.New()
	reg.Register(registry.Entry{
		Descriptor: protocol.ToolDescriptor{Name: platformprov.ToolToutiaoVideoRead},
		Handler: func(context.Context, *protocol.ToolCall) (map[string]any, []protocol.Artifact, error) {
			return map[string]any{
				"platform":     "toutiao",
				"content_type": "video",
				"title":        "保存失败但正文成功",
				"markdown":     "# 保存失败但正文成功\n\n正文仍需归档。",
				"media":        []any{map[string]any{"type": "video", "artifact_id": "unavailable-video"}},
				"platform_metadata": map[string]any{
					"video_download_status":         "failed",
					"video_download_failure_reason": "download_failed",
				},
			}, nil, providerVideoRetentionTestError{}
		},
	})
	outputDir := t.TempDir()
	svc := &Service{
		cfg: &config.Config{Collect: config.CollectConfig{
			DefaultFormats:  []string{"md", "json"},
			IncludeMedia:    &enabled,
			DownloadMedia:   &enabled,
			PersistMode:     "files",
			IndexEnabled:    &disabled,
			MaxMediaBytes:   1024,
			MediaTimeoutSec: 1,
		}},
		rt: runtime.New(reg, runtime.Options{InstanceID: "test"}),
	}

	result, err := svc.CollectURL(context.Background(), "https://www.toutiao.com/video/7667505035089101366/", []string{"md", "json"}, outputDir)
	if err != nil {
		t.Fatalf("Collect must export parsed content after optional video retention failure: %v", err)
	}
	if _, err := os.Stat(result.Paths.Markdown); err != nil {
		t.Fatalf("expected Markdown export: %v", err)
	}
	data, err := os.ReadFile(result.Paths.JSON)
	if err != nil {
		t.Fatalf("read JSON export: %v", err)
	}
	var exported map[string]any
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("decode JSON export: %v", err)
	}
	meta, _ := exported["platform_metadata"].(map[string]any)
	if meta["video_download_status"] != "failed" || exported["markdown"] == "" {
		t.Fatalf("export lost partial-success state: %#v", exported)
	}
	if len(result.Downloaded) != 1 || result.Downloaded[0]["type"] != "video" || result.Downloaded[0]["error"] == "" {
		t.Fatalf("failed retention must fall back to the normal media export path: %#v", result.Downloaded)
	}
}

func TestServicePersistCollectRetainsConcurrentIndexEntries(t *testing.T) {
	dir := t.TempDir()
	enabled := true
	svc := &Service{cfg: &config.Config{Collect: config.CollectConfig{
		PersistMode:  "index",
		IndexEnabled: &enabled,
	}}}
	const entries = 12
	errs := make(chan error, entries)
	var workers sync.WaitGroup
	for i := range entries {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			url := fmt.Sprintf("https://example.com/%d", i)
			result := &ExportResult{Paths: OutputPaths{BaseName: fmt.Sprintf("item-%d", i)}}
			errs <- svc.persistCollect(context.Background(), url, Match{Platform: "web"}, map[string]any{"title": fmt.Sprintf("item-%d", i)}, result, dir)
		}(i)
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != entries {
		t.Fatalf("index entries=%d want %d", len(idx.Entries), entries)
	}
}

func TestReservePlatformSlotUsesSharedPlatformSpacing(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	svc := &Service{cfg: &config.Config{Collect: config.CollectConfig{
		RateLimitMs:        20,
		PlatformRateLimits: map[string]int{"tiger": 100},
	}}}
	first := svc.reservePlatformSlot("TIGER", now)
	second := svc.reservePlatformSlot("tiger", now)
	otherPlatform := svc.reservePlatformSlot("rss", now)
	if !first.Equal(now) {
		t.Fatalf("first tiger slot=%s want %s", first, now)
	}
	if want := now.Add(100 * time.Millisecond); !second.Equal(want) {
		t.Fatalf("second tiger slot=%s want %s", second, want)
	}
	if !otherPlatform.Equal(now) {
		t.Fatalf("different platform slot=%s want %s", otherPlatform, now)
	}
}
