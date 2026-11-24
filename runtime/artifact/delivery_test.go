package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/protocol"
)

func TestMaterializeKeepsDeliveryFailuresAndActualSizeBound(t *testing.T) {
	store, err := NewDiskStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), "image", "image/png", "fixture", []byte("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "declared_oversize", "actual_oversize", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			item := ref
			limit := int64(5)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "missing":
				item.ID = "absent"
				item.Size = 1
			case "actual_oversize":
				item.Size = 1
			case "canceled":
				cancel()
				limit = 20
			}
			result := Materialize(ctx, []protocol.Artifact{item}, store, limit)
			if len(result) != 1 || result[0].InlineData != nil || result[0].InlineError == "" {
				t.Fatalf("delivery error lost: %+v", result)
			}
			// Failed delivery is a value, not permission to fetch from a new owner.
			_, _, err := ReadContent(context.Background(), result[0], nil, limit)
			if err == nil {
				t.Fatal("failed content accepted")
			}
			if mode == "actual_oversize" && !strings.Contains(err.Error(), "inline limit") {
				t.Fatal(err)
			}
		})
	}
	if ref.InlineData != nil || ref.InlineError != "" {
		t.Fatal("materialization mutated source metadata")
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "inline_") {
		t.Fatal("plain artifact reference gained transport data")
	}
}

func TestMaterializedContentSurvivesStoreRemoval(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskStore(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), "image", "image/png", "fixture", []byte("ready"))
	if err != nil {
		t.Fatal(err)
	}
	items := Materialize(context.Background(), []protocol.Artifact{ref}, store, 10)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	data, mime, err := ReadContent(context.Background(), items[0], nil, 10)
	if err != nil || string(data) != "ready" || mime != "image/png" {
		t.Fatalf("delivered result still needs store: %s %s %v", data, mime, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadContent(ctx, items[0], nil, 10); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
