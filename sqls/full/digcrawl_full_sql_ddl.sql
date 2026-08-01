-- DigCrawl full MySQL DDL
-- Database: dig_crawl
-- Table prefixes: dc_ for DigCrawl, df_ for embedded DigFlow runtime
-- Generated: 2026-07-16
-- DigFlow source commit: 8e77addb892c8f71ae24890733d5ca587ec168da

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

CREATE TABLE IF NOT EXISTS `dc_watchlist_sources` (
  `source_id` varchar(191) NOT NULL,
  `name` varchar(255) NULL,
  `platform` varchar(64) NOT NULL,
  `source_kind` varchar(128) NOT NULL,
  `source_url` text NOT NULL,
  `enabled` tinyint NOT NULL DEFAULT 1,
  `schedule_mode` varchar(64) NOT NULL DEFAULT 'interval',
  `schedule_every_sec` bigint NOT NULL DEFAULT 21600,
  `schedule_at` varchar(64) NULL,
  `notify_channel` varchar(191) NULL,
  `options_json` longtext NULL,
  `created_at` varchar(64) NOT NULL,
  `updated_at` varchar(64) NOT NULL,
  PRIMARY KEY (`source_id`),
  KEY `idx_dc_watchlist_sources_enabled` (`enabled`),
  KEY `idx_dc_watchlist_sources_platform_kind` (`platform`, `source_kind`)
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
  `message_provider` varchar(64) NULL,
  PRIMARY KEY (`watch_id`, `content_key`, `channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


-- DigFlow embedded runtime tables (df_)

-- Upstream embedded schema
-- 2026-06-10 宿主使用 DigFlow Embedded Runtime 的默认数据表 SQL
-- 范围：宿主通过 pkg/digflow.NewRuntime 使用默认 MySQL store / 默认 domain 表名时可能需要的表。
-- 不包含 DigFlow 自身 App 扩展表（如 df_brain_generated_agents、df_sql_dev）和治理学习扩展表。

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- Checkpoint / GraphState
CREATE TABLE IF NOT EXISTS `df_check_points` (
  `checkpoint_id` varchar(255) NOT NULL,
  `data` longblob,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`checkpoint_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Eino checkpoint 状态表';

CREATE TABLE IF NOT EXISTS `df_execution_state` (
  `execution_id` varchar(255) NOT NULL,
  `app_name` varchar(100) NOT NULL,
  `session_id` varchar(255) NOT NULL DEFAULT '',
  `execution_mode` varchar(20) DEFAULT 'auto',
  `paused_at_node` varchar(100) DEFAULT NULL,
  `checkpoint_nodes` json DEFAULT NULL,
  `graph_state_messages` text,
  `graph_state_data` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`execution_id`),
  KEY `idx_execution_state_app_name` (`app_name`),
  KEY `idx_execution_state_session_id` (`session_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow GraphState 快照表';

-- Optional status stream when host enables DigEino status persistence
CREATE TABLE IF NOT EXISTS `df_execution` (
  `execution_id` varchar(128) NOT NULL,
  `app_name` varchar(100) NOT NULL,
  `session_id` varchar(128) DEFAULT '',
  `status` varchar(32) DEFAULT 'running',
  `created_at` timestamp DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`execution_id`),
  KEY `idx_app_session` (`app_name`,`session_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 执行主表';

CREATE TABLE IF NOT EXISTS `df_execution_status` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `execution_id` varchar(128) NOT NULL,
  `type` varchar(50) NOT NULL,
  `timestamp` bigint NOT NULL,
  `node_key` varchar(100) DEFAULT NULL,
  `node_type` varchar(50) DEFAULT NULL,
  `status` varchar(32) NOT NULL,
  `error` text,
  `data_flow` json DEFAULT NULL,
  `control_flow` json DEFAULT NULL,
  `app_name` varchar(100) DEFAULT NULL,
  `request_id` varchar(128) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_exec_id` (`execution_id`),
  KEY `idx_node` (`node_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 执行事件流水表';

-- Resource / File RAG default tables
CREATE TABLE IF NOT EXISTS `df_resource_asset` (
  `id` varchar(191) NOT NULL,
  `owner_type` varchar(64) DEFAULT '',
  `owner_id` varchar(191) DEFAULT '',
  `sha256` varchar(64) DEFAULT '',
  `file_name` varchar(512) DEFAULT '',
  `mime_type` varchar(255) DEFAULT '',
  `file_size` bigint DEFAULT 0,
  `storage_key` varchar(512) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_resource_asset_owner_sha` (`owner_type`,`owner_id`,`sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 文件物理资产表';

CREATE TABLE IF NOT EXISTS `df_resource_file` (
  `id` varchar(191) NOT NULL,
  `asset_id` varchar(191) DEFAULT '',
  `owner_type` varchar(64) DEFAULT '',
  `owner_id` varchar(191) DEFAULT '',
  `workspace_id` varchar(191) DEFAULT '',
  `sha256` varchar(64) DEFAULT '',
  `file_name` varchar(512) DEFAULT '',
  `mime_type` varchar(255) DEFAULT '',
  `file_size` bigint DEFAULT 0,
  `storage_key` varchar(512) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `source` varchar(64) DEFAULT '',
  `visibility` varchar(64) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_resource_file_asset_id` (`asset_id`),
  KEY `idx_resource_file_workspace_id` (`workspace_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 逻辑资源文件表';

CREATE TABLE IF NOT EXISTS `df_resource_parse_result` (
  `id` varchar(191) NOT NULL,
  `asset_id` varchar(191) DEFAULT '',
  `parser` varchar(128) DEFAULT '',
  `parser_version` varchar(64) DEFAULT '',
  `signature` varchar(512) DEFAULT '',
  `signature_hash` varchar(64) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `text_digest` varchar(64) DEFAULT '',
  `chunk_count` bigint DEFAULT 0,
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_resource_parse_asset_signature` (`asset_id`,`signature_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 资源解析结果缓存表';

CREATE TABLE IF NOT EXISTS `df_resource_chunk` (
  `id` varchar(191) NOT NULL,
  `asset_id` varchar(191) DEFAULT '',
  `resource_file_id` varchar(191) DEFAULT '',
  `workspace_id` varchar(191) DEFAULT '',
  `parse_result_id` varchar(191) DEFAULT '',
  `parser_signature_hash` varchar(64) DEFAULT '',
  `chunk_index` bigint DEFAULT 0,
  `content` longtext,
  `token_count` bigint DEFAULT 0,
  `index_status` varchar(64) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_resource_chunk_asset_id` (`asset_id`),
  KEY `idx_resource_chunk_resource_file_id` (`resource_file_id`),
  KEY `idx_resource_chunk_workspace_id` (`workspace_id`),
  KEY `idx_resource_chunk_parse_result_id` (`parse_result_id`),
  KEY `idx_resource_chunk_parser_signature_hash` (`parser_signature_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 资源 chunk 表';

CREATE TABLE IF NOT EXISTS `df_resource_ingestion_pipeline` (
  `id` varchar(191) NOT NULL,
  `resource_file_id` varchar(191) DEFAULT '',
  `asset_id` varchar(191) DEFAULT '',
  `workspace_id` varchar(191) DEFAULT '',
  `owner_type` varchar(64) DEFAULT '',
  `owner_id` varchar(191) DEFAULT '',
  `parse_profile_signature_hash` varchar(191) DEFAULT '',
  `index_provider_signature` varchar(191) DEFAULT '',
  `idempotency_key` varchar(191) DEFAULT '',
  `active_key` varchar(191) DEFAULT NULL,
  `status` varchar(64) DEFAULT '',
  `current_stage` varchar(64) DEFAULT '',
  `asset_job_id` varchar(191) DEFAULT '',
  `file_index_job_id` varchar(191) DEFAULT '',
  `cancel_requested_at` datetime(3) DEFAULT NULL,
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `finished_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uidx_resource_pipeline_active_key` (`active_key`),
  KEY `idx_resource_ingestion_pipeline_resource_file_id` (`resource_file_id`),
  KEY `idx_resource_ingestion_pipeline_asset_id` (`asset_id`),
  KEY `idx_resource_pipeline_workspace_status` (`workspace_id`, `status`),
  KEY `idx_resource_ingestion_pipeline_idempotency_key` (`idempotency_key`),
  KEY `idx_resource_pipeline_asset_job_status` (`asset_job_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 异步资源摄取 pipeline 表';

CREATE TABLE IF NOT EXISTS `df_resource_ingestion_job` (
  `id` varchar(191) NOT NULL,
  `pipeline_id` varchar(191) DEFAULT '',
  `resource_file_id` varchar(191) DEFAULT '',
  `trigger_resource_file_id` varchar(191) DEFAULT '',
  `asset_id` varchar(191) DEFAULT '',
  `workspace_id` varchar(191) DEFAULT '',
  `owner_type` varchar(64) DEFAULT '',
  `owner_id` varchar(191) DEFAULT '',
  `job_kind` varchar(64) DEFAULT '',
  `job_scope_type` varchar(64) DEFAULT '',
  `job_scope_id` varchar(191) DEFAULT '',
  `parser_signature_hash` varchar(191) DEFAULT '',
  `idempotency_key` varchar(191) DEFAULT '',
  `active_key` varchar(191) DEFAULT NULL,
  `depends_on_job_id` varchar(191) DEFAULT '',
  `depends_on_scope_key` varchar(191) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `stage` varchar(64) DEFAULT '',
  `priority` bigint DEFAULT 0,
  `attempt_count` bigint DEFAULT 0,
  `max_attempts` bigint DEFAULT 0,
  `lease_owner` varchar(191) DEFAULT '',
  `lease_until` datetime(3) DEFAULT NULL,
  `locked_at` datetime(3) DEFAULT NULL,
  `cancel_requested_at` datetime(3) DEFAULT NULL,
  `pages_total` bigint DEFAULT 0,
  `pages_done` bigint DEFAULT 0,
  `chunks_total` bigint DEFAULT 0,
  `chunks_done` bigint DEFAULT 0,
  `indexed_chunks` bigint DEFAULT 0,
  `failed_chunks` bigint DEFAULT 0,
  `last_error` text,
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `started_at` datetime(3) DEFAULT NULL,
  `finished_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uidx_resource_ingestion_active_key` (`active_key`),
  KEY `idx_resource_ingestion_job_pipeline_id` (`pipeline_id`),
  KEY `idx_resource_ingestion_job_resource_file_id` (`resource_file_id`),
  KEY `idx_resource_ingestion_job_asset_id` (`asset_id`),
  KEY `idx_resource_ingestion_job_idempotency_key` (`idempotency_key`),
  KEY `idx_resource_ingestion_job_depends_on_job_id` (`depends_on_job_id`),
  KEY `idx_resource_ingestion_workspace_status` (`workspace_id`, `status`),
  KEY `idx_resource_ingestion_status_priority` (`status`, `priority`, `lease_until`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 异步资源摄取 job 表';

CREATE TABLE IF NOT EXISTS `df_resource_retrieval_session` (
  `id` varchar(191) NOT NULL,
  `scope_type` varchar(64) DEFAULT '',
  `scope_id` varchar(191) DEFAULT '',
  `query` text NOT NULL,
  `top_k` bigint DEFAULT 0,
  `strategy` varchar(64) DEFAULT '',
  `filters` text,
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_resource_retrieval_scope_type` (`scope_type`),
  KEY `idx_resource_retrieval_scope_id` (`scope_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 资源检索会话审计表';

CREATE TABLE IF NOT EXISTS `df_resource_retrieval_hit` (
  `id` varchar(191) NOT NULL,
  `retrieval_id` varchar(191) NOT NULL,
  `resource_file_id` varchar(191) NOT NULL,
  `chunk_id` varchar(191) NOT NULL,
  `score` double DEFAULT 0,
  `snippet` longtext,
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_resource_retrieval_hit_retrieval_id` (`retrieval_id`),
  KEY `idx_resource_retrieval_hit_resource_file_id` (`resource_file_id`),
  KEY `idx_resource_retrieval_hit_chunk_id` (`chunk_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 资源检索命中审计表';

-- Workflow default tables
CREATE TABLE IF NOT EXISTS `df_workflow_instance` (
  `id` varchar(191) NOT NULL,
  `agent_id` varchar(191) DEFAULT '',
  `title` varchar(512) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `current_node` varchar(191) DEFAULT '',
  `created_by` varchar(191) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_workflow_instance_agent_id` (`agent_id`),
  KEY `idx_workflow_instance_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow instance 表';

CREATE TABLE IF NOT EXISTS `df_workflow_node_state` (
  `instance_id` varchar(191) NOT NULL,
  `node_key` varchar(191) NOT NULL,
  `status` varchar(64) DEFAULT '',
  `run_id` varchar(191) DEFAULT '',
  `execution_id` varchar(191) DEFAULT '',
  `chat_session_id` varchar(191) DEFAULT '',
  `latest_draft_artifact_id` varchar(191) DEFAULT '',
  `confirmed_artifact_id` varchar(191) DEFAULT '',
  `stale` tinyint(1) DEFAULT 0,
  `error_message` text,
  `metadata` text,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`instance_id`,`node_key`),
  KEY `idx_workflow_node_state_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 节点状态表';

CREATE TABLE IF NOT EXISTS `df_workflow_node_attachment` (
  `id` varchar(191) NOT NULL,
  `instance_id` varchar(191) DEFAULT '',
  `node_key` varchar(191) DEFAULT '',
  `file_id` varchar(191) DEFAULT '',
  `resource_file_id` varchar(191) DEFAULT '',
  `file_name` varchar(512) DEFAULT '',
  `mime_type` varchar(255) DEFAULT '',
  `file_size` bigint DEFAULT 0,
  `role` varchar(64) DEFAULT '',
  `source` varchar(64) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_workflow_attachment_node` (`instance_id`,`node_key`),
  KEY `idx_workflow_attachment_resource_file_id` (`resource_file_id`),
  UNIQUE KEY `uidx_workflow_attachment_resource_role` (`instance_id`,`node_key`,`resource_file_id`,`role`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 节点附件表';

CREATE TABLE IF NOT EXISTS `df_workflow_node_chat_session` (
  `session_id` varchar(191) NOT NULL,
  `instance_id` varchar(191) NOT NULL,
  `node_key` varchar(191) NOT NULL,
  `agent_id` varchar(191) DEFAULT '',
  `user_id` varchar(191) DEFAULT '',
  `app_name` varchar(191) DEFAULT '',
  `title` varchar(512) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`session_id`,`instance_id`,`node_key`),
  KEY `idx_workflow_node_chat_session_node` (`instance_id`,`node_key`),
  KEY `idx_workflow_node_chat_session_agent_id` (`agent_id`),
  KEY `idx_workflow_node_chat_session_user_id` (`user_id`),
  KEY `idx_workflow_node_chat_session_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 节点对话会话表';

CREATE TABLE IF NOT EXISTS `df_workflow_node_chat_run` (
  `run_id` varchar(191) NOT NULL,
  `instance_id` varchar(191) NOT NULL,
  `node_key` varchar(191) NOT NULL,
  `chat_session_id` varchar(191) NOT NULL,
  `app_name` varchar(191) DEFAULT '',
  `user_id` varchar(191) DEFAULT '',
  `status` varchar(64) NOT NULL,
  `cancel_reason` varchar(191) DEFAULT '',
  `partial_content` longtext,
  `partial_reasoning` longtext,
  `error_message` text,
  `metadata` text,
  `started_at` datetime(3) DEFAULT NULL,
  `cancel_requested_at` datetime(3) DEFAULT NULL,
  `ended_at` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`run_id`),
  KEY `idx_df_workflow_node_chat_run_scope` (`instance_id`,`node_key`,`chat_session_id`),
  KEY `idx_df_workflow_node_chat_run_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 节点对话生成 run 表';

CREATE TABLE IF NOT EXISTS `df_workflow_artifact` (
  `id` varchar(191) NOT NULL,
  `instance_id` varchar(191) DEFAULT '',
  `node_key` varchar(191) DEFAULT '',
  `visibility` varchar(32) NOT NULL DEFAULT '',
  `node_path` varchar(255) NOT NULL DEFAULT '',
  `parent_node_key` varchar(191) NOT NULL DEFAULT '',
  `subgraph_scope_id` varchar(191) NOT NULL DEFAULT '',
  `subgraph_invocation_id` varchar(191) NOT NULL DEFAULT '',
  `owner_scope_type` varchar(64) NOT NULL DEFAULT '',
  `owner_scope_id` varchar(191) NOT NULL DEFAULT '',
  `type` varchar(64) DEFAULT '',
  `format` varchar(64) DEFAULT '',
  `title` varchar(512) DEFAULT '',
  `content` longtext,
  `file_id` varchar(191) DEFAULT '',
  `download_url` varchar(1024) DEFAULT '',
  `version` bigint DEFAULT 0,
  `status` varchar(64) DEFAULT '',
  `source_session_id` varchar(191) DEFAULT '',
  `source_message_id` varchar(191) DEFAULT '',
  `upstream_artifact_ids` text,
  `idempotency_key` varchar(191) DEFAULT '',
  `payload_hash` varchar(128) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_workflow_artifact_instance_node` (`instance_id`,`node_key`),
  KEY `idx_workflow_artifact_scope_visibility` (`instance_id`,`owner_scope_type`,`owner_scope_id`,`visibility`),
  KEY `idx_workflow_artifact_parent_scope` (`instance_id`,`parent_node_key`,`subgraph_scope_id`),
  KEY `idx_workflow_artifact_visibility_node` (`instance_id`,`visibility`,`node_key`),
  KEY `idx_workflow_artifact_status` (`status`),
  KEY `idx_workflow_artifact_node_version` (`instance_id`,`node_key`,`version`),
  UNIQUE KEY `uk_workflow_artifact_scope_version` (`instance_id`,`owner_scope_type`,`owner_scope_id`,`node_path`,`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 产物表';

CREATE TABLE IF NOT EXISTS `df_workflow_artifact_idempotency` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `scope_hash` varchar(64) NOT NULL,
  `scope` text,
  `idempotency_key` varchar(191) NOT NULL,
  `artifact_id` varchar(191) NOT NULL DEFAULT '',
  `payload_hash` varchar(128) NOT NULL DEFAULT '',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_workflow_artifact_idempotency` (`scope_hash`,`idempotency_key`),
  KEY `idx_workflow_artifact_idempotency_artifact` (`artifact_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow artifact 幂等记录表';

CREATE TABLE IF NOT EXISTS `df_workflow_artifact_edge` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `instance_id` varchar(191) DEFAULT '',
  `from_node_key` varchar(191) DEFAULT '',
  `to_node_key` varchar(191) DEFAULT '',
  `artifact_id` varchar(191) DEFAULT '',
  `handoff_status` varchar(64) DEFAULT '',
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_workflow_artifact_edge_instance_id` (`instance_id`),
  KEY `idx_workflow_artifact_edge_artifact_id` (`artifact_id`),
  UNIQUE KEY `uk_workflow_artifact_edge_once` (`instance_id`,`from_node_key`,`to_node_key`,`artifact_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 产物流转边表';

CREATE TABLE IF NOT EXISTS `df_workflow_subagent_run` (
  `id` varchar(191) NOT NULL,
  `instance_id` varchar(191) DEFAULT '',
  `parent_node_key` varchar(191) DEFAULT '',
  `agent_id` varchar(191) DEFAULT '',
  `run_id` varchar(191) DEFAULT '',
  `execution_id` varchar(191) DEFAULT '',
  `status` varchar(64) DEFAULT '',
  `input_artifact_id` varchar(191) DEFAULT '',
  `output_artifact_id` varchar(191) DEFAULT '',
  `error_message` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_workflow_subagent_run_instance_id` (`instance_id`),
  KEY `idx_workflow_subagent_run_parent_node_key` (`parent_node_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 子 Agent 运行表';

CREATE TABLE IF NOT EXISTS `df_workflow_event` (
  `seq` bigint unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(191) DEFAULT '',
  `instance_id` varchar(191) DEFAULT '',
  `node_key` varchar(191) DEFAULT '',
  `type` varchar(64) DEFAULT '',
  `artifact_id` varchar(191) DEFAULT '',
  `run_id` varchar(191) DEFAULT '',
  `execution_id` varchar(191) DEFAULT '',
  `payload` longtext,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`seq`),
  UNIQUE KEY `idx_workflow_event_id` (`id`),
  KEY `idx_workflow_event_instance_seq` (`instance_id`,`seq`),
  KEY `idx_workflow_event_node_key` (`node_key`),
  KEY `idx_workflow_event_type` (`type`),
  KEY `idx_workflow_event_artifact_id` (`artifact_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 事件表';

CREATE TABLE IF NOT EXISTS `df_context_event` (
  `id` varchar(191) NOT NULL,
  `app_name` varchar(191) NOT NULL DEFAULT '',
  `session_id` varchar(191) NOT NULL DEFAULT '',
  `user_id` varchar(191) NOT NULL DEFAULT '',
  `scope_type` varchar(64) NOT NULL DEFAULT '',
  `scope_id` varchar(191) NOT NULL DEFAULT '',
  `context_scope_id` varchar(191) NOT NULL DEFAULT '',
  `workflow_instance_id` varchar(191) NOT NULL DEFAULT '',
  `node_key` varchar(191) NOT NULL DEFAULT '',
  `parent_node_key` varchar(191) NOT NULL DEFAULT '',
  `subgraph_scope_id` varchar(191) NOT NULL DEFAULT '',
  `subgraph_invocation_id` varchar(191) NOT NULL DEFAULT '',
  `subgraph_node_path` varchar(255) NOT NULL DEFAULT '',
  `event_type` varchar(64) NOT NULL DEFAULT '',
  `title` varchar(255) NOT NULL DEFAULT '',
  `summary_snapshot` longtext,
  `messages_before` int NOT NULL DEFAULT 0,
  `messages_kept` int NOT NULL DEFAULT 0,
  `summary_hash` varchar(64) NOT NULL DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_context_event_session_time` (`app_name`,`session_id`,`created_at`),
  KEY `idx_context_event_scope_time` (`scope_type`,`scope_id`,`created_at`),
  KEY `idx_context_event_context_scope_time` (`context_scope_id`,`created_at`),
  KEY `idx_context_event_workflow_node` (`workflow_instance_id`,`node_key`,`created_at`),
  KEY `idx_context_event_parent_subgraph_time` (`parent_node_key`,`subgraph_scope_id`,`created_at`),
  KEY `idx_context_event_subgraph_scope_time` (`subgraph_scope_id`,`created_at`),
  KEY `idx_context_event_subgraph_invocation_time` (`subgraph_invocation_id`,`created_at`),
  KEY `idx_context_event_summary_hash` (`app_name`,`session_id`,`summary_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow ContextEngine lifecycle event 表';

CREATE TABLE IF NOT EXISTS `df_progress_event` (
  `seq` bigint unsigned NOT NULL AUTO_INCREMENT,
  `id` varchar(191) NOT NULL,
  `schema_version` varchar(64) NOT NULL DEFAULT 'digflow.progress.v1',
  `run_id` varchar(191) NOT NULL DEFAULT '',
  `execution_id` varchar(191) NOT NULL DEFAULT '',
  `app_name` varchar(191) NOT NULL DEFAULT '',
  `tenant_id` varchar(191) NOT NULL DEFAULT '',
  `user_id` varchar(191) NOT NULL DEFAULT '',
  `scope_type` varchar(64) NOT NULL DEFAULT '',
  `scope_id` varchar(191) NOT NULL DEFAULT '',
  `workflow_instance_id` varchar(191) NOT NULL DEFAULT '',
  `node_key` varchar(191) NOT NULL DEFAULT '',
  `session_id` varchar(191) NOT NULL DEFAULT '',
  `phase` varchar(64) NOT NULL DEFAULT '',
  `status` varchar(64) NOT NULL DEFAULT '',
  `severity` varchar(32) NOT NULL DEFAULT 'info',
  `visibility` varchar(32) NOT NULL DEFAULT 'user',
  `title` varchar(255) NOT NULL DEFAULT '',
  `message` text,
  `elapsed_ms` bigint NOT NULL DEFAULT 0,
  `last_activity_at` datetime(3) DEFAULT NULL,
  `metrics` longtext,
  `payload` longtext,
  `source_type` varchar(64) NOT NULL DEFAULT '',
  `source_event_id` varchar(191) NOT NULL DEFAULT '',
  `source_key` varchar(64) DEFAULT NULL,
  `source_seq` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`seq`),
  UNIQUE KEY `idx_df_progress_event_id` (`id`),
  UNIQUE KEY `idx_progress_source_key` (`source_key`),
  KEY `idx_progress_run_seq` (`run_id`,`seq`),
  KEY `idx_progress_workflow_seq` (`workflow_instance_id`,`seq`),
  KEY `idx_progress_scope_seq` (`scope_type`,`scope_id`,`seq`),
  KEY `idx_progress_workflow_node_seq` (`workflow_instance_id`,`node_key`,`seq`),
  KEY `idx_progress_visibility_seq` (`visibility`,`seq`),
  KEY `idx_progress_source` (`source_type`,`source_event_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow runtime progress event 表';

CREATE TABLE IF NOT EXISTS `df_workflow_template` (
  `id` varchar(191) NOT NULL,
  `name` varchar(512) DEFAULT '',
  `agent_id` varchar(191) DEFAULT '',
  `description` text,
  `status` varchar(64) DEFAULT '',
  `current_version` bigint DEFAULT 0,
  `created_by` varchar(191) DEFAULT '',
  `updated_by` varchar(191) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_workflow_template_name` (`name`),
  KEY `idx_workflow_template_agent_id` (`agent_id`),
  KEY `idx_workflow_template_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 模板表';

CREATE TABLE IF NOT EXISTS `df_workflow_template_version` (
  `template_id` varchar(191) NOT NULL,
  `version` bigint NOT NULL,
  `source_instance_id` varchar(191) DEFAULT '',
  `patch` longtext,
  `notes` text,
  `created_by` varchar(191) DEFAULT '',
  `metadata` text,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`template_id`,`version`),
  KEY `idx_workflow_template_version_source_instance_id` (`source_instance_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow workflow 模板版本表';

-- Common optional embedded runtime domain tables
CREATE TABLE IF NOT EXISTS `df_skills` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(255) NOT NULL,
  `description` text,
  `version` varchar(50) DEFAULT '1.0.0',
  `tags` json DEFAULT NULL,
  `category` varchar(100) DEFAULT '',
  `priority` bigint DEFAULT 0,
  `content` longtext NOT NULL,
  `metadata` json DEFAULT NULL,
  `scope` varchar(50) DEFAULT 'global',
  `app_name` varchar(100) DEFAULT '',
  `file_path` varchar(500) DEFAULT '',
  `is_active` tinyint(1) DEFAULT 1,
  `created_by` varchar(100) DEFAULT '',
  `updated_by` varchar(100) DEFAULT '',
  `addtime` int unsigned DEFAULT 0,
  `updatetime` int unsigned DEFAULT 0,
  `deletetime` int unsigned DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_name` (`name`),
  KEY `idx_category` (`category`),
  KEY `idx_app_name` (`app_name`),
  KEY `idx_is_active` (`is_active`),
  KEY `idx_addtime` (`addtime`),
  KEY `idx_updatetime` (`updatetime`),
  KEY `idx_deletetime` (`deletetime`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow Skill 数据库表';

CREATE TABLE IF NOT EXISTS `df_chat_message` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `sessionid` varchar(50) NOT NULL,
  `segmentid` bigint NOT NULL,
  `type` varchar(50) NOT NULL,
  `role` varchar(50) NOT NULL,
  `app_name` varchar(255) NOT NULL,
  `msg` text NOT NULL,
  `meta` text,
  `status` tinyint unsigned DEFAULT 1,
  `uuid` bigint unsigned NOT NULL DEFAULT 0,
  `inputtime` int unsigned DEFAULT 0,
  `updatetime` int unsigned DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `fid` (`sessionid`),
  KEY `idx_app_name` (`app_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 对话消息表';

CREATE TABLE IF NOT EXISTS `df_chat_session` (
  `id` varchar(50) NOT NULL,
  `app_name` varchar(255) NOT NULL,
  `uuid` bigint unsigned NOT NULL DEFAULT 0,
  `title` varchar(255) DEFAULT '',
  `model_name` varchar(64) DEFAULT '',
  `channel` varchar(32) DEFAULT 'web',
  `agent_name` varchar(128) DEFAULT '',
  `profile_name` varchar(128) DEFAULT '',
  `workflow_name` varchar(128) DEFAULT '',
  `is_plan` tinyint(1) DEFAULT 0,
  `meta` json DEFAULT NULL,
  `inputtime` int unsigned DEFAULT 0,
  `updatetime` int unsigned DEFAULT 0,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 对话会话表';

CREATE TABLE IF NOT EXISTS `df_long_term_memory` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `app_name` varchar(64) NOT NULL,
  `user_id` varchar(128) NOT NULL,
  `category` varchar(32) NOT NULL,
  `content` text NOT NULL,
  `source` varchar(50) DEFAULT '',
  `importance` bigint DEFAULT 5,
  `access_count` bigint DEFAULT 0,
  `created_at` bigint DEFAULT 0,
  `updated_at` bigint DEFAULT 0,
  `expire_at` bigint DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_ltm_app_user` (`app_name`,`user_id`),
  KEY `idx_ltm_category` (`category`),
  KEY `idx_ltm_created` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow 长期记忆表';

CREATE TABLE IF NOT EXISTS `df_token_usage_event` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `run_id` varchar(128) NOT NULL,
  `request_id` varchar(128) NOT NULL,
  `app_code` varchar(64) NOT NULL,
  `user_id` varchar(64) NOT NULL,
  `session_id` varchar(128) NOT NULL DEFAULT '',
  `node_id` varchar(128) NOT NULL DEFAULT '',
  `node_name` varchar(128) NOT NULL DEFAULT '',
  `node_type` varchar(64) NOT NULL DEFAULT '',
  `resource_code` varchar(64) NOT NULL DEFAULT 'token',
  `unit` varchar(32) NOT NULL DEFAULT 'token',
  `provider` varchar(64) NOT NULL DEFAULT '',
  `model` varchar(128) NOT NULL DEFAULT '',
  `prompt_tokens` bigint NOT NULL DEFAULT 0,
  `completion_tokens` bigint NOT NULL DEFAULT 0,
  `total_tokens` bigint NOT NULL DEFAULT 0,
  `amount` bigint NOT NULL DEFAULT 0,
  `estimated` tinyint(1) NOT NULL DEFAULT 0,
  `metadata` json DEFAULT NULL,
  `created_at` bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_run_id` (`run_id`),
  KEY `idx_request_id` (`request_id`),
  KEY `idx_app_user` (`app_code`,`user_id`),
  KEY `idx_model` (`provider`,`model`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='DigFlow runtime 用量事件表';

SET FOREIGN_KEY_CHECKS = 1;


-- Latest pkg/digflow patch: LoopRuntime durable ledger / active lease (2026-07-04)
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


-- Latest pkg/digflow patch: ResourceAsset storage_key uniqueness (2026-07-07)
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
