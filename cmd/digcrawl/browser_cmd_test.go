package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
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

func TestInstallerDefaultProfileMigrationPreservesCookieStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := config.DefaultSkillProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := migrateDefaultBrowserProfile(context.Background()); err != nil {
		t.Fatal("fresh installation should have no migration", err)
	}
	data := []byte("LocalBrowser:\n  ChromePath: /legacy/desktop\n  CookieStoreDir: /preserve/cookies\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateDefaultBrowserProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(saved), "/preserve/cookies") || strings.Contains(string(saved), "/legacy/desktop") {
		t.Fatal("migration changed session location or retained executable override", err)
	}
}

func TestBrowserCheckConcurrentProfileAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_RUNTIME_ACCEPTANCE") != "1" {
		t.Skip("real pinned browser CLI acceptance is opt-in")
	}
	if runtime.GOOS != "linux" {
		t.Skip("local HTTPS host-mapping fixture runs on Linux; macOS concurrency is separately verified against the signed-in session")
	}
	t.Setenv("DIGCRAWL_BROWSER_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := browser.InstallBrowserRuntime(ctx, os.Getenv("DIGCRAWL_RUNTIME_ARCHIVES")); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("<html><title>session check fixture</title><body>local session check</body></html>"))
	}))
	defer server.Close()
	profile := filepath.Join(t.TempDir(), "persistent")
	guard, _, err := browser.AcquireProfileLock(filepath.Join(profile, "digcrawl-isolated"))
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	configuration, err := json.Marshal(map[string]any{"LocalBrowser": map[string]any{
		"UserDataDir": profile, "CookieStoreDir": t.TempDir(),
		"ExtraArgs": []string{"--host-resolver-rules=MAP www.toutiao.com " + server.Listener.Addr().String(), "--ignore-certificate-errors", "--no-proxy-server"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, configuration, 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "digcrawl")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	temporary := t.TempDir()
	for _, platform := range []string{"toutiao", "invalid-platform"} {
		cmd := exec.CommandContext(ctx, executable, "browser", "check", "--config", path, "--platform", platform)
		cmd.Env = append(os.Environ(), "TMPDIR="+temporary, "TMP="+temporary, "TEMP="+temporary)
		out, err := cmd.CombinedOutput()
		if platform == "toutiao" {
			if err != nil || !strings.Contains(string(out), "pong ok") || requests.Load() == 0 {
				t.Fatalf("check competed with the held persistent profile or missed local fixture: %v: %s", err, out)
			}
		} else if err == nil {
			t.Fatal("invalid platform accepted")
		}
		remaining, err := filepath.Glob(filepath.Join(temporary, "digcrawl-browser-check-*"))
		if err != nil || len(remaining) != 0 {
			t.Fatalf("temporary check profile retained after %s: %v %v", platform, remaining, err)
		}
	}
	if replacement, _, err := browser.AcquireProfileLock(filepath.Join(profile, "digcrawl-isolated")); err == nil {
		replacement.Close()
		t.Fatal("CLI released the persistent owner's lock")
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != string(configuration) {
		t.Fatal("CLI check modified persisted browser configuration")
	}
}

func TestInstallerLockProtocolAcceptance(t *testing.T) {
	if os.Getenv("DIGCRAWL_RUNTIME_ACCEPTANCE") != "1" {
		t.Skip("real CLI protocol acceptance is opt-in")
	}
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM case requires Unix; Windows requires native acceptance")
	}
	t.Setenv("DIGCRAWL_BROWSER_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := browser.InstallBrowserRuntime(ctx, os.Getenv("DIGCRAWL_RUNTIME_ARCHIVES")); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "digcrawl")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	directory, shared := t.TempDir(), t.TempDir()
	for _, protocol := range []struct {
		name, ready string
		directories []string
	}{
		{"single", "DIGCRAWL_INSTALL_READY", []string{directory}},
		{"shared", "DIGCRAWL_INSTALL_READY_V2", []string{shared, directory, shared}},
	} {
		for _, ending := range []string{"release", "EOF", "SIGTERM"} {
			t.Run(protocol.name+"/"+ending, func(t *testing.T) {
				args := []string{"browser", "install"}
				for _, resource := range protocol.directories {
					args = append(args, "--installer-lock", resource)
				}
				cmd := exec.CommandContext(ctx, executable, args...)
				input, err := cmd.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				var diagnostic bytes.Buffer
				cmd.Stderr = &diagnostic
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() { input.Close(); cmd.Process.Kill(); cmd.Wait() }()
				reader := bufio.NewReader(output)
				line, err := reader.ReadString('\n')
				if err != nil || strings.TrimSpace(line) != protocol.ready {
					t.Fatalf("ready: %q %v %s", line, err, diagnostic.String())
				}
				for _, resource := range protocol.directories {
					if competing, err := browser.AcquireInstallLock(resource); err == nil {
						competing.Close()
						t.Fatalf("live owner was not exclusive for %s", resource)
					}
				}
				if ending == "release" {
					if _, err := input.Write([]byte("release\n")); err != nil {
						t.Fatal(err)
					}
					ack, err := reader.ReadString('\n')
					if err != nil || strings.TrimSpace(ack) != "DIGCRAWL_INSTALL_RELEASED" {
						t.Fatalf("ack: %q %v", ack, err)
					}
				} else if ending == "EOF" {
					input.Close()
				} else {
					cmd.Process.Signal(syscall.SIGTERM)
				}
				err = cmd.Wait()
				if (ending == "release") != (err == nil) {
					t.Fatalf("%s returned %v: %s", ending, err, diagnostic.String())
				}
				for _, resource := range protocol.directories {
					replacement, err := browser.AcquireInstallLock(resource)
					if err != nil {
						t.Fatal("owner did not release lock", err)
					}
					replacement.Close()
				}
			})
		}
	}
}
