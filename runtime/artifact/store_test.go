package artifact

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"testing"
	"time"
)

func TestDiskStoreRemovesExpiredArtifactOnRead(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Put(context.Background(), "expired", "image/png", "page.png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	store.TTL = time.Minute
	expiredAt := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(store.filePath(item.ID), expiredAt, expiredAt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(context.Background(), item.ID); err == nil {
		t.Fatal("expected expired artifact read to fail")
	}
	if _, err := os.Stat(store.filePath(item.ID)); !os.IsNotExist(err) {
		t.Fatalf("expired artifact file should be removed, stat err=%v", err)
	}
}

func TestDiskStoreAutomaticallyRemovesExpiredArtifacts(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Put(context.Background(), "auto-expired", "image/png", "page.png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(store.filePath(item.ID)); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expired artifact was not automatically removed")
}

func TestPutBase64PNGRejectsOversizedArtifact(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, DefaultInlineBytes+1)
	_, err = PutBase64PNG(context.Background(), store, "too-large", base64.StdEncoding.EncodeToString(data), 0)
	if err == nil {
		t.Fatal("expected oversized screenshot artifact to be rejected")
	}
}

func TestPutBase64PNGUsesConfiguredInlineLimit(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 2)
	if _, err := PutBase64PNG(context.Background(), store, "configured", base64.StdEncoding.EncodeToString(data), int64(len(data))); err != nil {
		t.Fatalf("configured limit should be accepted: %v", err)
	}
}

func TestDiskStorePutFileAndOpenStreamsArtifact(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir() + "/video.mp4"
	if err := os.WriteFile(source, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	item, err := store.PutFile(context.Background(), "video", "video/mp4", "video.mp4", source, 16)
	if err != nil {
		t.Fatal(err)
	}
	reader, contentType, err := store.Open(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "video" || contentType != "video/mp4" {
		t.Fatalf("data=%q contentType=%q err=%v", data, contentType, err)
	}
}

func TestDiskStoreRejectsPathLikeArtifactIDsOnWrite(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir() + "/video.mp4"
	if err := os.WriteFile(source, []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../escape", "nested/id", ".", ".."} {
		t.Run(id, func(t *testing.T) {
			if _, err := store.Put(context.Background(), id, "text/plain", "item.txt", []byte("data")); err == nil {
				t.Fatal("Put accepted a path-like ID")
			}
			if _, err := store.PutFile(context.Background(), id, "video/mp4", "video.mp4", source, 16); err == nil {
				t.Fatal("PutFile accepted a path-like ID")
			}
		})
	}
}

func TestNewIDIsOpaqueAndUnique(t *testing.T) {
	first, second := NewID(), NewID()
	if first == "" || second == "" || first == second {
		t.Fatalf("artifact IDs must be non-empty and unique: %q %q", first, second)
	}
}
