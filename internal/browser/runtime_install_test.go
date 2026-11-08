package browser

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/originaleric/digcrawl/config"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOwnedRuntimeNeverFallsBackToDesktop(t *testing.T) {
	t.Setenv("DIGCRAWL_BROWSER_DIR", t.TempDir())
	fake := filepath.Join(t.TempDir(), "chrome-headless-shell")
	if err := os.WriteFile(fake, []byte("desktop executable renamed as shell"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIGCRAWL_CHROME_PATH", fake)
	t.Setenv("CHROME_PATH", fake)
	for _, headed := range []bool{false, true} {
		path, err := RuntimeExecutable(headed)
		if err == nil || path != "" || !strings.Contains(err.Error(), "browser install") {
			t.Fatalf("fallback selected headed=%v path=%q err=%v", headed, path, err)
		}
	}
	cfg := normalizeLocalBrowserConfig(configForRuntimeTest(fake))
	manager := &browserManager{cfg: cfg}
	connection, err := manager.openBrowserConnection(context.Background())
	if err == nil || connection != nil {
		t.Fatalf("custom binary reached launch: %v %v", connection, err)
	}
}

func TestRuntimeExecutableHashIsChecked(t *testing.T) {
	t.Parallel()
	path := browserTestExecutablePath(t.TempDir(), "browser")
	data := []byte("verified fixture")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	asset := runtimeAsset{Kind: "chrome-headless-shell", ExecutableSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	if err := verifyRuntimeExecutable(path, asset); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeExecutable(path, asset); err == nil {
		t.Fatal("tampered browser accepted")
	}
}
func TestLoginRuntimeRejectsDailyMacIdentity(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS application identity")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "Contents", "MacOS", "browser")
	if err := os.MkdirAll(filepath.Dir(bin), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("verified fixture")
	if err := os.WriteFile(bin, data, 0700); err != nil {
		t.Fatal(err)
	}
	asset := runtimeAsset{Kind: "chrome", ExecutableSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	plist := filepath.Join(dir, "Contents", "Info.plist")
	for _, id := range []string{"com.google.Chrome", "com.google.chrome.for.testing"} {
		if err := os.WriteFile(plist, []byte("<plist><dict><key>CFBundleIdentifier</key><string>"+id+"</string></dict></plist>"), 0600); err != nil {
			t.Fatal(err)
		}
		err := verifyRuntimeExecutable(bin, asset)
		if (err == nil) != (id == "com.google.chrome.for.testing") {
			t.Fatalf("identity %q err=%v", id, err)
		}
	}
}
func TestExtraArgsCannotOverrideRuntimeOwnership(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{"--rod-bin=/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "--user-data-dir=/daily", "--profile-directory=Default", "--headless=false", "--remote-debugging-port=9222", "--app=https://example.com"} {
		if err := validateBrowserExtraArgs([]string{arg}); err == nil {
			t.Fatalf("ownership override accepted: %s", arg)
		}
	}
	if err := validateBrowserExtraArgs([]string{"--lang=zh-CN", "--mute-audio=false"}); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimeProfileIsolationIsIdempotent(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	profile := isolatedBrowserProfile(base)
	if profile == base || isolatedBrowserProfile(profile) != profile {
		t.Fatal("profile isolation changed or reused root")
	}
}
func TestRuntimeArchiveRejectsTraversalAndSymlinkWrites(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../escape", "/absolute", "C:/drive", "bad\\path"} {
		t.Run(name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "fixture.zip")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte("x"))
			_ = z.Close()
			_ = f.Close()
			if err := extractRuntimeArchive(context.Background(), archive, t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "link.zip")
	f, _ := os.Create(archive)
	z := zip.NewWriter(f)
	h := &zip.FileHeader{Name: "redirect"}
	h.SetMode(os.ModeSymlink | 0700)
	w, _ := z.CreateHeader(h)
	_, _ = w.Write([]byte("../outside"))
	_ = z.Close()
	_ = f.Close()
	if err := extractRuntimeArchive(context.Background(), archive, t.TempDir()); err == nil {
		t.Fatal("escaping link accepted")
	}
}
func TestStopInstanceRetainsOwnerAfterCleanupFailure(t *testing.T) {
	t.Parallel()
	im := &InstanceManager{instances: map[string]*BrowserInstance{"one": {ID: "one"}}}
	if err := im.StopInstance("one"); err == nil {
		t.Fatal("unverified owner was discarded")
	}
	if _, ok := im.instances["one"]; !ok {
		t.Fatal("cleanup failure lost owner")
	}
}

func configForRuntimeTest(path string) config.LocalBrowserConfig {
	return config.LocalBrowserConfig{ChromePath: path}
}

func TestPinnedRuntimeManifestCoversSupportedDistributions(t *testing.T) {
	m, err := browserRuntimeManifest()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range m.Assets {
		key := a.Platform + "/" + a.Kind
		if seen[key] || len(a.SHA256) != 64 || len(a.ExecutableSHA256) != 64 || a.Size <= 0 || a.Size > 512<<20 {
			t.Fatalf("invalid pin: %s", key)
		}
		seen[key] = true
		expected := "https://storage.googleapis.com/chrome-for-testing-public/" + m.Version + "/" + a.Platform + "/" + a.Kind + "-" + a.Platform + ".zip"
		if a.URL != expected {
			t.Fatal("noncanonical runtime vendor URL")
		}
	}
	for _, platform := range []string{"mac-arm64", "mac-x64", "linux-arm64", "linux64", "win32", "win64"} {
		for _, kind := range []string{"chrome", "chrome-headless-shell"} {
			if !seen[platform+"/"+kind] {
				t.Fatalf("missing %s/%s", platform, kind)
			}
		}
	}
}

func TestInstanceShutdownRetainsFailuresAndStopsNewCreation(t *testing.T) {
	expected := errors.New("unreleased external resource")
	im := &InstanceManager{instances: map[string]*BrowserInstance{
		"ok":     {ID: "ok", connection: &browserConnection{}},
		"failed": {ID: "failed", connection: &browserConnection{cleanupErr: expected}},
	}}
	if err := im.shutdown(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("shutdown failure not returned: %v", err)
	}
	if len(im.instances) != 1 || im.instances["failed"] == nil {
		t.Fatal("shutdown lost failed owner or retained released owner")
	}
	if _, err := im.CreateInstance(context.Background(), "after-stop"); err == nil {
		t.Fatal("creation after shutdown accepted")
	}
}

func TestInstallLockOwnerHelper(t *testing.T) {
	directory := os.Getenv("DIGCRAWL_TEST_INSTALL_LOCK")
	if directory == "" {
		return
	}
	lock, err := AcquireInstallLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(filepath.Join(directory, "ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestInstallLockReleasedAfterOwnerDeath(t *testing.T) {
	directory := t.TempDir()
	// Older installers may have left an empty lock file; it is not a live owner.
	if err := os.WriteFile(filepath.Join(directory, ".digcrawl-install.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestInstallLockOwnerHelper$")
	cmd.Env = append(os.Environ(), "DIGCRAWL_TEST_INSTALL_LOCK="+directory)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(directory, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if contender, err := AcquireInstallLock(directory); err == nil {
		contender.Close()
		t.Fatal("concurrent installation acquired lock")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	replacement, err := AcquireInstallLock(directory)
	if err != nil {
		t.Fatal("dead owner blocked recovery", err)
	}
	defer replacement.Close()
	if _, err := os.Stat(filepath.Join(directory, ".digcrawl-install.lock")); err != nil {
		t.Fatal("lock inode was removed", err)
	}
}
