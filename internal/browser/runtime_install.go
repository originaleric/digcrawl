package browser

// DigCrawl owns its browser distribution. Runtime selection never searches the
// desktop, PATH or a third-party cache: failure leaves the daily browser alone.
import (
	"archive/zip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/go-rod/rod/lib/launcher"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed runtime-lock.json
var runtimeLockData []byte

type runtimeAsset struct {
	Kind             string `json:"kind"`
	Platform         string `json:"platform"`
	URL              string `json:"url"`
	SHA256           string `json:"sha256"`
	Size             int64  `json:"size"`
	ExecutableSHA256 string `json:"executable_sha256"`
}
type runtimeManifest struct {
	Version string         `json:"version"`
	Assets  []runtimeAsset `json:"assets"`
}

func browserRuntimeManifest() (runtimeManifest, error) {
	var m runtimeManifest
	if err := json.Unmarshal(runtimeLockData, &m); err != nil {
		return m, err
	}
	if m.Version == "" {
		return m, errors.New("browser runtime manifest has no version")
	}
	return m, nil
}
func browserRuntimePlatform() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "mac-arm64"
	case "darwin/amd64":
		return "mac-x64"
	case "linux/amd64":
		return "linux64"
	case "linux/arm64":
		return "linux-arm64"
	case "windows/amd64":
		return "win64"
	case "windows/386":
		return "win32"
	}
	return ""
}
func browserRuntimeRoot() (string, error) {
	if p := strings.TrimSpace(os.Getenv("DIGCRAWL_BROWSER_DIR")); p != "" {
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Abs(filepath.Join(base, "digcrawl", "browsers"))
}
func runtimeExecutableRelative(kind, platform string) string {
	base := kind + "-" + platform
	if kind == "chrome-headless-shell" {
		name := "chrome-headless-shell"
		if strings.HasPrefix(platform, "win") {
			name += ".exe"
		}
		return filepath.Join(base, name)
	}
	if strings.HasPrefix(platform, "mac-") {
		return filepath.Join(base, "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing")
	}
	name := "chrome"
	if strings.HasPrefix(platform, "win") {
		name = "chrome.exe"
	}
	return filepath.Join(base, name)
}
func runtimeAssetFor(m runtimeManifest, kind string) (runtimeAsset, error) {
	platform := browserRuntimePlatform()
	for _, a := range m.Assets {
		if a.Platform == platform && a.Kind == kind {
			return a, nil
		}
	}
	return runtimeAsset{}, fmt.Errorf("unsupported DigCrawl browser runtime platform %s/%s", runtime.GOOS, runtime.GOARCH)
}
func runtimeInstallDir(m runtimeManifest) (string, error) {
	root, err := browserRuntimeRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, m.Version+"-"+browserRuntimePlatform()), nil
}
func hashRuntimeFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func runtimeDependencyError(err error) error {
	return fmt.Errorf("DigCrawl isolated browser unavailable: %w; run `digcrawl browser install` (offline: --archive-dir DIR); desktop Chrome will not be used", err)
}

// RuntimeExecutable returns only the pinned, verified binary. Legacy paths are
// checked at the launch boundary rather than silently honored or rediscovered.
func RuntimeExecutable(interactive bool) (string, error) {
	m, err := browserRuntimeManifest()
	if err != nil {
		return "", runtimeDependencyError(err)
	}
	dir, err := runtimeInstallDir(m)
	if err != nil {
		return "", runtimeDependencyError(err)
	}
	kind := "chrome-headless-shell"
	if interactive {
		kind = "chrome"
	}
	a, err := runtimeAssetFor(m, kind)
	if err != nil {
		return "", runtimeDependencyError(err)
	}
	bin := filepath.Join(dir, runtimeExecutableRelative(kind, a.Platform))
	if err = verifyRuntimeExecutable(bin, a); err != nil {
		return "", runtimeDependencyError(err)
	}
	return bin, nil
}
func verifyRuntimeExecutable(bin string, a runtimeAsset) error {
	if len(a.ExecutableSHA256) != 64 {
		return errors.New("runtime executable has no pinned SHA-256")
	}
	info, err := os.Lstat(bin)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("browser executable must not be a symlink")
	}
	if !isExecutableFile(bin) {
		return errors.New("browser executable missing or not executable")
	}
	digest, err := hashRuntimeFile(bin)
	if err != nil {
		return err
	}
	if digest != a.ExecutableSHA256 {
		return errors.New("browser executable integrity mismatch")
	}
	if runtime.GOOS == "darwin" && a.Kind == "chrome" {
		f, err := os.Open(filepath.Join(filepath.Dir(filepath.Dir(bin)), "Info.plist"))
		if err != nil {
			return err
		}
		defer f.Close()
		decoder := xml.NewDecoder(io.LimitReader(f, 1<<20))
		found := false
		for {
			tok, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "key" {
				var key string
				if err = decoder.DecodeElement(&key, &start); err != nil {
					return err
				}
				if key == "CFBundleIdentifier" {
					found = true
				}
			}
			if start, ok := tok.(xml.StartElement); ok && found && start.Name.Local == "string" {
				var id string
				if err = decoder.DecodeElement(&id, &start); err != nil {
					return err
				}
				if id != "com.google.chrome.for.testing" {
					return errors.New("login browser has daily Chrome application identity")
				}
				return nil
			}
		}
		return errors.New("login browser application identity missing")
	}
	return nil
}
func validateOwnedRuntimePath(bin string, interactive bool) error {
	expected, err := RuntimeExecutable(interactive)
	if err != nil {
		return err
	}
	actual, err := filepath.Abs(strings.TrimSpace(bin))
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("desktop/custom ChromePath is no longer allowed for owned browsers; run `digcrawl browser install` and remove the legacy ChromePath override")
	}
	return nil
}

// InstallBrowserRuntime stages both matching distributions, validates every
// archive before extraction and publishes one immutable version directory.
// Existing versions (including ones still used by old processes) are retained.
func InstallBrowserRuntime(ctx context.Context, archiveDir string) error {
	return installBrowserRuntime(ctx, archiveDir, false)
}

// Repair retains the previous directory; it never removes files that may
// still belong to a running process. Windows may refuse an in-use rename.
func RepairBrowserRuntime(ctx context.Context, archiveDir string) error {
	return installBrowserRuntime(ctx, archiveDir, true)
}
func installBrowserRuntime(ctx context.Context, archiveDir string, repair bool) error {
	m, err := browserRuntimeManifest()
	if err != nil {
		return err
	}
	target, err := runtimeInstallDir(m)
	if err != nil {
		return err
	}
	root := filepath.Dir(target)
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	lock, err := acquireProfileFileLock(filepath.Join(root, "install.lock"))
	if err != nil {
		return fmt.Errorf("another browser install may be running: %w", err)
	}
	defer lock.Close()
	unfinished, err := filepath.Glob(filepath.Join(root, ".install-*", "probe-owner.json"))
	if err != nil {
		return err
	}
	if len(unfinished) > 0 {
		return fmt.Errorf("prior runtime self-test cleanup remains unresolved at %s; verify that exact owner has exited before removing its retained stage", unfinished[0])
	}
	if !repair {
		if _, err = RuntimeExecutable(false); err == nil {
			if _, err = RuntimeExecutable(true); err == nil {
				return nil
			}
		}
	}
	_, existingErr := os.Lstat(target)
	if existingErr == nil && !repair {
		return fmt.Errorf("runtime directory exists but is invalid; migrate/stop its owners, then run `digcrawl browser install --repair` to preserve and replace it")
	}
	if existingErr != nil && !os.IsNotExist(existingErr) {
		return existingErr
	}
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return err
	}
	preserveStage := false
	defer func() {
		if !preserveStage {
			_ = os.RemoveAll(stage)
		}
	}()
	for _, kind := range []string{"chrome-headless-shell", "chrome"} {
		a, e := runtimeAssetFor(m, kind)
		if e != nil {
			return e
		}
		archive := filepath.Join(stage, kind+".zip")
		if err = obtainRuntimeArchive(ctx, a, archiveDir, archive); err != nil {
			return err
		}
		if err = extractRuntimeArchive(ctx, archive, stage); err != nil {
			return err
		}
		if err = os.Remove(archive); err != nil {
			return err
		}
		bin := filepath.Join(stage, runtimeExecutableRelative(kind, a.Platform))
		if err = verifyRuntimeExecutable(bin, a); err != nil {
			return err
		}
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		profile, err := os.MkdirTemp(stage, ".probe-profile-")
		if err != nil {
			cancel()
			return err
		}
		// Self-tests use only about:blank and the verified staged binary. Full
		// Chrome is also tested headlessly; no login window is opened by install.
		l := launcher.New().Context(probeCtx).Bin(bin).UserDataDir(profile).Headless(true).NoSandbox(true)
		args := []string(nil)
		if kind == "chrome-headless-shell" {
			args = headlessShellCompatibilityArgs()
		}
		l = applyBackgroundLauncherPolicy(l, args)
		// Publish intent before spawning: even an interrupted installer leaves
		// an actionable ownership boundary instead of an anonymous live stage.
		intent := []byte(fmt.Sprintf("{\"installer_pid\":%d,\"phase\":\"starting\"}\n", os.Getpid()))
		if err := os.WriteFile(filepath.Join(stage, "probe-owner.json"), intent, 0600); err != nil {
			cancel()
			return err
		}
		url, probeErr := launchBrowser(l)
		connection := newBrowserConnection(nil, nil, l, true, profile, 0)
		if probeErr == nil {
			connection.browser, connection.transport, probeErr = connectCDPBrowser(probeCtx, url, 10*time.Second)
		}
		if probeErr == nil {
			version, err := connection.browser.Context(probeCtx).Version()
			if err != nil {
				probeErr = err
			} else if !strings.Contains(version.Product, m.Version) {
				probeErr = errors.New("runtime version mismatch")
			}
		}
		cancel()
		cleanup, finish := context.WithTimeout(context.Background(), browserShutdownTimeout)
		cleanupErr := connection.closeResources(cleanup)
		finish()
		if cleanupErr != nil {
			preserveStage = true
			owner, marshalErr := json.Marshal(struct {
				PID        int    `json:"pid"`
				Identity   string `json:"identity"`
				Executable string `json:"executable"`
			}{l.PID(), connection.ownerIdentity, bin})
			var recordErr error
			if marshalErr == nil {
				recordErr = os.WriteFile(filepath.Join(stage, "probe-owner.json"), owner, 0600)
			}
			return fmt.Errorf("runtime self-test owner cleanup incomplete; retained %s (PID %d): %w", stage, l.PID(), errors.Join(probeErr, cleanupErr, marshalErr, recordErr))
		}
		if probeErr != nil || cleanupErr != nil {
			return fmt.Errorf("%s CDP self-test failed: %w", kind, errors.Join(probeErr, cleanupErr))
		}
		if err = os.Remove(filepath.Join(stage, "probe-owner.json")); err != nil {
			return err
		}
		if err = os.RemoveAll(profile); err != nil {
			return err
		}
	}

	if err = ctx.Err(); err != nil {
		return err
	}
	if existingErr == nil {
		previous, err := os.MkdirTemp(root, ".previous-"+m.Version+"-")
		if err != nil {
			return err
		}
		if err = os.Remove(previous); err != nil {
			return err
		}
		if err = os.Rename(target, previous); err != nil {
			return fmt.Errorf("preserve previous runtime (stop its owners first): %w", err)
		}
		if err = os.Rename(stage, target); err != nil {
			return errors.Join(err, os.Rename(previous, target))
		}
		return nil
	}
	return os.Rename(stage, target)
}
func obtainRuntimeArchive(ctx context.Context, a runtimeAsset, archiveDir, dst string) error {
	if len(a.SHA256) != 64 || a.Size <= 0 || a.Size > 512<<20 {
		return errors.New("invalid pinned runtime archive")
	}
	var input io.ReadCloser
	if archiveDir != "" {
		f, err := os.Open(filepath.Join(archiveDir, a.Kind+"-"+a.Platform+".zip"))
		if err != nil {
			return err
		}
		input = f
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
		if err != nil {
			return err
		}
		client := &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.Host != "storage.googleapis.com" {
				return errors.New("unexpected runtime download redirect")
			}
			if len(via) > 3 {
				return errors.New("too many runtime download redirects")
			}
			return nil
		}}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return fmt.Errorf("runtime download HTTP %d", res.StatusCode)
		}
		input = res.Body
	}
	defer input.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(input, a.Size+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("runtime archive integrity mismatch")
	}
	return ctx.Err()
}
func extractRuntimeArchive(ctx context.Context, archive, dst string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	var expanded uint64
	links := map[string]string{}
	for _, entry := range z.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		name := entry.Name
		if strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") {
			return errors.New("unsafe browser archive path")
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("browser archive path escapes install directory")
		}
		out := filepath.Join(dst, clean)
		expanded += entry.UncompressedSize64
		if expanded > 2<<30 {
			return errors.New("browser archive expanded size exceeds limit")
		}
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(out, 0700); err != nil {
				return err
			}
			continue
		}
		r, e := entry.Open()
		if e != nil {
			return e
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			b, e := io.ReadAll(io.LimitReader(r, 4097))
			r.Close()
			if e != nil {
				return e
			}
			target := string(b)
			resolved := filepath.Clean(filepath.Join(filepath.Dir(clean), target))
			if len(b) > 4096 || filepath.IsAbs(target) || strings.ContainsAny(target, "\\:") || resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
				return errors.New("unsafe browser archive symlink")
			}
			links[out] = target
			continue
		}
		if !entry.Mode().IsRegular() {
			r.Close()
			return errors.New("unsupported browser archive entry")
		}
		if err = os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			r.Close()
			return err
		}
		mode := os.FileMode(0600)
		if entry.Mode()&0111 != 0 {
			mode = 0700
		}
		f, e := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			r.Close()
			return e
		}
		_, e = io.Copy(f, io.LimitReader(r, int64(entry.UncompressedSize64)+1))
		ce := f.Close()
		r.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	// Links are last, so extraction never writes through archive-created links.
	for path, target := range links {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err = os.Symlink(target, path); err != nil {
			return err
		}
	}
	return nil
}

// AcquireInstallLock shares the existing OS file-lock implementation with the
// package installer. Keep the inode stable: process exit releases the lock.
func AcquireInstallLock(directory string) (*ProfileLock, error) {
	directory, err := CanonicalProfilePath(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return acquireProfileFileLock(filepath.Join(directory, ".digcrawl-install.lock"))
}
