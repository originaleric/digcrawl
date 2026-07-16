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
