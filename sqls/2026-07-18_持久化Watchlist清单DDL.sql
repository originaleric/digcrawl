-- DigCrawl incremental DDL
-- Title: 持久化 Watchlist 清单表
-- Date: 2026-07-18
-- Purpose: Move long watchlist source lists out of YAML and into the configured persistence backend.

-- MySQL DDL
-- Database: dig_crawl
-- Table prefix: dc_

USE `dig_crawl`;

CREATE TABLE IF NOT EXISTS `dc_watchlist_sources` (
  `source_id` varchar(191) NOT NULL,
  `name` varchar(255) NULL,
  `platform` varchar(64) NOT NULL,
  `source_kind` varchar(128) NOT NULL,
  `source_url` text NOT NULL,
  `enabled` tinyint NOT NULL DEFAULT 1,
  `schedule_mode` varchar(64) NOT NULL DEFAULT 'interval',
  `schedule_every_sec` bigint NOT NULL DEFAULT 21600,
  `notify_channel` varchar(191) NULL,
  `options_json` longtext NULL,
  `created_at` varchar(64) NOT NULL,
  `updated_at` varchar(64) NOT NULL,
  PRIMARY KEY (`source_id`),
  KEY `idx_dc_watchlist_sources_enabled` (`enabled`),
  KEY `idx_dc_watchlist_sources_platform_kind` (`platform`, `source_kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE `dc_watchlist_locks`
  MODIFY `watch_id` varchar(191) NOT NULL;

ALTER TABLE `dc_watchlist_runs`
  MODIFY `watch_id` varchar(191) NOT NULL;

ALTER TABLE `dc_watchlist_items`
  MODIFY `watch_id` varchar(191) NOT NULL;

ALTER TABLE `dc_watchlist_delivery`
  MODIFY `watch_id` varchar(191) NOT NULL;

-- SQLite DDL
-- Use this section when DigCrawl runs with local SQLite persistence.

CREATE TABLE IF NOT EXISTS dc_watchlist_sources (
  source_id          TEXT PRIMARY KEY,
  name               TEXT,
  platform           TEXT NOT NULL,
  source_kind        TEXT NOT NULL,
  source_url         TEXT NOT NULL,
  enabled            INTEGER NOT NULL DEFAULT 1,
  schedule_mode      TEXT NOT NULL DEFAULT 'interval',
  schedule_every_sec INTEGER NOT NULL DEFAULT 21600,
  notify_channel     TEXT,
  options_json       TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dc_watchlist_sources_enabled
  ON dc_watchlist_sources(enabled);

CREATE INDEX IF NOT EXISTS idx_dc_watchlist_sources_platform_kind
  ON dc_watchlist_sources(platform, source_kind);
