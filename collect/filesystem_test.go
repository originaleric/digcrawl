package collect

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"
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

func TestBuildOutputPathsTruncatesLongChineseTitleOnUTF8Boundary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	title := "【喜讯】广西医科大学二附院3名青年科技工作者入选2026年广西青年科技人才培育工程培育对象"

	paths, err := BuildOutputPaths(dir, title, time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(paths.BaseName) {
		t.Fatalf("expected valid utf-8 basename, got %q", paths.BaseName)
	}
	if len(paths.BaseName) > maxFilenameBytes {
		t.Fatalf("expected basename <= %d bytes, got %d: %q", maxFilenameBytes, len(paths.BaseName), paths.BaseName)
	}
	if err := os.WriteFile(paths.Markdown, []byte("ok"), 0o644); err != nil {
		t.Fatalf("expected truncated path to be writable: %v", err)
	}
}

func TestBuildOutputPathsDropsInvalidUTF8TitleBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	title := "有效标题" + string([]byte{0xff, 0xfe}) + "尾部"

	paths, err := BuildOutputPaths(dir, title, time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(paths.BaseName) {
		t.Fatalf("expected valid utf-8 basename, got %q", paths.BaseName)
	}
	if paths.BaseName != "有效标题尾部" {
		t.Fatalf("unexpected basename %q", paths.BaseName)
	}
}
