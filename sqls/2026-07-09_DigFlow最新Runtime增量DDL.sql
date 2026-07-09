-- DigFlow embedded runtime incremental MySQL DDL
-- For databases already initialized before DigFlow commit 8e77addb892c8f71ae24890733d5ca587ec168da.
-- Generated: 2026-07-09

SET NAMES utf8mb4;

-- DigFlow LoopRuntime durable ledger and active lease tables.
--
-- Phase2 introduces MySQL-backed LoopStore. Production MySQL environments
-- should create these tables explicitly instead of relying only on Runtime
-- AutoMigrate.

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS `df_loop_instance` (
  `id` varchar(191) NOT NULL,
  `agent_id` varchar(191) DEFAULT '',
  `loop_name` varchar(191) DEFAULT '',
  `goal` text,
  `status` varchar(64) DEFAULT '',
  `terminal_reason` varchar(191) DEFAULT '',
  `iteration_count` bigint DEFAULT 0,
  `token_budget` bigint DEFAULT 0,
  `tokens_used` bigint DEFAULT 0,
  `scope` varchar(191) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `completed_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_loop_instance_agent_status` (`agent_id`, `loop_name`, `status`),
  KEY `idx_loop_instance_scope` (`scope`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `df_loop_iteration` (
  `id` varchar(191) NOT NULL,
  `instance_id` varchar(191) DEFAULT '',
  `agent_id` varchar(191) DEFAULT '',
  `loop_name` varchar(191) DEFAULT '',
  `scope` varchar(191) DEFAULT '',
  `iteration` bigint DEFAULT 0,
  `status` varchar(64) DEFAULT '',
  `terminal_reason` varchar(191) DEFAULT '',
  `execution_id` varchar(191) DEFAULT '',
  `output` longtext,
  `output_hash` varchar(64) DEFAULT '',
  `output_truncated` tinyint(1) DEFAULT 0,
  `token_usage` text,
  `validation_results` text,
  `request_metadata` text,
  `error` text,
  `started_at` datetime(3) DEFAULT NULL,
  `completed_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_loop_iteration_instance_iteration` (`instance_id`, `iteration`),
  KEY `idx_loop_iteration_agent_id` (`agent_id`),
  KEY `idx_loop_iteration_scope` (`scope`),
  KEY `idx_loop_iteration_execution_id` (`execution_id`),
  KEY `idx_loop_iteration_output_hash` (`output_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `df_loop_active` (
  `active_key` varchar(512) NOT NULL,
  `agent_id` varchar(191) DEFAULT '',
  `loop_name` varchar(191) DEFAULT '',
  `scope` varchar(191) DEFAULT '',
  `owner_token` varchar(191) DEFAULT '',
  `lease_until` datetime(3) DEFAULT NULL,
  `acquired_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`active_key`),
  KEY `idx_loop_active_agent_id` (`agent_id`),
  KEY `idx_loop_active_owner_token` (`owner_token`),
  KEY `idx_loop_active_lease_until` (`lease_until`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


-- DigFlow ResourceAsset storage_key uniqueness migration.
--
-- Before adding the unique key, inspect and resolve historical duplicate
-- non-empty storage_key rows:
--
-- SELECT
--   storage_key,
--   COUNT(*) AS duplicate_count,
--   GROUP_CONCAT(id ORDER BY created_at, id) AS asset_ids
-- FROM df_resource_asset
-- WHERE storage_key IS NOT NULL AND TRIM(storage_key) <> ''
-- GROUP BY storage_key
-- HAVING COUNT(*) > 1;
--
-- Keep the asset whose blob is readable and update or delete duplicate rows
-- according to the host migration policy before running the CREATE UNIQUE INDEX
-- step. The migration intentionally fails if unresolved duplicates remain.

SET NAMES utf8mb4;

SET @df_resource_asset_storage_key_nullable_sql = (
  SELECT IF(
    EXISTS (
      SELECT 1
      FROM information_schema.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'df_resource_asset'
        AND COLUMN_NAME = 'storage_key'
    ),
    'ALTER TABLE `df_resource_asset` MODIFY COLUMN `storage_key` varchar(512) NULL',
    'SELECT 1'
  )
);
PREPARE df_resource_asset_storage_key_nullable_stmt FROM @df_resource_asset_storage_key_nullable_sql;
EXECUTE df_resource_asset_storage_key_nullable_stmt;
DEALLOCATE PREPARE df_resource_asset_storage_key_nullable_stmt;

SET @df_resource_asset_storage_key_blank_null_sql = (
  SELECT IF(
    EXISTS (
      SELECT 1
      FROM information_schema.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'df_resource_asset'
        AND COLUMN_NAME = 'storage_key'
    ),
    'UPDATE `df_resource_asset` SET `storage_key` = NULL WHERE `storage_key` IS NOT NULL AND TRIM(`storage_key`) = ''''',
    'SELECT 1'
  )
);
PREPARE df_resource_asset_storage_key_blank_null_stmt FROM @df_resource_asset_storage_key_blank_null_sql;
EXECUTE df_resource_asset_storage_key_blank_null_stmt;
DEALLOCATE PREPARE df_resource_asset_storage_key_blank_null_stmt;

SET @df_resource_asset_storage_key_unique_sql = (
  SELECT IF(
    EXISTS (
      SELECT 1
      FROM information_schema.TABLES
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'df_resource_asset'
    )
    AND NOT EXISTS (
      SELECT 1
      FROM information_schema.STATISTICS
      WHERE TABLE_SCHEMA = DATABASE()
        AND TABLE_NAME = 'df_resource_asset'
        AND INDEX_NAME = 'uidx_resource_asset_storage_key'
    ),
    'CREATE UNIQUE INDEX `uidx_resource_asset_storage_key` ON `df_resource_asset` (`storage_key`)',
    'SELECT 1'
  )
);
PREPARE df_resource_asset_storage_key_unique_stmt FROM @df_resource_asset_storage_key_unique_sql;
EXECUTE df_resource_asset_storage_key_unique_stmt;
DEALLOCATE PREPARE df_resource_asset_storage_key_unique_stmt;
