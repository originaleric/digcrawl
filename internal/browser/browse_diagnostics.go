package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/originaleric/digcrawl/runtime/version"
)

var diagnosticMu sync.Mutex

func diagnosticPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "digcrawl", "bridge")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "browser-diagnostics.jsonl"), nil
}
func recordBrowseDiagnostic(ctx context.Context, err error) {
	d := BrowseDiagnostics(ctx)
	if d == nil {
		return
	}
	d["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	d["build_id"] = version.BuildIdentity()
	d["status"] = "success"
	if err != nil {
		d["status"] = "error"
	}
	var browseErr *BrowseError
	if errors.As(err, &browseErr) {
		d["status"] = "error"
		d["code"] = browseErr.Code
		d["stage"] = browseErr.Stage
		d["reason"] = browseCauseReason(browseErr.Cause)
		if reason, ok := browseErr.Diagnostics["reason"].(string); ok {
			d["reason"] = reason
		}
	}
	writeBrowserDiagnostic(d)
}
func recordBrowserStage(ctx context.Context, operation string, conn *browserConnection, duration time.Duration, err error) {
	d := BrowseDiagnostics(ctx)
	if d == nil {
		d = map[string]any{"diagnostic_id": "manager"}
	}
	d["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	d["build_id"] = version.BuildIdentity()
	d["operation"] = operation
	d["duration_ms"] = duration.Milliseconds()
	d["reason"] = browseCauseReason(err)
	if conn != nil {
		d["connection_id"] = fmt.Sprintf("%p", conn)
		d["owned"] = conn.owned
	}
	writeBrowserDiagnostic(d)
}
func writeBrowserDiagnostic(d map[string]any) {
	// Deliberately omit URL, request arguments, raw errors, cookies and paths.
	data, marshalErr := json.Marshal(d)
	if marshalErr != nil {
		return
	}
	diagnosticMu.Lock()
	defer diagnosticMu.Unlock()
	path, pathErr := diagnosticPath()
	if pathErr != nil {
		return
	}
	lock, lockErr := acquireDiagnosticFileLock(path)
	if lockErr != nil {
		return
	}
	defer lock.Close()
	if info, e := os.Stat(path); e == nil && info.Size() > 1<<20 {
		_ = os.Remove(path + ".2")
		_ = os.Rename(path+".1", path+".2")
		_ = os.Rename(path, path+".1")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	if f.Chmod(0600) != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// DiagnosticSummary reads only bounded safe records, never arbitrary files.
func DiagnosticSummary(id string) ([]map[string]any, error) {
	if len(id) != 24 {
		return nil, fmt.Errorf("invalid diagnostic ID")
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return nil, fmt.Errorf("invalid diagnostic ID")
		}
	}
	diagnosticMu.Lock()
	defer diagnosticMu.Unlock()
	path, err := diagnosticPath()
	if err != nil {
		return nil, err
	}
	lock, err := acquireDiagnosticFileLock(path)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	var out []map[string]any
	for _, name := range []string{path + ".2", path + ".1", path} {
		f, e := os.Open(name)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		scanner := bufio.NewScanner(io.LimitReader(f, (1<<20)+(64<<10)))
		for scanner.Scan() {
			var d map[string]any
			if json.Unmarshal(scanner.Bytes(), &d) == nil && d["diagnostic_id"] == id {
				out = append(out, d)
			}
		}
		e = scanner.Err()
		_ = f.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

// Several dedicated profiles can log simultaneously. Reuse the existing OS
// file-lock helper to serialize rotation, with a small best-effort wait.
func acquireDiagnosticFileLock(path string) (*ProfileLock, error) {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		lock, err := acquireProfileFileLock(path + ".lock")
		if !errors.Is(err, ErrProfileInUse) || time.Now().After(deadline) {
			return lock, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
