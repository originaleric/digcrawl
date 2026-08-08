package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestNormalizeLocalBrowserConfigKeepsExplicitChromePath(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit-chrome")
	envPath := filepath.Join(t.TempDir(), "env-chrome")
	writeExecutable(t, envPath)
	t.Setenv("DIGCRAWL_CHROME_PATH", envPath)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{ChromePath: explicit})
	if cfg.ChromePath != explicit {
		t.Fatalf("explicit ChromePath should be preserved, got %q", cfg.ChromePath)
	}
}

func TestNormalizeLocalBrowserConfigUsesEnvChromePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chrome")
	writeExecutable(t, path)
	t.Setenv("DIGCRAWL_CHROME_PATH", path)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{})
	if cfg.ChromePath != path {
		t.Fatalf("expected env ChromePath %q, got %q", path, cfg.ChromePath)
	}
}

func TestNormalizeLocalBrowserConfigPrefersPlaywrightHeadlessShell(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, playwrightHeadlessShellRelPath())
	writeExecutable(t, path)
	t.Setenv("DIGCRAWL_CHROME_PATH", "")
	t.Setenv("CHROME_PATH", "")
	t.Setenv("CHROME_BIN", "")
	t.Setenv("DIGCRAWL_PLAYWRIGHT_BROWSERS_PATH", root)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{})
	if cfg.ChromePath != path {
		t.Fatalf("expected Playwright headless shell %q, got %q", path, cfg.ChromePath)
	}
	if !containsArg(cfg.ExtraArgs, "--disable-dev-shm-usage") {
		t.Fatalf("expected headless shell compatibility args, got %+v", cfg.ExtraArgs)
	}
}

func TestNormalizeLocalBrowserConfigDoesNotDuplicateHeadlessShellArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), playwrightHeadlessShellRelPath())
	writeExecutable(t, path)

	cfg := normalizeLocalBrowserConfig(config.LocalBrowserConfig{
		ChromePath: path,
		ExtraArgs:  []string{"--disable-dev-shm-usage"},
	})
	count := 0
	for _, arg := range cfg.ExtraArgs {
		if arg == "--disable-dev-shm-usage" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected compatibility arg once, got %d in %+v", count, cfg.ExtraArgs)
	}
}

func TestDiscoverInteractiveChromePathPrefersVisibleExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Chrome")
	if err := os.WriteFile(path, []byte("browser"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := discoverInteractiveChromePath(path); got != path {
		t.Fatalf("interactive path=%q want %q", got, path)
	}
}

func TestDiscoverInteractiveChromePathRejectsHeadlessShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chrome-headless-shell")
	if err := os.WriteFile(path, []byte("browser"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"DIGCRAWL_CHROME_PATH", "CHROME_PATH", "CHROME_BIN"} {
		t.Setenv(key, path)
	}
	if got := discoverInteractiveChromePath(path); got == path {
		t.Fatalf("headless shell must not be selected for interactive login: %q", got)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func playwrightHeadlessShellRelPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "chromium_headless_shell-9999/chrome-headless-shell-mac-arm64/chrome-headless-shell"
	case "windows":
		return "chromium_headless_shell-9999/chrome-headless-shell-win64/chrome-headless-shell.exe"
	default:
		return "chromium_headless_shell-9999/chrome-headless-shell-linux-x64/chrome-headless-shell"
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
