-- DigCrawl incremental DDL
-- Title: Watchlist 数据库调度与 FFmpeg 修复
-- Date: 2026-08-01
-- Purpose: Add schedule_at so dc_watchlist_sources can be the single source of truth for both interval and daily watchlist schedules.

-- MySQL DDL
-- Database: dig_crawl
-- Table prefix: dc_

USE `dig_crawl`;

SET @dc_watchlist_sources_schedule_at_exists := (
  SELECT COUNT(1)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'dc_watchlist_sources'
    AND column_name = 'schedule_at'
);

SET @dc_watchlist_sources_schedule_at_sql := IF(
  @dc_watchlist_sources_schedule_at_exists = 0,
  'ALTER TABLE `dc_watchlist_sources` ADD COLUMN `schedule_at` varchar(64) NULL AFTER `schedule_every_sec`',
  'SELECT 1'
);

PREPARE dc_watchlist_sources_schedule_at_stmt FROM @dc_watchlist_sources_schedule_at_sql;
EXECUTE dc_watchlist_sources_schedule_at_stmt;
DEALLOCATE PREPARE dc_watchlist_sources_schedule_at_stmt;

-- SQLite DDL
-- Use this section only when DigCrawl runs with local SQLite persistence.

-- SQLite does not support IF NOT EXISTS for ADD COLUMN on all deployed versions.
-- Check PRAGMA table_info(dc_watchlist_sources) first if the app has already
-- started once with this version.
ALTER TABLE dc_watchlist_sources
  ADD COLUMN schedule_at TEXT;
