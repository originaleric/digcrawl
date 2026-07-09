package watchlist

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreLockAndBaseline(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ok, err := st.AcquireLock(ctx, "w1", "owner1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first lock ok=%v err=%v", ok, err)
	}
	ok, err = st.AcquireLock(ctx, "w1", "owner2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second owner acquired active lock")
	}
	item := FeedItem{ID: "i1", ContentKey: "k1", URL: "https://example.com/a", Title: "A"}
	isNew, err := st.UpsertDiscovered(ctx, "w1", item, true)
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Fatal("baseline item should not be new")
	}
	status, err := st.ItemStatus(ctx, "w1", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusSkippedBaseline {
		t.Fatalf("status=%s", status)
	}
}

func TestStoreDeliveryRetryState(t *testing.T) {
	ctx := context.Background()
	st, err := OpenStore(filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertDelivery(ctx, "w1", "k1", "c1", StatusNotifyFailed, "http 500", true, "title", "body"); err != nil {
		t.Fatal(err)
	}
	pending, err := st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("next_retry_at should delay pending delivery, got %+v", pending)
	}
	if err := st.UpsertDelivery(ctx, "w1", "k2", "c1", StatusNotifyFailed, "http 500", false, "title2", "body2"); err != nil {
		t.Fatal(err)
	}
	pending, err = st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("non-retryable failure should not be pending: %+v", pending)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE watchlist_delivery SET next_retry_at = ? WHERE watch_id = ? AND content_key = ?`, "2000-01-01T00:00:00Z", "w1", "k1"); err != nil {
		t.Fatal(err)
	}
	pending, err = st.ListPendingDeliveries(ctx, "w1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Text != "body" {
		t.Fatalf("expected due retryable delivery: %+v", pending)
	}
	if err := st.UpsertDelivery(ctx, "w1", "k1", "c1", StatusNotified, "", false, "title", "body"); err != nil {
		t.Fatal(err)
	}
}
