package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/google/uuid"
)

//go:embed migrate/001_init.sql
var migrationSQL string

// SQLiteStore implements Store backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// OpenSQLite opens or creates a collect SQLite database.
func OpenSQLite(path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	if _, err := s.db.Exec(migrationSQL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := s.migrateLegacyTables(); err != nil {
		return fmt.Errorf("migrate legacy tables: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateLegacyTables() error {
	legacyCollects, err := s.tableExists("collects")
	if err != nil {
		return err
	}
	if legacyCollects {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dc_collects (
  id, source_url, canonical_url, platform, tool_name, title, content_type,
  status, error_code, captured_at, output_root, base_name, created_at
) SELECT
  id, source_url, canonical_url, platform, tool_name, title, content_type,
  status, error_code, captured_at, output_root, base_name, created_at
FROM collects`); err != nil {
			return err
		}
	}
	legacyFiles, err := s.tableExists("collect_files")
	if err != nil {
		return err
	}
	if legacyFiles {
		if _, err := s.db.Exec(`INSERT INTO dc_collect_files (collect_id, kind, path)
SELECT f.collect_id, f.kind, f.path
FROM collect_files f
WHERE EXISTS (
  SELECT 1 FROM dc_collects c WHERE c.id = f.collect_id
)
AND NOT EXISTS (
  SELECT 1 FROM dc_collect_files d
  WHERE d.collect_id = f.collect_id
    AND d.kind = f.kind
    AND d.path = f.path
)`); err != nil {
			return err
		}
	}
	legacyMedia, err := s.tableExists("collect_media")
	if err != nil {
		return err
	}
	if legacyMedia {
		if _, err := s.db.Exec(`INSERT INTO dc_collect_media (
  collect_id, media_type, remote_url, local_path, download_error, sort_order
)
SELECT m.collect_id, m.media_type, m.remote_url, m.local_path, m.download_error, m.sort_order
FROM collect_media m
WHERE EXISTS (
  SELECT 1 FROM dc_collects c WHERE c.id = m.collect_id
)
AND NOT EXISTS (
  SELECT 1 FROM dc_collect_media d
  WHERE d.collect_id = m.collect_id
    AND d.media_type = m.media_type
    AND d.remote_url IS m.remote_url
    AND d.local_path IS m.local_path
    AND d.download_error IS m.download_error
    AND d.sort_order = m.sort_order
)`); err != nil {
			return err
		}
	}
	legacyToolRuns, err := s.tableExists("tool_runs")
	if err != nil {
		return err
	}
	if legacyToolRuns {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dc_tool_runs (id, tool_name, source_url, status, output_json, created_at)
SELECT id, tool_name, source_url, status, output_json, created_at FROM tool_runs`); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) tableExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	return n > 0, err
}

func (s *SQLiteStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) Upsert(ctx context.Context, in UpsertInput) (string, error) {
	if strings.TrimSpace(in.SourceURL) == "" {
		return "", fmt.Errorf("source_url is required")
	}
	in.SourceURL = normalizeURLKey(in.SourceURL)
	now := time.Now().UTC().Format(time.RFC3339)
	if in.CapturedAt == "" {
		in.CapturedAt = now
	}
	if in.Status == "" {
		in.Status = "success"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	id, err := findCollectID(ctx, tx, in.SourceURL)
	if err != nil {
		return "", err
	}
	if id == "" {
		id = uuid.NewString()
		_, err = tx.ExecContext(ctx, `
INSERT INTO dc_collects (
  id, source_url, canonical_url, platform, tool_name, title, content_type,
  status, error_code, captured_at, output_root, base_name, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, in.SourceURL, nullStr(in.CanonicalURL), in.Platform, in.ToolName,
			nullStr(in.Title), nullStr(in.ContentType), in.Status, nullStr(in.ErrorCode),
			in.CapturedAt, in.OutputRoot, nullStr(in.BaseName), now,
		)
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE dc_collects SET
  canonical_url = ?, platform = ?, tool_name = ?, title = ?, content_type = ?,
  status = ?, error_code = ?, captured_at = ?, output_root = ?, base_name = ?
WHERE id = ?`,
			nullStr(in.CanonicalURL), in.Platform, in.ToolName, nullStr(in.Title),
			nullStr(in.ContentType), in.Status, nullStr(in.ErrorCode), in.CapturedAt,
			in.OutputRoot, nullStr(in.BaseName), id,
		)
	}
	if err != nil {
		return "", err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM dc_collect_files WHERE collect_id = ?`, id); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM dc_collect_media WHERE collect_id = ?`, id); err != nil {
		return "", err
	}

	for _, f := range in.Files {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO dc_collect_files (collect_id, kind, path) VALUES (?, ?, ?)`,
			id, f.Kind, f.Path,
		); err != nil {
			return "", err
		}
	}
	for _, m := range in.Media {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO dc_collect_media (collect_id, media_type, remote_url, local_path, download_error, sort_order)
VALUES (?, ?, ?, ?, ?, ?)`,
			id, m.MediaType, nullStr(m.RemoteURL), nullStr(m.LocalPath),
			nullStr(m.DownloadError), m.SortOrder,
		); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func findCollectID(ctx context.Context, tx *sql.Tx, sourceURL string) (string, error) {
	key := normalizeURLKey(sourceURL)
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM dc_collects WHERE source_url = ?`, key).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *SQLiteStore) List(ctx context.Context, limit, offset int) ([]CollectRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, source_url, canonical_url, platform, tool_name, title, content_type,
       status, error_code, captured_at, output_root, base_name, created_at
FROM dc_collects
ORDER BY captured_at DESC
LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCollectRows(rows)
}

func (s *SQLiteStore) Search(ctx context.Context, query string, limit int) ([]CollectRecord, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return s.List(ctx, limit, 0)
	}
	if limit <= 0 {
		limit = 50
	}
	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
SELECT id, source_url, canonical_url, platform, tool_name, title, content_type,
       status, error_code, captured_at, output_root, base_name, created_at
FROM dc_collects
WHERE lower(source_url) LIKE ? OR lower(title) LIKE ? OR lower(platform) LIKE ?
ORDER BY captured_at DESC
LIMIT ?`, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCollectRows(rows)
}

func (s *SQLiteStore) GetByID(ctx context.Context, id string) (*CollectDetail, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, source_url, canonical_url, platform, tool_name, title, content_type,
       status, error_code, captured_at, output_root, base_name, created_at
FROM dc_collects WHERE id = ?`, id)
	rec, err := scanCollectRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.loadDetail(ctx, rec)
}

func (s *SQLiteStore) GetByURL(ctx context.Context, sourceURL string) (*CollectDetail, error) {
	key := normalizeURLKey(sourceURL)
	row := s.db.QueryRowContext(ctx, `
SELECT id, source_url, canonical_url, platform, tool_name, title, content_type,
       status, error_code, captured_at, output_root, base_name, created_at
FROM dc_collects WHERE source_url = ?`, key)
	rec, err := scanCollectRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.loadDetail(ctx, rec)
}

func (s *SQLiteStore) loadDetail(ctx context.Context, rec CollectRecord) (*CollectDetail, error) {
	detail := &CollectDetail{Collect: rec}

	fileRows, err := s.db.QueryContext(ctx,
		`SELECT kind, path FROM dc_collect_files WHERE collect_id = ? ORDER BY id`, rec.ID)
	if err != nil {
		return nil, err
	}
	defer fileRows.Close()
	for fileRows.Next() {
		var f FileRecord
		if err := fileRows.Scan(&f.Kind, &f.Path); err != nil {
			return nil, err
		}
		detail.Files = append(detail.Files, f)
	}
	if err := fileRows.Err(); err != nil {
		return nil, err
	}

	mediaRows, err := s.db.QueryContext(ctx, `
SELECT media_type, remote_url, local_path, download_error, sort_order
FROM dc_collect_media WHERE collect_id = ? ORDER BY sort_order, id`, rec.ID)
	if err != nil {
		return nil, err
	}
	defer mediaRows.Close()
	for mediaRows.Next() {
		var m MediaRecord
		var remote, local, dlErr sql.NullString
		if err := mediaRows.Scan(&m.MediaType, &remote, &local, &dlErr, &m.SortOrder); err != nil {
			return nil, err
		}
		m.RemoteURL = remote.String
		m.LocalPath = local.String
		m.DownloadError = dlErr.String
		detail.Media = append(detail.Media, m)
	}
	if err := mediaRows.Err(); err != nil {
		return nil, err
	}
	return detail, nil
}

func scanCollectRows(rows *sql.Rows) ([]CollectRecord, error) {
	var out []CollectRecord
	for rows.Next() {
		rec, err := scanCollectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanCollectRow(scanner interface {
	Scan(dest ...any) error
}) (CollectRecord, error) {
	var rec CollectRecord
	var canonical, title, contentType, errorCode, baseName sql.NullString
	err := scanner.Scan(
		&rec.ID, &rec.SourceURL, &canonical, &rec.Platform, &rec.ToolName,
		&title, &contentType, &rec.Status, &errorCode, &rec.CapturedAt,
		&rec.OutputRoot, &baseName, &rec.CreatedAt,
	)
	if err != nil {
		return CollectRecord{}, err
	}
	rec.CanonicalURL = canonical.String
	rec.Title = title.String
	rec.ContentType = contentType.String
	rec.ErrorCode = errorCode.String
	rec.BaseName = baseName.String
	return rec, nil
}

func normalizeURLKey(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.ToLower(raw)), "/")
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
