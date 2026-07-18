-- DigCrawl incremental MySQL DDL
-- Title: dc prefix MySQL initial schema
-- Date: 2026-07-09
-- Database: dig_crawl
-- Table prefix: dc_

CREATE DATABASE IF NOT EXISTS `dig_crawl`
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;

USE `dig_crawl`;

CREATE TABLE IF NOT EXISTS `dc_collects` (
  `id` varchar(64) NOT NULL,
  `source_url` text NOT NULL,
  `canonical_url` text NULL,
  `platform` varchar(64) NOT NULL,
  `tool_name` varchar(128) NOT NULL,
  `title` text NULL,
  `content_type` varchar(64) NULL,
  `status` varchar(64) NOT NULL,
  `error_code` varchar(128) NULL,
  `captured_at` varchar(64) NOT NULL,
  `output_root` text NOT NULL,
  `base_name` varchar(255) NULL,
  `created_at` varchar(64) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_dc_collects_source_url` (`source_url`(768))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_collect_files` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `collect_id` varchar(64) NOT NULL,
  `kind` varchar(64) NOT NULL,
  `path` text NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_dc_collect_files_collect_id` (`collect_id`),
  CONSTRAINT `fk_dc_collect_files_collect_id`
    FOREIGN KEY (`collect_id`) REFERENCES `dc_collects` (`id`)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_collect_media` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `collect_id` varchar(64) NOT NULL,
  `media_type` varchar(64) NOT NULL,
  `remote_url` text NULL,
  `local_path` text NULL,
  `download_error` text NULL,
  `sort_order` int DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_dc_collect_media_collect_id` (`collect_id`),
  CONSTRAINT `fk_dc_collect_media_collect_id`
    FOREIGN KEY (`collect_id`) REFERENCES `dc_collects` (`id`)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_tool_runs` (
  `id` varchar(64) NOT NULL,
  `tool_name` varchar(128) NOT NULL,
  `source_url` text NULL,
  `status` varchar(64) NOT NULL,
  `output_json` longtext NULL,
  `created_at` varchar(64) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_watchlist_locks` (
  `watch_id` varchar(191) NOT NULL,
  `owner` varchar(255) NOT NULL,
  `acquired_at` varchar(64) NOT NULL,
  `expires_at` varchar(64) NOT NULL,
  PRIMARY KEY (`watch_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_watchlist_runs` (
  `run_id` varchar(64) NOT NULL,
  `watch_id` varchar(191) NOT NULL,
  `owner` varchar(255) NOT NULL,
  `started_at` varchar(64) NOT NULL,
  `finished_at` varchar(64) NULL,
  `status` varchar(64) NOT NULL,
  `discovered_count` int DEFAULT 0,
  `new_count` int DEFAULT 0,
  `notified_count` int DEFAULT 0,
  `error` text NULL,
  PRIMARY KEY (`run_id`),
  KEY `idx_dc_watchlist_runs_watch_id` (`watch_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_watchlist_items` (
  `watch_id` varchar(191) NOT NULL,
  `content_key` varchar(512) NOT NULL,
  `platform_item_id` varchar(255) NULL,
  `url` text NOT NULL,
  `canonical_url` text NULL,
  `title` text NULL,
  `published_at` varchar(64) NULL,
  `content_type` varchar(64) NULL,
  `content_hash` varchar(128) NULL,
  `summary_hash` varchar(128) NULL,
  `first_seen_at` varchar(64) NOT NULL,
  `last_seen_at` varchar(64) NOT NULL,
  `baseline_only` tinyint DEFAULT 0,
  `status` varchar(64) NOT NULL,
  `collect_error` text NULL,
  `summary_error` text NULL,
  PRIMARY KEY (`watch_id`, `content_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `dc_watchlist_delivery` (
  `watch_id` varchar(191) NOT NULL,
  `content_key` varchar(512) NOT NULL,
  `channel_id` varchar(64) NOT NULL,
  `status` varchar(64) NOT NULL,
  `notified_at` varchar(64) NULL,
  `retry_count` int DEFAULT 0,
  `next_retry_at` varchar(64) NULL,
  `notify_error` text NULL,
  `message_title` text NULL,
  `message_text` longtext NULL,
  PRIMARY KEY (`watch_id`, `content_key`, `channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
