package collect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Inbox processes URL drop files from the inbox directory.
type Inbox struct {
	Dir       string
	Processed string
	Failed    string
}

// NewInbox creates inbox paths under configured root.
func NewInbox(root string) *Inbox {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "collect-inbox"
	}
	return &Inbox{
		Dir:       root,
		Processed: filepath.Join(root, "processed"),
		Failed:    filepath.Join(root, "failed"),
	}
}

// EnsureDirs creates inbox directories.
func (b *Inbox) EnsureDirs() error {
	for _, d := range []string{b.Dir, b.Processed, b.Failed} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// PendingFiles lists .url / .txt files in the inbox root (non-recursive).
func (b *Inbox) PendingFiles() ([]string, error) {
	entries, err := os.ReadDir(b.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, b.EnsureDirs()
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".url") || strings.HasSuffix(name, ".txt") {
			files = append(files, filepath.Join(b.Dir, e.Name()))
		}
	}
	return files, nil
}

// ProcessOnce collects all pending inbox files.
func (b *Inbox) ProcessOnce(ctx context.Context, svc *Service, formats []string, outDir string) ([]ExportResult, error) {
	if err := b.EnsureDirs(); err != nil {
		return nil, err
	}
	files, err := b.PendingFiles()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	var results []ExportResult
	var firstErr error
	for _, f := range files {
		rs, err := svc.CollectFile(ctx, f, formats, outDir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", f, err)
			}
			_ = b.archiveTo(f, b.Failed)
			continue
		}
		results = append(results, rs...)
		_ = b.archiveTo(f, b.Processed)
	}
	if len(results) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return results, firstErr
}

func (b *Inbox) archiveTo(path, dir string) error {
	base := filepath.Base(path)
	dest := filepath.Join(dir, base)
	if _, err := os.Stat(dest); err == nil {
		dest = filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().Unix(), base))
	}
	return os.Rename(path, dest)
}

// Watch polls the inbox until ctx is cancelled.
func (b *Inbox) Watch(ctx context.Context, svc *Service, formats []string, outDir string, interval time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		_, _ = b.ProcessOnce(ctx, svc, formats, outDir)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
