package watchlist

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db      *sql.DB
	dialect string
}

type StoreOptions struct {
	Driver          string
	SQLitePath      string
	MySQLDSN        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func OpenStore(path string) (*Store, error) {
	return OpenStoreWithOptions(StoreOptions{Driver: "sqlite", SQLitePath: path})
}

func OpenStoreWithOptions(opts StoreOptions) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(opts.Driver))
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite", "sqlite3":
		return openSQLiteStore(opts)
	case "mysql":
		return openMySQLStore(opts)
	default:
		return nil, fmt.Errorf("unsupported watchlist persistence driver %q", opts.Driver)
	}
}

func openSQLiteStore(opts StoreOptions) (*Store, error) {
	path := opts.SQLitePath
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("watchlist db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	s := &Store{db: db, dialect: "sqlite"}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func openMySQLStore(opts StoreOptions) (*Store, error) {
	dsn := strings.TrimSpace(opts.MySQLDSN)
	if dsn == "" {
		return nil, fmt.Errorf("watchlist mysql dsn is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if opts.MaxOpenConns > 0 {
		db.SetMaxOpenConns(opts.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(10)
	}
	if opts.MaxIdleConns > 0 {
		db.SetMaxIdleConns(opts.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(2)
	}
	if opts.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(opts.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(30 * time.Minute)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect watchlist mysql: %w", err)
	}
	s := &Store{db: db, dialect: "mysql"}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) UpsertSource(ctx context.Context, src WatchSource) error {
	src.ID = strings.TrimSpace(src.ID)
	if src.ID == "" {
		return fmt.Errorf("watchlist source id is required")
	}
	src.Platform = strings.ToLower(strings.TrimSpace(src.Platform))
	src.Type = strings.ToLower(strings.TrimSpace(src.Type))
	src.URL = strings.TrimSpace(src.URL)
	if src.Platform == "" {
		return fmt.Errorf("watchlist source %q platform is required", src.ID)
	}
	if src.Type == "" {
		return fmt.Errorf("watchlist source %q kind is required", src.ID)
	}
	if src.URL == "" {
		return fmt.Errorf("watchlist source %q url is required", src.ID)
	}
	src.ScheduleMode = strings.ToLower(strings.TrimSpace(src.ScheduleMode))
	if src.ScheduleMode == "" {
		src.ScheduleMode = "interval"
	}
	src.ScheduleAt = strings.TrimSpace(src.ScheduleAt)
	if src.ScheduleMode != "interval" && src.ScheduleMode != "daily" {
		return fmt.Errorf("watchlist source %q unsupported schedule mode %q", src.ID, src.ScheduleMode)
	}
	if src.Interval <= 0 {
		if src.ScheduleMode == "daily" {
			src.Interval = 24 * time.Hour
		} else {
			src.Interval = 6 * time.Hour
		}
	}
	if err := ValidateSchedule(TaskDefinition{
		ID:            src.ID,
		ScheduleMode:  src.ScheduleMode,
		ScheduleEvery: src.Interval,
		ScheduleAt:    src.ScheduleAt,
	}); err != nil {
		return fmt.Errorf("watchlist source %q invalid schedule: %w", src.ID, err)
	}
	now := time.Now().UTC()
	if src.CreatedAt.IsZero() {
		src.CreatedAt = now
	}
	src.UpdatedAt = now
	opts, err := json.Marshal(src.Options)
	if err != nil {
		return fmt.Errorf("marshal watchlist source options: %w", err)
	}
	optionsJSON := string(opts)
	if src.Options == nil {
		optionsJSON = ""
	}
	if s.dialect == "mysql" {
		_, err = s.db.ExecContext(ctx, `INSERT INTO dc_watchlist_sources (
  source_id, name, platform, source_kind, source_url, enabled, schedule_mode, schedule_every_sec, schedule_at, notify_channel, options_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  name = VALUES(name),
  platform = VALUES(platform),
  source_kind = VALUES(source_kind),
  source_url = VALUES(source_url),
  enabled = VALUES(enabled),
  schedule_mode = VALUES(schedule_mode),
  schedule_every_sec = VALUES(schedule_every_sec),
  schedule_at = VALUES(schedule_at),
  notify_channel = VALUES(notify_channel),
  options_json = VALUES(options_json),
  updated_at = VALUES(updated_at)`,
			src.ID, nullStr(src.Name), src.Platform, src.Type, src.URL, boolInt(src.Enabled), src.ScheduleMode,
			int64(src.Interval.Seconds()), nullStr(src.ScheduleAt), nullStr(src.NotifyTo), nullStr(optionsJSON), src.CreatedAt.Format(time.RFC3339), src.UpdatedAt.Format(time.RFC3339))
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO dc_watchlist_sources (
  source_id, name, platform, source_kind, source_url, enabled, schedule_mode, schedule_every_sec, schedule_at, notify_channel, options_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_id) DO UPDATE SET
  name = excluded.name,
  platform = excluded.platform,
  source_kind = excluded.source_kind,
  source_url = excluded.source_url,
  enabled = excluded.enabled,
  schedule_mode = excluded.schedule_mode,
  schedule_every_sec = excluded.schedule_every_sec,
  schedule_at = excluded.schedule_at,
  notify_channel = excluded.notify_channel,
  options_json = excluded.options_json,
  updated_at = excluded.updated_at`,
		src.ID, nullStr(src.Name), src.Platform, src.Type, src.URL, boolInt(src.Enabled), src.ScheduleMode,
		int64(src.Interval.Seconds()), nullStr(src.ScheduleAt), nullStr(src.NotifyTo), nullStr(optionsJSON), src.CreatedAt.Format(time.RFC3339), src.UpdatedAt.Format(time.RFC3339))
	return err
}

func (s *Store) ListSources(ctx context.Context, enabledOnly bool) ([]WatchSource, error) {
	query := `SELECT source_id, COALESCE(name, ''), platform, source_kind, source_url, enabled, schedule_mode, schedule_every_sec, COALESCE(schedule_at, ''), COALESCE(notify_channel, ''), COALESCE(options_json, ''), created_at, updated_at
FROM dc_watchlist_sources`
	if enabledOnly {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY platform, source_kind, source_id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchSource
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) GetSource(ctx context.Context, id string) (WatchSource, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return WatchSource{}, fmt.Errorf("watchlist source id is required")
	}
	row := s.db.QueryRowContext(ctx, `SELECT source_id, COALESCE(name, ''), platform, source_kind, source_url, enabled, schedule_mode, schedule_every_sec, COALESCE(schedule_at, ''), COALESCE(notify_channel, ''), COALESCE(options_json, ''), created_at, updated_at
FROM dc_watchlist_sources WHERE source_id = ?`, id)
	src, err := scanSource(row)
	if err == sql.ErrNoRows {
		return WatchSource{}, fmt.Errorf("watchlist source %q not found", id)
	}
	return src, err
}

func (s *Store) SetSourceEnabled(ctx context.Context, id string, enabled bool) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("watchlist source id is required")
	}
	if ok, err := s.sourceExists(ctx, id); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("watchlist source %q not found", id)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE dc_watchlist_sources SET enabled = ?, updated_at = ? WHERE source_id = ?`,
		boolInt(enabled), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	_, _ = res.RowsAffected()
	return nil
}

func (s *Store) DeleteSource(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("watchlist source id is required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM dc_watchlist_sources WHERE source_id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return fmt.Errorf("watchlist source %q not found", id)
	}
	return nil
}

func (s *Store) sourceExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM dc_watchlist_sources WHERE source_id = ?`, id).Scan(&n)
	return n > 0, err
}

type sourceScanner interface {
	Scan(dest ...any) error
}

func scanSource(scanner sourceScanner) (WatchSource, error) {
	var src WatchSource
	var enabled int
	var everySec int64
	var optionsJSON, createdAt, updatedAt string
	if err := scanner.Scan(&src.ID, &src.Name, &src.Platform, &src.Type, &src.URL, &enabled, &src.ScheduleMode, &everySec, &src.ScheduleAt, &src.NotifyTo, &optionsJSON, &createdAt, &updatedAt); err != nil {
		return WatchSource{}, err
	}
	src.Enabled = enabled != 0
	src.Interval = time.Duration(everySec) * time.Second
	if strings.TrimSpace(optionsJSON) != "" {
		var opts map[string]any
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return WatchSource{}, fmt.Errorf("decode watchlist source %s options: %w", src.ID, err)
		}
		src.Options = opts
	}
	src.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	src.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return src, nil
}

func (s *Store) migrate() error {
	stmts := s.migrationStatements()
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing("dc_watchlist_delivery", "message_title", s.textColumnType()); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("dc_watchlist_delivery", "message_text", s.textColumnType()); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("dc_watchlist_delivery", "message_provider", s.shortTextColumnType()); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("dc_watchlist_sources", "schedule_at", s.shortTextColumnType()); err != nil {
		return err
	}
	if s.dialect == "sqlite" {
		if err := s.migrateLegacyTables(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) textColumnType() string {
	if s.dialect == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}

func (s *Store) shortTextColumnType() string {
	if s.dialect == "mysql" {
		return "VARCHAR(64)"
	}
	return "TEXT"
}

func (s *Store) migrationStatements() []string {
	if s.dialect == "mysql" {
		return mysqlMigrationStatements()
	}
	return sqliteMigrationStatements()
}

func sqliteMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS dc_watchlist_sources (
  source_id TEXT PRIMARY KEY,
  name TEXT,
  platform TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  source_url TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  schedule_mode TEXT NOT NULL DEFAULT 'interval',
  schedule_every_sec INTEGER NOT NULL DEFAULT 21600,
  schedule_at TEXT,
  notify_channel TEXT,
  options_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_watchlist_sources_enabled ON dc_watchlist_sources(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_watchlist_sources_platform_kind ON dc_watchlist_sources(platform, source_kind)`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_locks (
  watch_id TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_runs (
  run_id TEXT PRIMARY KEY,
  watch_id TEXT NOT NULL,
  owner TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  discovered_count INTEGER DEFAULT 0,
  new_count INTEGER DEFAULT 0,
  notified_count INTEGER DEFAULT 0,
  error TEXT
)`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_items (
  watch_id TEXT NOT NULL,
  content_key TEXT NOT NULL,
  platform_item_id TEXT,
  url TEXT NOT NULL,
  canonical_url TEXT,
  title TEXT,
  published_at TEXT,
  content_type TEXT,
  content_hash TEXT,
  summary_hash TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  baseline_only INTEGER DEFAULT 0,
  status TEXT NOT NULL,
  collect_error TEXT,
  summary_error TEXT,
  PRIMARY KEY (watch_id, content_key)
)`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_delivery (
  watch_id TEXT NOT NULL,
  content_key TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  status TEXT NOT NULL,
  notified_at TEXT,
  retry_count INTEGER DEFAULT 0,
	  next_retry_at TEXT,
	  notify_error TEXT,
	  message_title TEXT,
	  message_text TEXT,
	  message_provider TEXT,
	  PRIMARY KEY (watch_id, content_key, channel_id)
)`,
		`CREATE TABLE IF NOT EXISTS dc_tasks_runtime (
  task_id TEXT PRIMARY KEY,
  task_type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  schedule_mode TEXT NOT NULL,
  schedule_every_sec INTEGER NOT NULL DEFAULT 0,
  source_platform TEXT,
  source_kind TEXT,
  source_url TEXT,
  notify_channel TEXT,
  config_hash TEXT NOT NULL,
  last_run_id TEXT,
  last_run_at TEXT,
  next_run_at TEXT NOT NULL,
  last_status TEXT,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS dc_task_runs (
  run_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  task_type TEXT NOT NULL,
  owner TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  error TEXT,
  metadata_json TEXT
)`,
		`CREATE TABLE IF NOT EXISTS dc_task_locks (
  task_id TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS dc_task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  run_id TEXT,
  event_type TEXT NOT NULL,
  status TEXT NOT NULL,
  message TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_tasks_runtime_next_run_at ON dc_tasks_runtime(next_run_at)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_tasks_runtime_type_enabled ON dc_tasks_runtime(task_type, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_runs_task_id ON dc_task_runs(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_runs_started_at ON dc_task_runs(started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_runs_status ON dc_task_runs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_locks_expires_at ON dc_task_locks(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_events_task_id ON dc_task_events(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_events_run_id ON dc_task_events(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_dc_task_events_created_at ON dc_task_events(created_at)`,
	}
}

func mysqlMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS dc_watchlist_sources (
  source_id VARCHAR(191) PRIMARY KEY,
  name VARCHAR(255),
  platform VARCHAR(64) NOT NULL,
  source_kind VARCHAR(128) NOT NULL,
  source_url TEXT NOT NULL,
  enabled TINYINT NOT NULL DEFAULT 1,
  schedule_mode VARCHAR(64) NOT NULL DEFAULT 'interval',
  schedule_every_sec BIGINT NOT NULL DEFAULT 21600,
  schedule_at VARCHAR(64),
  notify_channel VARCHAR(191),
  options_json LONGTEXT,
  created_at VARCHAR(64) NOT NULL,
  updated_at VARCHAR(64) NOT NULL,
  KEY idx_dc_watchlist_sources_enabled (enabled),
  KEY idx_dc_watchlist_sources_platform_kind (platform, source_kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_locks (
  watch_id VARCHAR(191) PRIMARY KEY,
  owner VARCHAR(191) NOT NULL,
  acquired_at VARCHAR(64) NOT NULL,
  expires_at VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_runs (
  run_id VARCHAR(191) PRIMARY KEY,
  watch_id VARCHAR(191) NOT NULL,
  owner VARCHAR(191) NOT NULL,
  started_at VARCHAR(64) NOT NULL,
  finished_at VARCHAR(64),
  status VARCHAR(64) NOT NULL,
  discovered_count INT DEFAULT 0,
  new_count INT DEFAULT 0,
  notified_count INT DEFAULT 0,
  error TEXT,
  KEY idx_dc_watchlist_runs_watch_id (watch_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_items (
  watch_id VARCHAR(191) NOT NULL,
  content_key VARCHAR(512) NOT NULL,
  platform_item_id VARCHAR(191),
  url TEXT NOT NULL,
  canonical_url TEXT,
  title TEXT,
  published_at VARCHAR(64),
  content_type VARCHAR(64),
  content_hash VARCHAR(128),
  summary_hash VARCHAR(128),
  first_seen_at VARCHAR(64) NOT NULL,
  last_seen_at VARCHAR(64) NOT NULL,
  baseline_only TINYINT DEFAULT 0,
  status VARCHAR(64) NOT NULL,
  collect_error TEXT,
  summary_error TEXT,
  PRIMARY KEY (watch_id, content_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_watchlist_delivery (
  watch_id VARCHAR(191) NOT NULL,
  content_key VARCHAR(512) NOT NULL,
  channel_id VARCHAR(64) NOT NULL,
  status VARCHAR(64) NOT NULL,
  notified_at VARCHAR(64),
  retry_count INT DEFAULT 0,
  next_retry_at VARCHAR(64),
  notify_error TEXT,
  message_title TEXT,
  message_text LONGTEXT,
  message_provider VARCHAR(64),
  PRIMARY KEY (watch_id, content_key, channel_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_tasks_runtime (
  task_id VARCHAR(191) PRIMARY KEY,
  task_type VARCHAR(128) NOT NULL,
  enabled TINYINT NOT NULL DEFAULT 1,
  schedule_mode VARCHAR(64) NOT NULL,
  schedule_every_sec BIGINT NOT NULL DEFAULT 0,
  source_platform VARCHAR(64),
  source_kind VARCHAR(128),
  source_url TEXT,
  notify_channel VARCHAR(191),
  config_hash VARCHAR(128) NOT NULL,
  last_run_id VARCHAR(191),
  last_run_at VARCHAR(64),
  next_run_at VARCHAR(64) NOT NULL,
  last_status VARCHAR(64),
  consecutive_failures INT NOT NULL DEFAULT 0,
  updated_at VARCHAR(64) NOT NULL,
  KEY idx_dc_tasks_runtime_next_run_at (next_run_at),
  KEY idx_dc_tasks_runtime_type_enabled (task_type, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_task_runs (
  run_id VARCHAR(191) PRIMARY KEY,
  task_id VARCHAR(191) NOT NULL,
  task_type VARCHAR(128) NOT NULL,
  owner VARCHAR(191) NOT NULL,
  started_at VARCHAR(64) NOT NULL,
  finished_at VARCHAR(64),
  status VARCHAR(64) NOT NULL,
  error TEXT,
  metadata_json LONGTEXT,
  KEY idx_dc_task_runs_task_id (task_id),
  KEY idx_dc_task_runs_started_at (started_at),
  KEY idx_dc_task_runs_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_task_locks (
  task_id VARCHAR(191) PRIMARY KEY,
  owner VARCHAR(191) NOT NULL,
  acquired_at VARCHAR(64) NOT NULL,
  expires_at VARCHAR(64) NOT NULL,
  KEY idx_dc_task_locks_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS dc_task_events (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id VARCHAR(191) NOT NULL,
  run_id VARCHAR(191),
  event_type VARCHAR(128) NOT NULL,
  status VARCHAR(64) NOT NULL,
  message TEXT,
  payload_json LONGTEXT,
  created_at VARCHAR(64) NOT NULL,
  KEY idx_dc_task_events_task_id (task_id),
  KEY idx_dc_task_events_run_id (run_id),
  KEY idx_dc_task_events_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
}

func (s *Store) migrateLegacyTables() error {
	if ok, err := s.tableExists("watchlist_locks"); err != nil {
		return err
	} else if ok {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dc_watchlist_locks (watch_id, owner, acquired_at, expires_at)
SELECT watch_id, owner, acquired_at, expires_at FROM watchlist_locks`); err != nil {
			return err
		}
	}
	if ok, err := s.tableExists("watchlist_runs"); err != nil {
		return err
	} else if ok {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dc_watchlist_runs (
  run_id, watch_id, owner, started_at, finished_at, status, discovered_count, new_count, notified_count, error
) SELECT run_id, watch_id, owner, started_at, finished_at, status, discovered_count, new_count, notified_count, error
FROM watchlist_runs`); err != nil {
			return err
		}
	}
	if ok, err := s.tableExists("watchlist_items"); err != nil {
		return err
	} else if ok {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dc_watchlist_items (
  watch_id, content_key, platform_item_id, url, canonical_url, title, published_at, content_type,
  content_hash, summary_hash, first_seen_at, last_seen_at, baseline_only, status, collect_error, summary_error
) SELECT
  watch_id, content_key, platform_item_id, url, canonical_url, title, published_at, content_type,
  content_hash, summary_hash, first_seen_at, last_seen_at, baseline_only, status, collect_error, summary_error
FROM watchlist_items`); err != nil {
			return err
		}
	}
	if ok, err := s.tableExists("watchlist_delivery"); err != nil {
		return err
	} else if ok {
		if err := s.addColumnIfMissing("watchlist_delivery", "message_title", "TEXT"); err != nil {
			return err
		}
		if err := s.addColumnIfMissing("watchlist_delivery", "message_text", "TEXT"); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dc_watchlist_delivery (
  watch_id, content_key, channel_id, status, notified_at, retry_count, next_retry_at, notify_error, message_title, message_text, message_provider
) SELECT watch_id, content_key, channel_id, status, notified_at, retry_count, next_retry_at, notify_error, message_title, message_text, ''
FROM watchlist_delivery`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) tableExists(name string) (bool, error) {
	var n int
	if s.dialect == "mysql" {
		err := s.db.QueryRow(`SELECT COUNT(1) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, name).Scan(&n)
		return n > 0, err
	}
	err := s.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	return n > 0, err
}

func (s *Store) addColumnIfMissing(table, column, typ string) error {
	if s.dialect == "mysql" {
		var n int
		err := s.db.QueryRow(`SELECT COUNT(1) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`, table, column).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + typ)
		return err
	}
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + typ)
	return err
}

func (s *Store) AcquireLock(ctx context.Context, watchID, owner string, ttl time.Duration) (bool, error) {
	now := time.Now().UTC()
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var expires string
	err = tx.QueryRowContext(ctx, `SELECT expires_at FROM dc_watchlist_locks WHERE watch_id = ?`, watchID).Scan(&expires)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil {
		t, parseErr := time.Parse(time.RFC3339, expires)
		if parseErr == nil && t.After(now) {
			return false, tx.Commit()
		}
		_, err = tx.ExecContext(ctx, `UPDATE dc_watchlist_locks SET owner = ?, acquired_at = ?, expires_at = ? WHERE watch_id = ?`,
			owner, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339), watchID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO dc_watchlist_locks (watch_id, owner, acquired_at, expires_at) VALUES (?, ?, ?, ?)`,
			watchID, owner, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) ReleaseLock(ctx context.Context, watchID, owner string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM dc_watchlist_locks WHERE watch_id = ? AND owner = ?`, watchID, owner)
}

func (s *Store) StartRun(ctx context.Context, watchID, owner string) (string, error) {
	runID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO dc_watchlist_runs (run_id, watch_id, owner, started_at, status) VALUES (?, ?, ?, ?, ?)`,
		runID, watchID, owner, now, "running")
	return runID, err
}

func (s *Store) FinishRun(ctx context.Context, runID string, summary RunSummary, status string, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dc_watchlist_runs SET finished_at = ?, status = ?, discovered_count = ?, new_count = ?, notified_count = ?, error = ? WHERE run_id = ?`,
		time.Now().UTC().Format(time.RFC3339), status, summary.Discovered, summary.NewItems, summary.Notified, nullStr(errText), runID)
	return err
}

func (s *Store) UpsertTaskDefinition(ctx context.Context, def TaskDefinition, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := ValidateSchedule(def); err != nil {
		return err
	}
	nowText := now.UTC().Format(time.RFC3339)
	nextText := def.InitialNextRunAt(now).Format(time.RFC3339)
	if s.dialect == "mysql" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO dc_tasks_runtime (
  task_id, task_type, enabled, schedule_mode, schedule_every_sec, source_platform, source_kind, source_url,
  notify_channel, config_hash, next_run_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  task_type = VALUES(task_type),
  enabled = VALUES(enabled),
  schedule_mode = VALUES(schedule_mode),
  schedule_every_sec = VALUES(schedule_every_sec),
  source_platform = VALUES(source_platform),
  source_kind = VALUES(source_kind),
  source_url = VALUES(source_url),
  notify_channel = VALUES(notify_channel),
  next_run_at = IF(config_hash != VALUES(config_hash), VALUES(next_run_at), next_run_at),
  config_hash = VALUES(config_hash),
  updated_at = VALUES(updated_at)`,
			def.ID, def.Type, boolInt(def.Enabled), def.ScheduleMode, int64(def.ScheduleEvery.Seconds()),
			nullStr(def.SourcePlatform), nullStr(def.SourceKind), nullStr(def.SourceURL), nullStr(def.NotifyChannel),
			def.ConfigHash, nextText, nowText)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dc_tasks_runtime (
  task_id, task_type, enabled, schedule_mode, schedule_every_sec, source_platform, source_kind, source_url,
  notify_channel, config_hash, next_run_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
  task_type = excluded.task_type,
  enabled = excluded.enabled,
  schedule_mode = excluded.schedule_mode,
  schedule_every_sec = excluded.schedule_every_sec,
  source_platform = excluded.source_platform,
  source_kind = excluded.source_kind,
  source_url = excluded.source_url,
  notify_channel = excluded.notify_channel,
  next_run_at = CASE WHEN config_hash != excluded.config_hash THEN excluded.next_run_at ELSE next_run_at END,
  config_hash = excluded.config_hash,
  updated_at = excluded.updated_at`,
		def.ID, def.Type, boolInt(def.Enabled), def.ScheduleMode, int64(def.ScheduleEvery.Seconds()),
		nullStr(def.SourcePlatform), nullStr(def.SourceKind), nullStr(def.SourceURL), nullStr(def.NotifyChannel),
		def.ConfigHash, nextText, nowText)
	return err
}

func (s *Store) IsTaskDue(ctx context.Context, taskID string, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var next string
	err := s.db.QueryRowContext(ctx, `SELECT next_run_at FROM dc_tasks_runtime WHERE task_id = ?`, taskID).Scan(&next)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	t, err := time.Parse(time.RFC3339, next)
	if err != nil {
		return false, err
	}
	return !t.After(now.UTC()), nil
}

func (s *Store) DisableMissingTaskDefinitions(ctx context.Context, taskType string, activeIDs []string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	updated := now.UTC().Format(time.RFC3339)
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return nil
	}
	if len(activeIDs) == 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE dc_tasks_runtime SET enabled = 0, updated_at = ? WHERE task_type = ?`, updated, taskType)
		return err
	}
	placeholders := make([]string, 0, len(activeIDs))
	args := []any{updated, taskType}
	for _, id := range activeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(placeholders) == 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE dc_tasks_runtime SET enabled = 0, updated_at = ? WHERE task_type = ?`, updated, taskType)
		return err
	}
	query := `UPDATE dc_tasks_runtime SET enabled = 0, updated_at = ? WHERE task_type = ? AND task_id NOT IN (` + strings.Join(placeholders, ",") + `)`
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) AcquireTaskLock(ctx context.Context, taskID, owner string, ttl time.Duration) (bool, error) {
	now := time.Now().UTC()
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var expires string
	err = tx.QueryRowContext(ctx, `SELECT expires_at FROM dc_task_locks WHERE task_id = ?`, taskID).Scan(&expires)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil {
		t, parseErr := time.Parse(time.RFC3339, expires)
		if parseErr == nil && t.After(now) {
			return false, tx.Commit()
		}
		_, err = tx.ExecContext(ctx, `UPDATE dc_task_locks SET owner = ?, acquired_at = ?, expires_at = ? WHERE task_id = ?`,
			owner, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339), taskID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO dc_task_locks (task_id, owner, acquired_at, expires_at) VALUES (?, ?, ?, ?)`,
			taskID, owner, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) ReleaseTaskLock(ctx context.Context, taskID, owner string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM dc_task_locks WHERE task_id = ? AND owner = ?`, taskID, owner)
}

func (s *Store) StartTaskRun(ctx context.Context, def TaskDefinition, owner string) (string, error) {
	runID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO dc_task_runs (run_id, task_id, task_type, owner, started_at, status) VALUES (?, ?, ?, ?, ?, ?)`,
		runID, def.ID, def.Type, owner, now, "running")
	if err != nil {
		return "", err
	}
	_ = s.InsertTaskEvent(ctx, def.ID, runID, "run_started", "running", "", "")
	return runID, nil
}

func (s *Store) FinishTaskRun(ctx context.Context, runID string, def TaskDefinition, status, errText, metadataJSON string, finishedAt time.Time) error {
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	if err := ValidateSchedule(def); err != nil {
		return err
	}
	nextRunAt := def.NextRunAfter(finishedAt)
	finishedText := finishedAt.UTC().Format(time.RFC3339)
	nextText := nextRunAt.Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `UPDATE dc_task_runs SET finished_at = ?, status = ?, error = ?, metadata_json = ? WHERE run_id = ?`,
		finishedText, status, nullStr(errText), nullStr(metadataJSON), runID)
	if err != nil {
		return err
	}
	failDelta := 0
	if status == "success" || status == "skipped_locked" {
		failDelta = -1
	}
	if failDelta < 0 {
		_, err = tx.ExecContext(ctx, `UPDATE dc_tasks_runtime SET last_run_id = ?, last_run_at = ?, next_run_at = ?, last_status = ?, consecutive_failures = 0, updated_at = ? WHERE task_id = ?`,
			runID, finishedText, nextText, status, finishedText, def.ID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE dc_tasks_runtime SET last_run_id = ?, last_run_at = ?, next_run_at = ?, last_status = ?, consecutive_failures = consecutive_failures + 1, updated_at = ? WHERE task_id = ?`,
			runID, finishedText, nextText, status, finishedText, def.ID)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	_ = s.InsertTaskEvent(context.Background(), def.ID, runID, "run_finished", status, errText, metadataJSON)
	return nil
}

func (s *Store) InsertTaskEvent(ctx context.Context, taskID, runID, eventType, status, message, payloadJSON string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO dc_task_events (task_id, run_id, event_type, status, message, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, nullStr(runID), eventType, status, nullStr(message), nullStr(payloadJSON), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) UpsertDiscovered(ctx context.Context, watchID string, item FeedItem, baseline bool) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	key := contentKey(item)
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT content_key FROM dc_watchlist_items WHERE watch_id = ? AND content_key = ?`, watchID, key).Scan(&existing)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE dc_watchlist_items SET
  last_seen_at = ?,
  title = COALESCE(?, title),
  url = ?,
  canonical_url = COALESCE(?, canonical_url),
  published_at = COALESCE(?, published_at),
  content_type = COALESCE(?, content_type)
WHERE watch_id = ? AND content_key = ?`,
			now, nullStr(item.Title), item.URL, nullStr(item.CanonicalURL), timeStr(item.PublishedAt), nullStr(item.ContentType), watchID, key)
		return false, err
	}
	status := StatusDiscovered
	if baseline {
		status = StatusSkippedBaseline
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO dc_watchlist_items (
  watch_id, content_key, platform_item_id, url, canonical_url, title, published_at, content_type,
  first_seen_at, last_seen_at, baseline_only, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		watchID, key, nullStr(item.ID), item.URL, nullStr(item.CanonicalURL), nullStr(item.Title),
		timeStr(item.PublishedAt), nullStr(item.ContentType), now, now, boolInt(baseline), status)
	return !baseline, err
}

func (s *Store) UpdateItemStatus(ctx context.Context, watchID, contentKey, status, collectErr, summaryErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dc_watchlist_items SET status = ?, collect_error = ?, summary_error = ? WHERE watch_id = ? AND content_key = ?`,
		status, nullStr(collectErr), nullStr(summaryErr), watchID, contentKey)
	return err
}

func (s *Store) UpdateItemCollectedMetadata(ctx context.Context, watchID, contentKey, title, publishedAt, contentType string) error {
	title = strings.TrimSpace(title)
	publishedAt = strings.TrimSpace(publishedAt)
	contentType = strings.TrimSpace(contentType)
	if title == "" && publishedAt == "" && contentType == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE dc_watchlist_items SET
  title = COALESCE(NULLIF(?, ''), title),
  published_at = COALESCE(NULLIF(?, ''), published_at),
  content_type = COALESCE(NULLIF(?, ''), content_type)
WHERE watch_id = ? AND content_key = ?`,
		title, publishedAt, contentType, watchID, contentKey)
	return err
}

func (s *Store) ItemStatus(ctx context.Context, watchID, key string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM dc_watchlist_items WHERE watch_id = ? AND content_key = ?`, watchID, key).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return status, err
}

type PendingDelivery struct {
	WatchID         string
	ContentKey      string
	ChannelID       string
	Title           string
	Text            string
	ContentType     string
	MessageProvider string
}

func (s *Store) UpsertDelivery(ctx context.Context, watchID, contentKey, channelID, status, errText string, retryable bool, messageTitle, messageText string) error {
	return s.UpsertDeliveryWithProvider(ctx, watchID, contentKey, channelID, status, errText, retryable, messageTitle, messageText, "")
}

func (s *Store) UpsertDeliveryWithProvider(ctx context.Context, watchID, contentKey, channelID, status, errText string, retryable bool, messageTitle, messageText, messageProvider string) error {
	now := time.Now().UTC()
	var notified any
	if status == StatusNotified || status == "notified" {
		notified = now.Format(time.RFC3339)
		status = StatusNotified
	}
	nextRetry := ""
	if retryable {
		nextRetry = now.Add(5 * time.Minute).Format(time.RFC3339)
	} else if status == StatusNotifyFailed {
		status = StatusNotifyFinalFail
	}
	if s.dialect == "mysql" {
		_, err := s.db.ExecContext(ctx, `INSERT INTO dc_watchlist_delivery (
  watch_id, content_key, channel_id, status, notified_at, retry_count, next_retry_at, notify_error, message_title, message_text, message_provider
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  status = VALUES(status),
  notified_at = COALESCE(VALUES(notified_at), notified_at),
  retry_count = retry_count + CASE WHEN VALUES(status) = ? THEN 1 ELSE 0 END,
  next_retry_at = VALUES(next_retry_at),
  notify_error = VALUES(notify_error),
  message_title = COALESCE(VALUES(message_title), message_title),
  message_text = COALESCE(VALUES(message_text), message_text),
  message_provider = COALESCE(VALUES(message_provider), message_provider)`,
			watchID, contentKey, channelID, status, notified, 0, nullStr(nextRetry), nullStr(errText), nullStr(messageTitle), nullStr(messageText), nullStr(messageProvider), StatusNotifyFailed)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dc_watchlist_delivery (
  watch_id, content_key, channel_id, status, notified_at, retry_count, next_retry_at, notify_error, message_title, message_text, message_provider
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(watch_id, content_key, channel_id) DO UPDATE SET
  status = excluded.status,
  notified_at = COALESCE(excluded.notified_at, dc_watchlist_delivery.notified_at),
  retry_count = dc_watchlist_delivery.retry_count + CASE WHEN excluded.status = ? THEN 1 ELSE 0 END,
  next_retry_at = excluded.next_retry_at,
  notify_error = excluded.notify_error,
  message_title = COALESCE(excluded.message_title, dc_watchlist_delivery.message_title),
  message_text = COALESCE(excluded.message_text, dc_watchlist_delivery.message_text),
  message_provider = COALESCE(excluded.message_provider, dc_watchlist_delivery.message_provider)`,
		watchID, contentKey, channelID, status, notified, 0, nullStr(nextRetry), nullStr(errText), nullStr(messageTitle), nullStr(messageText), nullStr(messageProvider), StatusNotifyFailed)
	return err
}

func (s *Store) ListPendingDeliveries(ctx context.Context, watchID string, limit int) ([]PendingDelivery, error) {
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
SELECT d.watch_id, d.content_key, d.channel_id, COALESCE(d.message_title, ''), COALESCE(d.message_text, ''),
  COALESCE(i.content_type, ''), COALESCE(d.message_provider, '')
FROM dc_watchlist_delivery d
LEFT JOIN dc_watchlist_items i ON i.watch_id = d.watch_id AND i.content_key = d.content_key
WHERE d.watch_id = ? AND d.status = ? AND d.next_retry_at IS NOT NULL AND d.next_retry_at != '' AND d.next_retry_at <= ?
ORDER BY d.next_retry_at, d.content_key
LIMIT ?`, watchID, StatusNotifyFailed, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingDelivery
	for rows.Next() {
		var d PendingDelivery
		if err := rows.Scan(&d.WatchID, &d.ContentKey, &d.ChannelID, &d.Title, &d.Text, &d.ContentType, &d.MessageProvider); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) HasNotifiedContentOnDate(ctx context.Context, watchID string, day time.Time) (bool, error) {
	if day.IsZero() {
		day = time.Now()
	}
	localStart := time.Date(day.In(time.Local).Year(), day.In(time.Local).Month(), day.In(time.Local).Day(), 0, 0, 0, 0, time.Local)
	start := localStart.UTC().Format(time.RFC3339)
	end := localStart.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1)
FROM dc_watchlist_delivery
WHERE watch_id = ?
  AND status = ?
  AND notified_at >= ?
  AND notified_at < ?
  AND content_key NOT LIKE '__no_update__:%'`,
		watchID, StatusNotified, start, end).Scan(&n)
	return n > 0, err
}

func (s *Store) HasAnyItem(ctx context.Context, watchID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM dc_watchlist_items WHERE watch_id = ?`, watchID).Scan(&n)
	return n > 0, err
}

func contentKey(item FeedItem) string {
	for _, v := range []string{item.ContentKey, item.ID, item.CanonicalURL, item.URL} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return uuid.NewString()
}

func ContentKey(item FeedItem) string {
	return contentKey(item)
}

func nullStr(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func timeStr(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
