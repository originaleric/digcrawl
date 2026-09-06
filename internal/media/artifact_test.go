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
	validMP4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	if _, err := store.Put(context.Background(), "video", "video/mp4", "video.mp4", validMP4); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "video.mp4")
	contentType, err := CopyArtifactToFile(context.Background(), store, "video", dest, "video", 16)
	if err != nil || contentType != "video/mp4" {
		t.Fatalf("contentType=%q err=%v", contentType, err)
	}
	if data, err := os.ReadFile(dest); err != nil || string(data) != string(validMP4) {
		t.Fatalf("data=%q err=%v", data, err)
	}

	oversized := filepath.Join(t.TempDir(), "oversized.mp4")
	if _, err := CopyArtifactToFile(context.Background(), store, "video", oversized, "video", 4); err == nil {
		t.Fatal("expected oversized artifact to fail")
	}
	if _, err := os.Stat(oversized); !os.IsNotExist(err) {
		t.Fatalf("oversized partial output must be removed, stat err=%v", err)
	}
}

func TestCopyArtifactToFileRejectsNonVideoVideoArtifact(t *testing.T) {
	store, err := artifact.NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "invalid-video", "video/mp4", "video.mp4", []byte("not a video")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "video.mp4")
	if _, err := CopyArtifactToFile(context.Background(), store, "invalid-video", dest, "video", 16); err == nil {
		t.Fatal("expected invalid video artifact to be rejected")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("invalid video artifact must be removed, stat err=%v", err)
	}
}
