package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexUpsert(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	url := "https://mp.weixin.qq.com/s/test"
	entry := IndexEntry{
		URL:      url,
		Platform: "wechat",
		Title:    "Hello",
	}
	if err := UpsertEntry(dir, entry); err != nil {
		t.Fatal(err)
	}
	entry.Title = "Updated"
	if err := UpsertEntry(dir, entry); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("expected 1 entry after upsert, got %d", len(idx.Entries))
	}
	if idx.Entries[0].Title != "Updated" {
		t.Fatalf("expected updated title, got %q", idx.Entries[0].Title)
	}
	found, ok, err := FindByURL(dir, url)
	if err != nil || !ok {
		t.Fatalf("FindByURL: ok=%v err=%v", ok, err)
	}
	if found.Title != "Updated" {
		t.Fatalf("FindByURL title=%q", found.Title)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatal(err)
	}
}
