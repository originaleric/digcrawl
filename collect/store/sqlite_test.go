package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteUpsertAndQuery(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "collect.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var foreignKeys int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d", foreignKeys)
	}

	ctx := context.Background()
	url := "https://mp.weixin.qq.com/s/test-article"
	in := UpsertInput{
		SourceURL:   url,
		Platform:    "wechat",
		ToolName:    "wechat.article.read",
		Title:       "Hello",
		ContentType: "article",
		Status:      "success",
		CapturedAt:  time.Now().UTC().Format(time.RFC3339),
		OutputRoot:  t.TempDir(),
		BaseName:    "hello",
		Files: []FileRecord{
			{Kind: "md", Path: "/tmp/hello.md"},
			{Kind: "json", Path: "/tmp/hello.json"},
		},
		Media: []MediaRecord{
			{MediaType: "image", RemoteURL: "https://mmbiz.qpic.cn/a.jpg", LocalPath: "hello.assets/001.jpg", SortOrder: 0},
			{MediaType: "image", RemoteURL: "https://mmbiz.qpic.cn/b.jpg", DownloadError: "http 403", SortOrder: 1},
		},
	}
	id1, err := s.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" {
		t.Fatal("expected collect id")
	}

	in.Title = "Updated"
	in.Media = []MediaRecord{
		{MediaType: "image", RemoteURL: "https://mmbiz.qpic.cn/c.jpg", LocalPath: "hello.assets/001.jpg", SortOrder: 0},
	}
	id2, err := s.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("expected same id on upsert, got %q vs %q", id1, id2)
	}

	byURL, err := s.GetByURL(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if byURL == nil || byURL.Collect.Title != "Updated" {
		t.Fatalf("GetByURL title=%q", byURL.Collect.Title)
	}
	if len(byURL.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(byURL.Files))
	}
	if len(byURL.Media) != 1 {
		t.Fatalf("expected 1 media row after replace, got %d", len(byURL.Media))
	}

	list, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 collect, got %d", len(list))
	}

	found, err := s.Search(ctx, "updated", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("search expected 1, got %d", len(found))
	}

	byID, err := s.GetByID(ctx, id1)
	if err != nil || byID == nil {
		t.Fatalf("GetByID: %v", err)
	}
}

func TestSQLitePersistModesIndependent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "collect.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	for i, url := range []string{
		"https://example.com/a",
		"https://example.com/b",
	} {
		_, err := s.Upsert(ctx, UpsertInput{
			SourceURL:  url,
			Platform:   "web",
			ToolName:   "browser.browse",
			Status:     "success",
			CapturedAt: time.Now().UTC().Format(time.RFC3339),
			OutputRoot: t.TempDir(),
			BaseName:   "item",
			Media:      []MediaRecord{{MediaType: "image", RemoteURL: url, SortOrder: i}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 collects, got %d", len(list))
	}
}

func TestSQLiteMigratesLegacyTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "collect.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE collects (
  id TEXT PRIMARY KEY,
  source_url TEXT NOT NULL,
  canonical_url TEXT,
  platform TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  title TEXT,
  content_type TEXT,
  status TEXT NOT NULL,
  error_code TEXT,
  captured_at TEXT NOT NULL,
  output_root TEXT NOT NULL,
  base_name TEXT,
  created_at TEXT NOT NULL
);
CREATE TABLE collect_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  collect_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL
);
CREATE TABLE collect_media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  collect_id TEXT NOT NULL,
  media_type TEXT NOT NULL,
  remote_url TEXT,
  local_path TEXT,
  download_error TEXT,
  sort_order INTEGER DEFAULT 0
);
	CREATE TABLE tool_runs (
	  id TEXT PRIMARY KEY,
	  tool_name TEXT NOT NULL,
	  source_url TEXT,
	  status TEXT NOT NULL,
	  output_json TEXT,
	  created_at TEXT NOT NULL
	);
` + migrationSQL + `
INSERT INTO dc_collects (
  id, source_url, canonical_url, platform, tool_name, title, content_type,
  status, error_code, captured_at, output_root, base_name, created_at
) VALUES ('existing', 'https://example.com/existing', NULL, 'web', 'browser.browse', 'Existing', 'article', 'success', NULL, '2026-01-01T00:00:00Z', '/tmp/out', 'existing', '2026-01-01T00:00:00Z');
INSERT INTO dc_collect_files (id, collect_id, kind, path) VALUES (1, 'existing', 'md', '/tmp/existing.md');
INSERT INTO dc_collect_media (id, collect_id, media_type, remote_url, local_path, download_error, sort_order) VALUES (1, 'existing', 'image', 'https://example.com/existing.png', 'existing.png', NULL, 0);
INSERT INTO collects (
  id, source_url, canonical_url, platform, tool_name, title, content_type,
  status, error_code, captured_at, output_root, base_name, created_at
) VALUES ('c1', 'https://example.com/a', NULL, 'web', 'browser.browse', 'Legacy', 'article', 'success', NULL, '2026-01-01T00:00:00Z', '/tmp/out', 'legacy', '2026-01-01T00:00:00Z');
INSERT INTO collect_files (id, collect_id, kind, path) VALUES (1, 'c1', 'md', '/tmp/a.md');
INSERT INTO collect_files (id, collect_id, kind, path) VALUES (2, 'missing-parent', 'md', '/tmp/orphan.md');
INSERT INTO collect_media (id, collect_id, media_type, remote_url, local_path, download_error, sort_order) VALUES (1, 'c1', 'image', 'https://example.com/a.png', 'a.png', NULL, 0);
INSERT INTO collect_media (id, collect_id, media_type, remote_url, local_path, download_error, sort_order) VALUES (2, 'missing-parent', 'image', 'https://example.com/orphan.png', 'orphan.png', NULL, 0);
INSERT INTO tool_runs (id, tool_name, source_url, status, output_json, created_at) VALUES ('r1', 'browser.browse', 'https://example.com/a', 'success', '{}', '2026-01-01T00:00:00Z');
	`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}

	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	detail, err := s.GetByURL(context.Background(), "https://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil || detail.Collect.Title != "Legacy" || len(detail.Files) != 1 || len(detail.Media) != 1 {
		t.Fatalf("legacy collect not migrated: %+v", detail)
	}
	if detail.Files[0].Path != "/tmp/a.md" {
		t.Fatalf("legacy file was not migrated through id collision: %+v", detail.Files)
	}
	if detail.Media[0].RemoteURL != "https://example.com/a.png" {
		t.Fatalf("legacy media was not migrated through id collision: %+v", detail.Media)
	}
	var orphanFiles int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM dc_collect_files WHERE collect_id = 'missing-parent'`).Scan(&orphanFiles); err != nil {
		t.Fatal(err)
	}
	if orphanFiles != 0 {
		t.Fatalf("orphanFiles=%d", orphanFiles)
	}
	var orphanMedia int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM dc_collect_media WHERE collect_id = 'missing-parent'`).Scan(&orphanMedia); err != nil {
		t.Fatal(err)
	}
	if orphanMedia != 0 {
		t.Fatalf("orphanMedia=%d", orphanMedia)
	}
	var toolRuns int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM dc_tool_runs`).Scan(&toolRuns); err != nil {
		t.Fatal(err)
	}
	if toolRuns != 1 {
		t.Fatalf("toolRuns=%d", toolRuns)
	}
}

func TestSQLiteUpsertNormalizesURLKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "collect.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	in := UpsertInput{
		SourceURL:  "https://EXAMPLE.com/a/",
		Platform:   "web",
		ToolName:   "browser.browse",
		Title:      "First",
		Status:     "success",
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		OutputRoot: t.TempDir(),
		BaseName:   "item",
	}
	id1, err := s.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	in.SourceURL = "https://example.com/a"
	in.Title = "Second"
	id2, err := s.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("expected same id, got %q vs %q", id1, id2)
	}

	detail, err := s.GetByURL(ctx, "https://example.com/a/")
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil || detail.Collect.Title != "Second" {
		t.Fatalf("expected updated collect, got %#v", detail)
	}
}
