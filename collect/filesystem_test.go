package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUniquePathsChecksRequestedFormats(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths, err := BuildOutputPaths(dir, "same-title", time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Markdown, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, err := UniquePaths(paths, []string{"md"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Markdown == paths.Markdown {
		t.Fatalf("expected unique markdown path, got same path %s", next.Markdown)
	}
	if filepath.Base(next.Markdown) != "same-title_1.md" {
		t.Fatalf("unexpected next path %s", next.Markdown)
	}
}
