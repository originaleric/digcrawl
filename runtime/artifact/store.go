package artifact

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/originaleric/digcrawl/protocol"
)

// DefaultInlineBytes is the default maximum artifact size returned as MCP content.
const DefaultInlineBytes int64 = 10 * 1024 * 1024

// InlineLimit returns the configured attachment limit, or the default when it
// is not configured.
func InlineLimit(configured int64) int64 {
	if configured <= 0 {
		return DefaultInlineBytes
	}
	return configured
}

// NewID returns a runtime-owned opaque artifact identifier. External call IDs
// are correlation data only and must not be reused as storage identities.
func NewID() string {
	return uuid.NewString()
}

// Store persists large tool outputs (screenshots, files).
type Store interface {
	Put(ctx context.Context, id, contentType, name string, data []byte) (protocol.Artifact, error)
	Get(ctx context.Context, id string) ([]byte, string, error)
}

// FileStore extends Store for large artifacts. Callers can persist and serve
// files without forcing a complete media payload into process memory.
type FileStore interface {
	Store
	PutFile(ctx context.Context, id, contentType, name, sourcePath string, maxBytes int64) (protocol.Artifact, error)
	Open(ctx context.Context, id string) (io.ReadCloser, string, error)
}

// DiskStore saves artifacts under a base directory.
type DiskStore struct {
	BaseDir      string
	TTL          time.Duration
	cleanupMu    sync.Mutex
	cleanupTimer *time.Timer
	cleanupAt    time.Time
}

func NewDiskStore(baseDir string, ttl time.Duration) (*DiskStore, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("artifact base dir is required")
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	store := &DiskStore{BaseDir: abs, TTL: ttl}
	store.scheduleCleanup(store.cleanupExpired(time.Now()))
	return store, nil
}

func (s *DiskStore) Put(_ context.Context, id, contentType, name string, data []byte) (protocol.Artifact, error) {
	var err error
	if id, err = writeID(id); err != nil {
		return protocol.Artifact{}, err
	}
	path := s.filePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return protocol.Artifact{}, err
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return protocol.Artifact{}, err
	}
	_ = os.WriteFile(s.metaPath(id), []byte(contentType), 0o640)
	s.scheduleCleanup(time.Now().Add(s.TTL))
	expires := time.Now().Add(s.TTL).Format(time.RFC3339)
	return protocol.Artifact{
		ID:        id,
		Type:      contentType,
		Name:      name,
		Size:      int64(len(data)),
		URI:       "digcrawl-artifact://" + id,
		ExpiresAt: expires,
	}, nil
}

// PutFile persists a regular local file with a caller-provided size limit.
// It is the large-media counterpart to Put and writes through a bounded copy.
func (s *DiskStore) PutFile(_ context.Context, id, contentType, name, sourcePath string, maxBytes int64) (protocol.Artifact, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return protocol.Artifact{}, err
	}
	if !info.Mode().IsRegular() {
		return protocol.Artifact{}, fmt.Errorf("artifact source is not a regular file")
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return protocol.Artifact{}, fmt.Errorf("artifact source exceeds max bytes %d", maxBytes)
	}
	if id, err = writeID(id); err != nil {
		return protocol.Artifact{}, err
	}
	path := s.filePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return protocol.Artifact{}, err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return protocol.Artifact{}, err
	}
	defer source.Close()
	target, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return protocol.Artifact{}, err
	}
	reader := io.Reader(source)
	if maxBytes > 0 {
		reader = io.LimitReader(source, maxBytes+1)
	}
	written, copyErr := io.Copy(target, reader)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil || (maxBytes > 0 && written > maxBytes) {
		_ = os.Remove(path)
		if copyErr != nil {
			return protocol.Artifact{}, copyErr
		}
		if closeErr != nil {
			return protocol.Artifact{}, closeErr
		}
		return protocol.Artifact{}, fmt.Errorf("artifact source exceeds max bytes %d", maxBytes)
	}
	_ = os.WriteFile(s.metaPath(id), []byte(contentType), 0o640)
	s.scheduleCleanup(time.Now().Add(s.TTL))
	expires := time.Now().Add(s.TTL).Format(time.RFC3339)
	return protocol.Artifact{ID: id, Type: contentType, Name: name, Size: written, URI: "digcrawl-artifact://" + id, ExpiresAt: expires}, nil
}

func (s *DiskStore) Get(ctx context.Context, id string) ([]byte, string, error) {
	reader, contentType, err := s.Open(ctx, id)
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	return data, contentType, err
}

// Open returns an artifact stream for HTTP delivery without buffering it in
// the transport process.
func (s *DiskStore) Open(_ context.Context, id string) (io.ReadCloser, string, error) {
	id = sanitizeID(id)
	path := s.filePath(id)
	if info, err := os.Stat(path); err != nil {
		return nil, "", err
	} else if s.TTL > 0 && time.Since(info.ModTime()) > s.TTL {
		_ = os.Remove(path)
		_ = os.Remove(s.metaPath(id))
		return nil, "", fmt.Errorf("artifact %q has expired", id)
	}
	metaPath := s.metaPath(id)
	contentType := "application/octet-stream"
	if b, err := os.ReadFile(metaPath); err == nil {
		contentType = strings.TrimSpace(string(b))
	}
	reader, err := os.Open(path)
	return reader, contentType, err
}

func (s *DiskStore) scheduleCleanup(at time.Time) {
	if s.TTL <= 0 || at.IsZero() {
		return
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if s.cleanupTimer != nil && !at.Before(s.cleanupAt) {
		return
	}
	if s.cleanupTimer != nil {
		s.cleanupTimer.Stop()
	}
	delay := time.Until(at)
	if delay < 0 {
		delay = 0
	}
	s.cleanupAt = at
	s.cleanupTimer = time.AfterFunc(delay, s.runScheduledCleanup)
}

func (s *DiskStore) runScheduledCleanup() {
	s.cleanupMu.Lock()
	s.cleanupTimer = nil
	s.cleanupAt = time.Time{}
	s.cleanupMu.Unlock()
	s.scheduleCleanup(s.cleanupExpired(time.Now()))
}

// cleanupExpired removes expired binary artifacts and their metadata. It
// returns the next expiration so cleanup remains inactive when the store is
// empty rather than leaving a permanent background loop behind.
func (s *DiskStore) cleanupExpired(now time.Time) time.Time {
	if s.TTL <= 0 {
		return time.Time{}
	}
	entries, err := os.ReadDir(s.BaseDir)
	if err != nil {
		return time.Time{}
	}
	var next time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".bin") {
			continue
		}
		path := filepath.Join(s.BaseDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		expiresAt := info.ModTime().Add(s.TTL)
		if !expiresAt.After(now) {
			_ = os.Remove(path)
			_ = os.Remove(strings.TrimSuffix(path, ".bin") + ".meta")
			continue
		}
		if next.IsZero() || expiresAt.Before(next) {
			next = expiresAt
		}
	}
	return next
}

func (s *DiskStore) filePath(id string) string {
	return filepath.Join(s.BaseDir, id+".bin")
}

func (s *DiskStore) metaPath(id string) string {
	return filepath.Join(s.BaseDir, id+".meta")
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "digcrawl-artifact://")
	return filepath.Base(id)
}

// writeID rejects path-like artifact IDs before any file operation. Reads
// accept a digcrawl-artifact URI for convenience, but writers must preserve a
// single opaque ID so every artifact remains beneath BaseDir.
func writeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return NewID(), nil
	}
	if id != filepath.Base(id) || id == "." || id == ".." {
		return "", fmt.Errorf("artifact id must not contain a path")
	}
	return id, nil
}

// PutBase64PNG decodes base64 PNG and stores it.
func PutBase64PNG(ctx context.Context, store Store, id, b64 string, maxInlineBytes int64) (protocol.Artifact, error) {
	if store == nil || b64 == "" {
		return protocol.Artifact{}, fmt.Errorf("empty artifact")
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return protocol.Artifact{}, err
	}
	limit := InlineLimit(maxInlineBytes)
	if int64(len(data)) > limit {
		return protocol.Artifact{}, fmt.Errorf("screenshot artifact exceeds %d byte limit", limit)
	}
	return store.Put(ctx, id, "image/png", "page.png", data)
}
