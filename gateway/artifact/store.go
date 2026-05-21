package artifact

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/originaleric/diggate/gateway/protocol"
)

// Store persists large tool outputs (screenshots, files).
type Store interface {
	Put(ctx context.Context, id, contentType, name string, data []byte) (protocol.Artifact, error)
	Get(ctx context.Context, id string) ([]byte, string, error)
}

// DiskStore saves artifacts under a base directory.
type DiskStore struct {
	BaseDir string
	TTL     time.Duration
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
	return &DiskStore{BaseDir: abs, TTL: ttl}, nil
}

func (s *DiskStore) Put(_ context.Context, id, contentType, name string, data []byte) (protocol.Artifact, error) {
	if id == "" {
		id = uuid.NewString()
	}
	path := s.filePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return protocol.Artifact{}, err
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return protocol.Artifact{}, err
	}
	_ = os.WriteFile(s.metaPath(id), []byte(contentType), 0o640)
	expires := time.Now().Add(s.TTL).Format(time.RFC3339)
	return protocol.Artifact{
		ID:        id,
		Type:      contentType,
		Name:      name,
		Size:      int64(len(data)),
		URI:       "diggate-artifact://" + id,
		ExpiresAt: expires,
	}, nil
}

func (s *DiskStore) Get(_ context.Context, id string) ([]byte, string, error) {
	id = sanitizeID(id)
	metaPath := s.metaPath(id)
	contentType := "application/octet-stream"
	if b, err := os.ReadFile(metaPath); err == nil {
		contentType = strings.TrimSpace(string(b))
	}
	data, err := os.ReadFile(s.filePath(id))
	return data, contentType, err
}

func (s *DiskStore) filePath(id string) string {
	return filepath.Join(s.BaseDir, id+".bin")
}

func (s *DiskStore) metaPath(id string) string {
	return filepath.Join(s.BaseDir, id+".meta")
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "diggate-artifact://")
	return filepath.Base(id)
}

// PutBase64PNG decodes base64 PNG and stores it.
func PutBase64PNG(ctx context.Context, store Store, id, b64 string) (protocol.Artifact, error) {
	if store == nil || b64 == "" {
		return protocol.Artifact{}, fmt.Errorf("empty artifact")
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return protocol.Artifact{}, err
	}
	return store.Put(ctx, id, "image/png", "page.png", data)
}
