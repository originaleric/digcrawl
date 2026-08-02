package artifact

import (
	"context"
	"encoding/base64"
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
