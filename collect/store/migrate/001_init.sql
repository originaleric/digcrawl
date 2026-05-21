CREATE TABLE IF NOT EXISTS collects (
  id            TEXT PRIMARY KEY,
  source_url    TEXT NOT NULL,
  canonical_url TEXT,
  platform      TEXT NOT NULL,
  tool_name     TEXT NOT NULL,
  title         TEXT,
  content_type  TEXT,
  status        TEXT NOT NULL,
  error_code    TEXT,
  captured_at   TEXT NOT NULL,
  output_root   TEXT NOT NULL,
  base_name     TEXT,
  created_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_collects_source_url ON collects(source_url);

CREATE TABLE IF NOT EXISTS collect_files (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  collect_id  TEXT NOT NULL REFERENCES collects(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  path        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS collect_media (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  collect_id     TEXT NOT NULL REFERENCES collects(id) ON DELETE CASCADE,
  media_type     TEXT NOT NULL,
  remote_url     TEXT,
  local_path     TEXT,
  download_error TEXT,
  sort_order     INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tool_runs (
  id          TEXT PRIMARY KEY,
  tool_name   TEXT NOT NULL,
  source_url  TEXT,
  status      TEXT NOT NULL,
  output_json TEXT,
  created_at  TEXT NOT NULL
);
