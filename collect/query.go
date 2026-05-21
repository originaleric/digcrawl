package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/originaleric/diggate/collect/store"
	"github.com/originaleric/diggate/config"
)

// CollectDB returns the SQLite store when configured, opening on demand.
func CollectDB(cfg *config.Config) (store.Store, error) {
	if !cfg.Collect.UseSQLite() {
		return nil, fmt.Errorf("collect sqlite not enabled (PersistMode=%s)", cfg.Collect.PersistModeOrDefault())
	}
	return store.OpenSQLite(cfg.Collect.DBPathOrDefault())
}

// ListCollects queries SQLite for recent collects.
func ListCollects(ctx context.Context, cfg *config.Config, limit, offset int) ([]store.CollectRecord, error) {
	db, err := CollectDB(cfg)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.List(ctx, limit, offset)
}

// ShowCollect returns one collect by id or URL.
func ShowCollect(ctx context.Context, cfg *config.Config, idOrURL string) (*store.CollectDetail, error) {
	db, err := CollectDB(cfg)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	idOrURL = strings.TrimSpace(idOrURL)
	if strings.HasPrefix(idOrURL, "http://") || strings.HasPrefix(idOrURL, "https://") {
		return db.GetByURL(ctx, idOrURL)
	}
	return db.GetByID(ctx, idOrURL)
}

// SearchCollects searches SQLite by URL/title/platform substring.
func SearchCollects(ctx context.Context, cfg *config.Config, query string, limit int) ([]store.CollectRecord, error) {
	db, err := CollectDB(cfg)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.Search(ctx, query, limit)
}

// MigrateIndexToSQLite imports index.json entries into collect.db.
func MigrateIndexToSQLite(ctx context.Context, cfg *config.Config, outputRoot string) (int, error) {
	if outputRoot == "" {
		outputRoot = cfg.Collect.OutputDir
	}
	idx, err := LoadIndex(outputRoot)
	if err != nil {
		return 0, err
	}
	db, err := CollectDB(cfg)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	imported := 0
	for _, e := range idx.Entries {
		in := store.UpsertInput{
			SourceURL:  e.URL,
			Platform:   e.Platform,
			ToolName:   platformToolName(e.Platform),
			Title:      e.Title,
			Status:     "success",
			CapturedAt: e.CollectedAt,
			OutputRoot: outputRoot,
			BaseName:   e.BaseName,
		}
		if in.CapturedAt == "" {
			in.CapturedAt = time.Now().UTC().Format(time.RFC3339)
		}
		for kind, path := range e.Files {
			in.Files = append(in.Files, store.FileRecord{Kind: kind, Path: path})
		}
		if e.AssetsDir != "" {
			manifestPath := filepath.Join(e.AssetsDir, "media.json")
			if data, err := os.ReadFile(manifestPath); err == nil {
				in.Media = mediaFromIndexManifest(data, e.AssetsDir)
			}
		}
		if _, err := db.Upsert(ctx, in); err != nil {
			return imported, fmt.Errorf("import %q: %w", e.URL, err)
		}
		imported++
	}
	return imported, nil
}

func platformToolName(platform string) string {
	switch platform {
	case "wechat":
		return "wechat.article.read"
	case "xiaohongshu":
		return "xiaohongshu.note.read"
	case "douyin":
		return "douyin.video.read"
	case "x":
		return "x.post.read"
	default:
		return "browser.browse"
	}
}

func mediaFromIndexManifest(data []byte, assetsDir string) []store.MediaRecord {
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	raw, ok := manifest["downloaded"].([]any)
	if !ok {
		return nil
	}
	var downloaded []map[string]any
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			downloaded = append(downloaded, m)
		}
	}
	return mediaRecordsFromManifest(downloaded, assetsDir)
}
