package collect

import (
	"context"
	"path/filepath"
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
