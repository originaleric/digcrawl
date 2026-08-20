package collect

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/originaleric/digcrawl/collect/store"
	"github.com/originaleric/digcrawl/config"
)

func TestShowCollectAcceptsNonHTTPSourceURI(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Collect: config.CollectConfig{
		PersistMode: "sqlite",
		DBPath:      filepath.Join(dir, "collect.db"),
	}}
	db, err := CollectDB(cfg)
	if err != nil {
		t.Fatalf("CollectDB() error = %v", err)
	}
	source := "wechat-channels://video/4f626a6563744944?nonce=4e6f6e63654944"
	_, err = db.Upsert(context.Background(), store.UpsertInput{
		SourceURL: source,
		Platform:  "wechat_channels",
		ToolName:  "wechat.channels.import",
		Title:     "Imported video",
		Status:    "success",
	})
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close db: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := ShowCollect(context.Background(), cfg, source)
	if err != nil || got == nil {
		t.Fatalf("ShowCollect() detail=%#v err=%v", got, err)
	}
	if got.Collect.SourceURL != source {
		t.Fatalf("source=%q, want %q", got.Collect.SourceURL, source)
	}
}

func TestPlatformToolNameKeepsWechatChannelsImport(t *testing.T) {
	if got := platformToolName("wechat_channels"); got != "wechat.channels.import" {
		t.Fatalf("tool=%q", got)
	}
}
