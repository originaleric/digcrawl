package collect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/collect/store"
	"github.com/originaleric/digcrawl/config"
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
