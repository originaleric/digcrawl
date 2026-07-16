-- DigCrawl incremental MySQL DDL
-- Title: 通用 Task 调度 Runtime 表
-- Date: 2026-07-16
-- Database: dig_crawl
-- Table prefix: dc_

USE `dig_crawl`;

CREATE TABLE IF NOT EXISTS `dc_tasks_runtime` (
  `task_id` varchar(191) NOT NULL,
  `task_type` varchar(64) NOT NULL,
  `enabled` tinyint DEFAULT 1,
  `schedule_mode` varchar(64) NOT NULL,
  `schedule_every_sec` bigint DEFAULT 0,
  `source_platform` varchar(64) NULL,
  `source_kind` varchar(128) NULL,
  `source_url` text NULL,
  `notify_channel` varchar(64) NULL,
  `config_hash` varchar(128) NOT NULL,
  `last_run_id` varchar(64) NULL,
  `last_run_at` varchar(64) NULL,
  `next_run_at` varchar(64) NOT NULL,
  `last_status` varchar(64) NULL,
  `consecutive_failures` int DEFAULT 0,
  `updated_at` varchar(64) NOT NULL,
  PRIMARY KEY (`task_id`),
  KEY `idx_dc_tasks_runtime_next_run_at` (`next_run_at`),
  KEY `idx_dc_tasks_runtime_type_enabled` (`task_type`, `enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_task_runs` (
  `run_id` varchar(64) NOT NULL,
  `task_id` varchar(191) NOT NULL,
  `task_type` varchar(64) NOT NULL,
  `owner` varchar(255) NOT NULL,
  `started_at` varchar(64) NOT NULL,
  `finished_at` varchar(64) NULL,
  `status` varchar(64) NOT NULL,
  `error` text NULL,
  `metadata_json` longtext NULL,
  PRIMARY KEY (`run_id`),
  KEY `idx_dc_task_runs_task_id` (`task_id`),
  KEY `idx_dc_task_runs_started_at` (`started_at`),
  KEY `idx_dc_task_runs_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_task_locks` (
  `task_id` varchar(191) NOT NULL,
  `owner` varchar(255) NOT NULL,
  `acquired_at` varchar(64) NOT NULL,
  `expires_at` varchar(64) NOT NULL,
  PRIMARY KEY (`task_id`),
  KEY `idx_dc_task_locks_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_task_events` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_id` varchar(191) NOT NULL,
  `run_id` varchar(64) NULL,
  `event_type` varchar(64) NOT NULL,
  `status` varchar(64) NOT NULL,
  `message` text NULL,
  `payload_json` longtext NULL,
  `created_at` varchar(64) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_dc_task_events_task_id` (`task_id`),
  KEY `idx_dc_task_events_run_id` (`run_id`),
  KEY `idx_dc_task_events_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


-- SQLite DDL
-- Use this section when DigCrawl runs with local SQLite persistence.

CREATE TABLE IF NOT EXISTS dc_tasks_runtime (
  task_id              TEXT PRIMARY KEY,
  task_type            TEXT NOT NULL,
  enabled              INTEGER NOT NULL DEFAULT 1,
  schedule_mode        TEXT NOT NULL,
  schedule_every_sec   INTEGER NOT NULL DEFAULT 0,
  source_platform      TEXT,
  source_kind          TEXT,
  source_url           TEXT,
  notify_channel       TEXT,
  config_hash          TEXT NOT NULL,
  last_run_id          TEXT,
  last_run_at          TEXT,
  next_run_at          TEXT NOT NULL,
  last_status          TEXT,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  updated_at           TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dc_tasks_runtime_next_run_at
  ON dc_tasks_runtime(next_run_at);

CREATE INDEX IF NOT EXISTS idx_dc_tasks_runtime_type_enabled
  ON dc_tasks_runtime(task_type, enabled);

CREATE TABLE IF NOT EXISTS dc_task_runs (
  run_id        TEXT PRIMARY KEY,
  task_id       TEXT NOT NULL,
  task_type     TEXT NOT NULL,
  owner         TEXT NOT NULL,
  started_at    TEXT NOT NULL,
  finished_at   TEXT,
  status        TEXT NOT NULL,
  error         TEXT,
  metadata_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_dc_task_runs_task_id
  ON dc_task_runs(task_id);

CREATE INDEX IF NOT EXISTS idx_dc_task_runs_started_at
  ON dc_task_runs(started_at);

CREATE INDEX IF NOT EXISTS idx_dc_task_runs_status
  ON dc_task_runs(status);

CREATE TABLE IF NOT EXISTS dc_task_locks (
  task_id     TEXT PRIMARY KEY,
  owner       TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  expires_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dc_task_locks_expires_at
  ON dc_task_locks(expires_at);

CREATE TABLE IF NOT EXISTS dc_task_events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id      TEXT NOT NULL,
  run_id       TEXT,
  event_type   TEXT NOT NULL,
  status       TEXT NOT NULL,
  message      TEXT,
  payload_json TEXT,
  created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dc_task_events_task_id
  ON dc_task_events(task_id);

CREATE INDEX IF NOT EXISTS idx_dc_task_events_run_id
  ON dc_task_events(run_id);

CREATE INDEX IF NOT EXISTS idx_dc_task_events_created_at
  ON dc_task_events(created_at);
