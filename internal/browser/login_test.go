package browser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreadsLoginSpecUsesCurrentDomain(t *testing.T) {
	t.Parallel()

	spec, err := LoginSpecFor("threads")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IndexURL != "https://www.threads.com" || spec.CookieDomain != "www.threads.com" {
		t.Fatalf("unexpected threads login spec: %+v", spec)
	}
}

func TestYouTubeLoginUsesYouTubeSessionScope(t *testing.T) {
	t.Parallel()
	spec, err := LoginSpecFor(" YouTube ")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IndexURL != "https://www.youtube.com" || spec.CookieDomain != "www.youtube.com" {
		t.Fatalf("unexpected YouTube login scope: %+v", spec)
	}
	// Google sign-in may run in this browser, but synchronization must return
	// to YouTube rather than persisting a Google-wide authenticated session.
	for _, rawURL := range spec.CookieURLs {
		if rawURL != "https://www.youtube.com" && rawURL != "https://youtube.com" {
			t.Fatalf("unexpected cross-site cookie warmup: %s", rawURL)
		}
	}
}

func TestPrepareInteractiveLoginProfileRejectsLiveOwnerAndClearsStaleLock(t *testing.T) {
	profile := t.TempDir()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host+"-12345", filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("cookie", filepath.Join(profile, "SingletonCookie")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(os.TempDir(), "digcrawl-stale-socket-12345"), filepath.Join(profile, "SingletonSocket")); err != nil {
		t.Fatal(err)
	}
	cleared, err := prepareInteractiveLoginProfile(profile, func(pid int) (bool, error) { return pid == 12345, nil })
	if !errors.Is(err, errInteractiveLoginInProgress) || cleared {
		t.Fatalf("live lock cleared=%v err=%v, want in-progress error", cleared, err)
	}
	if _, err := os.Lstat(filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatalf("live lock was removed: %v", err)
	}

	cleared, err = prepareInteractiveLoginProfile(profile, func(int) (bool, error) { return false, nil })
	if err != nil || !cleared {
		t.Fatalf("stale lock cleared=%v err=%v", cleared, err)
	}
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		if _, err := os.Lstat(filepath.Join(profile, name)); !os.IsNotExist(err) {
			t.Fatalf("stale lock %s still exists: %v", name, err)
		}
	}
}

func TestPrepareInteractiveLoginProfileDoesNotClearUnknownOwner(t *testing.T) {
	profile := t.TempDir()
	if err := os.Symlink("other-host-12345", filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareInteractiveLoginProfile(profile, func(int) (bool, error) { return false, nil }); err == nil || !strings.Contains(err.Error(), "无法确认") {
		t.Fatalf("unknown-owner error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatalf("unknown lock was removed: %v", err)
	}
}

func TestChromeSingletonLockPIDAcceptsLocalSuffixOnly(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	host = strings.TrimSuffix(host, ".local")
	pid, ok := chromeSingletonLockPID(host + ".local-45678")
	if !ok || pid != 45678 {
		t.Fatalf("parsed pid=%d ok=%v", pid, ok)
	}
	if _, ok := chromeSingletonLockPID("other-host-45678"); ok {
		t.Fatal("foreign host was accepted as a local stale lock")
	}
}

func TestFinishInteractiveLoginReportsCleanupFailure(t *testing.T) {
	cleanupErr := errors.New("profile remains locked")
	if err := finishInteractiveLoginCleanup(nil, true, cleanupErr); err == nil || !strings.Contains(err.Error(), "Cookie 已保存") || !errors.Is(err, cleanupErr) {
		t.Fatalf("success cleanup error=%v", err)
	}
	primary := errors.New("page navigation failed")
	err := finishInteractiveLoginCleanup(primary, false, cleanupErr)
	if !errors.Is(err, primary) || !errors.Is(err, cleanupErr) || !strings.Contains(err.Error(), "登录失败后") {
		t.Fatalf("combined error=%v", err)
	}
}

func TestWaitForLoginConfirmationRequiresNewline(t *testing.T) {
	if err := waitForLoginConfirmation(strings.NewReader("\n")); err != nil {
		t.Fatal(err)
	}
	if err := waitForLoginConfirmation(strings.NewReader("")); err == nil || !strings.Contains(err.Error(), "未收到登录确认") {
		t.Fatalf("error=%v", err)
	}
}

func TestRequireInteractiveTerminalRejectsPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	err = requireInteractiveTerminal(reader)
	if err == nil || !strings.Contains(err.Error(), "可交互终端") {
		t.Fatalf("error=%v, want interactive-terminal guidance", err)
	}
}

func TestRequireInteractiveTerminalRejectsDevNull(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	err = requireInteractiveTerminal(devNull)
	if err == nil || !strings.Contains(err.Error(), "可交互终端") {
		t.Fatalf("error=%v, want interactive-terminal guidance", err)
	}
}

func TestPersistConfirmedLoginCookiesSavesBeforeBestEffortWarmup(t *testing.T) {
	saveCalls := 0
	var events []string
	warnings, err := persistConfirmedLoginCookies(
		func() error {
			saveCalls++
			events = append(events, "save")
			return nil
		},
		"https://www.toutiao.com",
		[]string{"https://www.toutiao.com", "https://toutiao.com"},
		func(rawURL string) error {
			events = append(events, "navigate:"+rawURL)
			return errors.New("connection closed")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if saveCalls != 1 {
		t.Fatalf("save calls=%d, want first snapshot retained despite warmup failure", saveCalls)
	}
	if got, want := strings.Join(events, ","), "save,navigate:https://toutiao.com"; got != want {
		t.Fatalf("events=%q, want %q", got, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Error(), "connection closed") {
		t.Fatalf("warnings=%v", warnings)
	}
}

func TestPersistConfirmedLoginCookiesRefreshesAfterSuccessfulWarmup(t *testing.T) {
	saveCalls := 0
	var events []string
	warnings, err := persistConfirmedLoginCookies(
		func() error {
			saveCalls++
			events = append(events, "save")
			return nil
		},
		"https://www.douyin.com",
		[]string{"https://creator.douyin.com"},
		func(rawURL string) error {
			events = append(events, "navigate:"+rawURL)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || saveCalls != 2 {
		t.Fatalf("warnings=%v save calls=%d, want no warnings and initial+refreshed snapshots", warnings, saveCalls)
	}
	if got, want := strings.Join(events, ","), "save,navigate:https://creator.douyin.com,save"; got != want {
		t.Fatalf("events=%q, want %q", got, want)
	}
}
