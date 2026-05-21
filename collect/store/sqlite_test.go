package store

import (
	"context"
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
