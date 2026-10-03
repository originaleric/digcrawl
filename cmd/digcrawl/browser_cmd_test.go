package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/watchlist"
	"github.com/originaleric/digcrawl/internal/watchlist/service"
)

func TestRecordWatchlistLoginUnlocksFailedCollect(t *testing.T) {
	enabled := true
	cfg := &config.Config{Watchlist: config.WatchlistConfig{
		LoginRecovery: config.WatchlistLoginRecoveryConfig{Enabled: &enabled},
		Persistence:   config.WatchlistPersistenceConfig{Driver: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "watch.db")},
	}}
	st, err := service.OpenConfiguredStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _, err = st.UpsertFailedCollect(ctx, watchlist.FailedCollect{TaskID: "task", ConfigHash: "v1", Platform: "toutiao", Stage: watchlist.RecoveryDiscoverSource}, 24*time.Hour, true, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := recordWatchlistLogin(cfg, "toutiao"); err != nil {
		t.Fatal(err)
	}
	st, err = service.OpenConfiguredStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ready, err := st.ListReadyFailedCollects(ctx, time.Now())
	if err != nil || len(ready) != 1 {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
}

func TestBrowserProfilePathPrefersProjectConfigThenUserSkillProfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := browserProfilePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("profile=%q want %q", got, path)
	}
	if err := os.MkdirAll("config", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("config/config.yaml", []byte("Gateway: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = browserProfilePath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "config/config.yaml" {
		t.Fatalf("profile=%q want project config", got)
	}
}

func TestBrowserProfilePathRequiresInitialization(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := browserProfilePath("")
	if err == nil || !strings.Contains(err.Error(), "config/config.yaml") || !strings.Contains(err.Error(), "digcrawl init --profile skill") {
		t.Fatalf("error=%v", err)
	}
}

func TestBrowserProfilePathPreservesExplicitProfile(t *testing.T) {
	if got, err := browserProfilePath("/tmp/custom-skill.yaml"); err != nil || got != "/tmp/custom-skill.yaml" {
		t.Fatalf("profile=%q err=%v", got, err)
	}
}
