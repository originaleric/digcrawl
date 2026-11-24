package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWatchDefinitionRollbackAndRetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("DIGCRAWL_BROWSER_DIR", "relative-browser-runtime")
	bin := filepath.Join(home, "commands")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// Inject a native dependency failure without altering the user's launchd
	// domain. Files model the existing job, rather than a second service state.
	script := `#!/bin/sh
case "$1" in
print) if [ -f "$HOME/loaded" ]; then echo 'state = running'; else echo 'Could not find service' >&2; exit 113; fi ;;
bootout) rm -f "$HOME/loaded" ;;
bootstrap) if [ -f "$HOME/fail-once" ]; then rm "$HOME/fail-once"; echo 'injected bootstrap failure' >&2; exit 75; fi; touch "$HOME/loaded" ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	profile := filepath.Join(home, "profile.yaml")
	if err := os.WriteFile(profile, []byte("Gateway: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureBridgeWatch(profile, false); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	jobs, err := installedWatchJobs(context.Background(), exe)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	job := jobs[0]
	working, _ := os.Getwd()
	if job.WorkingDirectory != working {
		t.Fatalf("watcher changed cwd: %q != %q", job.WorkingDirectory, working)
	}
	if !bytes.Contains(job.original, []byte("<key>DIGCRAWL_BROWSER_DIR</key><string>"+filepath.Join(working, "relative-browser-runtime")+"</string>")) {
		t.Fatal("watcher lost the installer's browser runtime selection")
	}
	// A missing/relative launch context cannot be guessed from the installer's
	// cwd. Generated historical definitions already contain an absolute value.
	for _, replacement := range []string{"", "<key>WorkingDirectory</key><string>relative</string>"} {
		bad := bytes.Replace(job.original, []byte("<key>WorkingDirectory</key><string>"+working+"</string>"), []byte(replacement), 1)
		if err := os.WriteFile(job.plist, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := installedWatchJobs(context.Background(), exe); err == nil {
			t.Fatal("unknown launch context accepted")
		}
	}
	if err := os.WriteFile(job.plist, job.original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "fail-once"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureBridgeWatch(profile, false); err == nil || !strings.Contains(err.Error(), "injected bootstrap failure") {
		t.Fatalf("failure hidden: %v", err)
	}
	data, err := os.ReadFile(job.plist)
	if err != nil || !bytes.Equal(data, job.original) {
		t.Fatal("previous definition not restored")
	}
	if exists, _, err := watchJobState(context.Background(), job.Label); err != nil || !exists {
		t.Fatalf("previous watcher not reloaded: %v", err)
	}
	if err := configureBridgeWatch(profile, false); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := configureBridgeWatch(profile, true); err != nil {
		t.Fatal(err)
	}
	if err := configureBridgeWatch(profile, true); err != nil {
		t.Fatalf("idempotent uninstall: %v", err)
	}
	if _, err := os.Stat(job.plist); !os.IsNotExist(err) {
		t.Fatal("uninstall intent not persisted")
	}
}
