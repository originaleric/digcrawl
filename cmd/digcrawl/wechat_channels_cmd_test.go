package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
)

func TestConfirmWechatChannelsImport(t *testing.T) {
	confirmed, err := confirmWechatChannelsImport(bytes.NewBufferString("yes\n"), &bytes.Buffer{})
	if err != nil || !confirmed {
		t.Fatalf("confirmation = %v, %v", confirmed, err)
	}
	confirmed, err = confirmWechatChannelsImport(bytes.NewBufferString("\n"), &bytes.Buffer{})
	if err != nil || confirmed {
		t.Fatalf("empty confirmation = %v, %v", confirmed, err)
	}
}

func TestConfirmWechatChannelsBridge(t *testing.T) {
	confirmed, err := confirmWechatChannelsBridge(bytes.NewBufferString("y\n"), &bytes.Buffer{}, 20261)
	if err != nil || !confirmed {
		t.Fatalf("confirmation = %v, %v", confirmed, err)
	}
	confirmed, err = confirmWechatChannelsBridge(bytes.NewBufferString("no\n"), &bytes.Buffer{}, 20261)
	if err != nil || confirmed {
		t.Fatalf("rejection = %v, %v", confirmed, err)
	}
}

func TestPersistWechatChannelsImportUsesStableVideoIdentity(t *testing.T) {
	enabled := true
	dir := t.TempDir()
	cfg := &config.Config{Collect: config.CollectConfig{
		OutputDir:    dir,
		PersistMode:  "both",
		IndexEnabled: &enabled,
		DBPath:       filepath.Join(dir, "collect.db"),
	}}
	content, err := wechatchannels.ImportActive(context.Background(), "https://bridge.example")
	if err == nil || content != nil {
		t.Fatalf("non-loopback bridge must be rejected before import, content=%v err=%v", content, err)
	}

	// The command receives a normalized Content from the local bridge. Build a
	// representative output here so this test stays independent of network I/O.
	out := map[string]any{
		"platform":      wechatchannels.PlatformName,
		"content_type":  wechatchannels.ContentType,
		"source_url":    "wechat-channels://video/object-1?nonce=nonce-1",
		"canonical_url": "wechat-channels://video/object-1?nonce=nonce-1",
		"title":         "Video title",
		"text":          "Description",
		"markdown":      "Description",
		"captured_at":   "2026-08-20T00:00:00Z",
	}
	result, err := collect.Export(context.Background(), out, collect.ExportOptions{OutputDir: dir, Formats: []string{"md", "json"}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if err := persistWechatChannelsImport(context.Background(), cfg, out["source_url"].(string), out, &result); err != nil {
		t.Fatalf("persistWechatChannelsImport() error = %v", err)
	}
	entry, ok, err := collect.FindByURL(dir, out["source_url"].(string))
	if err != nil || !ok || entry.Platform != wechatchannels.PlatformName {
		t.Fatalf("index entry = %#v, found=%v, err=%v", entry, ok, err)
	}
	db, err := collect.CollectDB(cfg)
	if err != nil {
		t.Fatalf("CollectDB() error = %v", err)
	}
	defer db.Close()
	record, err := db.GetByURL(context.Background(), out["source_url"].(string))
	if err != nil || record == nil {
		t.Fatalf("GetByURL() record=%#v err=%v", record, err)
	}
	if record.Collect.Platform != wechatchannels.PlatformName || record.Collect.ToolName != "wechat.channels.import" {
		t.Fatalf("unexpected sqlite record: %#v", record.Collect)
	}
}
