package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
)

func TestNewWechatChannelsCaptureStateDirUsesTimestampAndRandomSuffix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "captures")
	now := time.Date(2026, time.August, 24, 12, 34, 56, 123456789, time.FixedZone("test", 8*60*60))
	first, err := newWechatChannelsCaptureStateDir(root, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newWechatChannelsCaptureStateDir(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first) != root || filepath.Dir(second) != root || first == second {
		t.Fatalf("state directories = %q, %q", first, second)
	}
	if !strings.HasPrefix(filepath.Base(first), "20260824T043456.123456789Z-") {
		t.Fatalf("timestamped directory = %q", filepath.Base(first))
	}
	info, err := os.Stat(first)
	if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("state directory info=%#v err=%v", info, err)
	}
}

func TestNewWechatChannelsCaptureOutputPathUsesTimestampAndRandomSuffix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "captures")
	now := time.Date(2026, time.August, 24, 12, 34, 56, 123456789, time.FixedZone("test", 8*60*60))
	first, err := newWechatChannelsCaptureOutputPath(root, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newWechatChannelsCaptureOutputPath(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first) != root || filepath.Dir(second) != root || first == second {
		t.Fatalf("output paths = %q, %q", first, second)
	}
	if !strings.HasPrefix(filepath.Base(first), "wechat_video_20260824T043456.123456789Z-") || filepath.Ext(first) != ".mp4" {
		t.Fatalf("timestamped output path = %q", filepath.Base(first))
	}
	if _, err := os.Lstat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generated output path must remain absent, err=%v", err)
	}
}

func TestAutomaticWechatChannelsCapturePathsAreAbsoluteForRelativeRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	now := time.Date(2026, time.August, 24, 12, 34, 56, 0, time.UTC)
	stateDir, err := newWechatChannelsCaptureStateDir("captures", now)
	if err != nil {
		t.Fatal(err)
	}
	outputPath, err := newWechatChannelsCaptureOutputPath("captures", now)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("captures")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(outputPath) || filepath.Dir(stateDir) != root || filepath.Dir(outputPath) != root {
		t.Fatalf("automatic paths must use absolute capture root %q: state=%q output=%q", root, stateDir, outputPath)
	}
}

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

func TestConfirmWechatChannelsCapture(t *testing.T) {
	confirmed, err := confirmWechatChannelsCapture(bytes.NewBufferString("yes\n"), &bytes.Buffer{}, "/tmp/video.mp4", true, "local")
	if err != nil || !confirmed {
		t.Fatalf("confirmation = %v, %v", confirmed, err)
	}
	confirmed, err = confirmWechatChannelsCapture(bytes.NewBufferString("\n"), &bytes.Buffer{}, "/tmp/video.mp4", false, "local")
	if err != nil || confirmed {
		t.Fatalf("empty confirmation = %v, %v", confirmed, err)
	}
	output := &bytes.Buffer{}
	if _, err := confirmWechatChannelsCapture(bytes.NewBufferString("no\n"), output, "/tmp/video.mp4", true, "api"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte("audio may leave this computer")) {
		t.Fatalf("remote ASR confirmation omitted disclosure: %q", output.String())
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
