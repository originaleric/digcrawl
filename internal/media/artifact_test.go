package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/runtime/artifact"
)

func TestCopyArtifactToFileStreamsWithinLimitAndRemovesOversizedPartialFile(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "video", "video/mp4", "video.mp4", []byte("video")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "video.mp4")
	contentType, err := CopyArtifactToFile(context.Background(), store, "video", dest, 5)
	if err != nil || contentType != "video/mp4" {
		t.Fatalf("contentType=%q err=%v", contentType, err)
	}
	if data, err := os.ReadFile(dest); err != nil || string(data) != "video" {
		t.Fatalf("data=%q err=%v", data, err)
	}

	oversized := filepath.Join(t.TempDir(), "oversized.mp4")
	if _, err := CopyArtifactToFile(context.Background(), store, "video", oversized, 4); err == nil {
		t.Fatal("expected oversized artifact to fail")
	}
	if _, err := os.Stat(oversized); !os.IsNotExist(err) {
		t.Fatalf("oversized partial output must be removed, stat err=%v", err)
	}
}
