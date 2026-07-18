-- DigCrawl full SQLite DDL (DigCrawl + DigFlow)
-- Default local database file: storage/digcrawl/collect.db
-- Generated: 2026-07-16
--
-- This file mirrors the SQLite schemas needed for local DigCrawl runtime,
-- including Collect, Watchlist/Task runtime, and embedded DigFlow tables.
-- MySQL deployments should use digcrawl_full_sql_ddl.sql.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS dc_collects (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_dc_collects_source_url
  ON dc_collects(source_url);

CREATE TABLE IF NOT EXISTS dc_collect_files (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  collect_id TEXT NOT NULL REFERENCES dc_collects(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  path       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dc_collect_media (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  collect_id     TEXT NOT NULL REFERENCES dc_collects(id) ON DELETE CASCADE,
  media_type     TEXT NOT NULL,
  remote_url     TEXT,
  local_path     TEXT,
  download_error TEXT,
  sort_order     INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS dc_tool_runs (
  id          TEXT PRIMARY KEY,
  tool_name   TEXT NOT NULL,
  source_url  TEXT,
  status      TEXT NOT NULL,
  output_json TEXT,
  created_at  TEXT NOT NULL
);

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

CREATE TABLE IF NOT EXISTS dc_watchlist_locks (
  watch_id    TEXT PRIMARY KEY,
  owner       TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  expires_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dc_watchlist_runs (
  run_id           TEXT PRIMARY KEY,
  watch_id         TEXT NOT NULL,
  owner            TEXT NOT NULL,
  started_at       TEXT NOT NULL,
  finished_at      TEXT,
  status           TEXT NOT NULL,
  discovered_count INTEGER DEFAULT 0,
  new_count        INTEGER DEFAULT 0,
  notified_count   INTEGER DEFAULT 0,
  error            TEXT
);

CREATE TABLE IF NOT EXISTS dc_watchlist_items (
  watch_id         TEXT NOT NULL,
  content_key      TEXT NOT NULL,
  platform_item_id TEXT,
  url              TEXT NOT NULL,
  canonical_url    TEXT,
  title            TEXT,
  published_at     TEXT,
  content_type     TEXT,
  content_hash     TEXT,
  summary_hash     TEXT,
  first_seen_at    TEXT NOT NULL,
  last_seen_at     TEXT NOT NULL,
  baseline_only    INTEGER DEFAULT 0,
  status           TEXT NOT NULL,
  collect_error    TEXT,
  summary_error    TEXT,
  PRIMARY KEY (watch_id, content_key)
);

CREATE TABLE IF NOT EXISTS dc_watchlist_delivery (
  watch_id      TEXT NOT NULL,
  content_key   TEXT NOT NULL,
  channel_id    TEXT NOT NULL,
  status        TEXT NOT NULL,
  notified_at   TEXT,
  retry_count   INTEGER DEFAULT 0,
  next_retry_at TEXT,
  notify_error  TEXT,
  message_title TEXT,
  message_text  TEXT,
  PRIMARY KEY (watch_id, content_key, channel_id)
);

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


-- DigFlow embedded runtime tables (SQLite)
-- Source: digflow_embedded_runtime_full_ddl.sql converted for local SQLite.

CREATE TABLE IF NOT EXISTS df_check_points (
  checkpoint_id TEXT NOT NULL,
  data BLOB,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (checkpoint_id)
);

CREATE TABLE IF NOT EXISTS df_execution_state (
  execution_id TEXT NOT NULL,
  app_name TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  execution_mode TEXT DEFAULT 'auto',
  paused_at_node TEXT DEFAULT NULL,
  checkpoint_nodes TEXT DEFAULT NULL,
  graph_state_messages TEXT,
  graph_state_data TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (execution_id)
);
CREATE INDEX IF NOT EXISTS df_execution_state_idx_execution_state_app_name ON df_execution_state(app_name);
CREATE INDEX IF NOT EXISTS df_execution_state_idx_execution_state_session_id ON df_execution_state(session_id);

CREATE TABLE IF NOT EXISTS df_execution (
  execution_id TEXT NOT NULL,
  app_name TEXT NOT NULL,
  session_id TEXT DEFAULT '',
  status TEXT DEFAULT 'running',
  created_at TEXT DEFAULT CURRENT_TEXT,
  updated_at TEXT DEFAULT CURRENT_TEXT,
  PRIMARY KEY (execution_id)
);
CREATE INDEX IF NOT EXISTS df_execution_idx_app_session ON df_execution(app_name,session_id);

CREATE TABLE IF NOT EXISTS df_execution_status (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  execution_id TEXT NOT NULL,
  type TEXT NOT NULL,
  timestamp INTEGER NOT NULL,
  node_key TEXT DEFAULT NULL,
  node_type TEXT DEFAULT NULL,
  status TEXT NOT NULL,
  error TEXT,
  data_flow TEXT DEFAULT NULL,
  control_flow TEXT DEFAULT NULL,
  app_name TEXT DEFAULT NULL,
  request_id TEXT DEFAULT NULL
);
CREATE INDEX IF NOT EXISTS df_execution_status_idx_exec_id ON df_execution_status(execution_id);
CREATE INDEX IF NOT EXISTS df_execution_status_idx_node ON df_execution_status(node_key);

CREATE TABLE IF NOT EXISTS df_resource_asset (
  id TEXT NOT NULL,
  owner_type TEXT DEFAULT '',
  owner_id TEXT DEFAULT '',
  sha256 TEXT DEFAULT '',
  file_name TEXT DEFAULT '',
  mime_type TEXT DEFAULT '',
  file_size INTEGER DEFAULT 0,
  storage_key TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_resource_asset_idx_resource_asset_owner_sha ON df_resource_asset(owner_type,owner_id,sha256);

CREATE TABLE IF NOT EXISTS df_resource_file (
  id TEXT NOT NULL,
  asset_id TEXT DEFAULT '',
  owner_type TEXT DEFAULT '',
  owner_id TEXT DEFAULT '',
  workspace_id TEXT DEFAULT '',
  sha256 TEXT DEFAULT '',
  file_name TEXT DEFAULT '',
  mime_type TEXT DEFAULT '',
  file_size INTEGER DEFAULT 0,
  storage_key TEXT DEFAULT '',
  status TEXT DEFAULT '',
  source TEXT DEFAULT '',
  visibility TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_resource_file_idx_resource_file_asset_id ON df_resource_file(asset_id);
CREATE INDEX IF NOT EXISTS df_resource_file_idx_resource_file_workspace_id ON df_resource_file(workspace_id);

CREATE TABLE IF NOT EXISTS df_resource_parse_result (
  id TEXT NOT NULL,
  asset_id TEXT DEFAULT '',
  parser TEXT DEFAULT '',
  parser_version TEXT DEFAULT '',
  signature TEXT DEFAULT '',
  signature_hash TEXT DEFAULT '',
  status TEXT DEFAULT '',
  text_digest TEXT DEFAULT '',
  chunk_count INTEGER DEFAULT 0,
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_resource_parse_result_idx_resource_parse_asset_signature ON df_resource_parse_result(asset_id,signature_hash);

CREATE TABLE IF NOT EXISTS df_resource_chunk (
  id TEXT NOT NULL,
  asset_id TEXT DEFAULT '',
  resource_file_id TEXT DEFAULT '',
  workspace_id TEXT DEFAULT '',
  parse_result_id TEXT DEFAULT '',
  parser_signature_hash TEXT DEFAULT '',
  chunk_index INTEGER DEFAULT 0,
  content TEXT,
  token_count INTEGER DEFAULT 0,
  index_status TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_resource_chunk_idx_resource_chunk_asset_id ON df_resource_chunk(asset_id);
CREATE INDEX IF NOT EXISTS df_resource_chunk_idx_resource_chunk_resource_file_id ON df_resource_chunk(resource_file_id);
CREATE INDEX IF NOT EXISTS df_resource_chunk_idx_resource_chunk_workspace_id ON df_resource_chunk(workspace_id);
CREATE INDEX IF NOT EXISTS df_resource_chunk_idx_resource_chunk_parse_result_id ON df_resource_chunk(parse_result_id);
CREATE INDEX IF NOT EXISTS df_resource_chunk_idx_resource_chunk_parser_signature_hash ON df_resource_chunk(parser_signature_hash);

CREATE TABLE IF NOT EXISTS df_resource_ingestion_pipeline (
  id TEXT NOT NULL,
  resource_file_id TEXT DEFAULT '',
  asset_id TEXT DEFAULT '',
  workspace_id TEXT DEFAULT '',
  owner_type TEXT DEFAULT '',
  owner_id TEXT DEFAULT '',
  parse_profile_signature_hash TEXT DEFAULT '',
  index_provider_signature TEXT DEFAULT '',
  idempotency_key TEXT DEFAULT '',
  active_key TEXT DEFAULT NULL,
  status TEXT DEFAULT '',
  current_stage TEXT DEFAULT '',
  asset_job_id TEXT DEFAULT '',
  file_index_job_id TEXT DEFAULT '',
  cancel_requested_at TEXT DEFAULT NULL,
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  finished_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS df_resource_ingestion_pipeline_uidx_resource_pipeline_active_key ON df_resource_ingestion_pipeline(active_key);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_pipeline_idx_resource_ingestion_pipeline_resource_file_id ON df_resource_ingestion_pipeline(resource_file_id);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_pipeline_idx_resource_ingestion_pipeline_asset_id ON df_resource_ingestion_pipeline(asset_id);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_pipeline_idx_resource_pipeline_workspace_status ON df_resource_ingestion_pipeline(workspace_id, status);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_pipeline_idx_resource_ingestion_pipeline_idempotency_key ON df_resource_ingestion_pipeline(idempotency_key);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_pipeline_idx_resource_pipeline_asset_job_status ON df_resource_ingestion_pipeline(asset_job_id, status);

CREATE TABLE IF NOT EXISTS df_resource_ingestion_job (
  id TEXT NOT NULL,
  pipeline_id TEXT DEFAULT '',
  resource_file_id TEXT DEFAULT '',
  trigger_resource_file_id TEXT DEFAULT '',
  asset_id TEXT DEFAULT '',
  workspace_id TEXT DEFAULT '',
  owner_type TEXT DEFAULT '',
  owner_id TEXT DEFAULT '',
  job_kind TEXT DEFAULT '',
  job_scope_type TEXT DEFAULT '',
  job_scope_id TEXT DEFAULT '',
  parser_signature_hash TEXT DEFAULT '',
  idempotency_key TEXT DEFAULT '',
  active_key TEXT DEFAULT NULL,
  depends_on_job_id TEXT DEFAULT '',
  depends_on_scope_key TEXT DEFAULT '',
  status TEXT DEFAULT '',
  stage TEXT DEFAULT '',
  priority INTEGER DEFAULT 0,
  attempt_count INTEGER DEFAULT 0,
  max_attempts INTEGER DEFAULT 0,
  lease_owner TEXT DEFAULT '',
  lease_until TEXT DEFAULT NULL,
  locked_at TEXT DEFAULT NULL,
  cancel_requested_at TEXT DEFAULT NULL,
  pages_total INTEGER DEFAULT 0,
  pages_done INTEGER DEFAULT 0,
  chunks_total INTEGER DEFAULT 0,
  chunks_done INTEGER DEFAULT 0,
  indexed_chunks INTEGER DEFAULT 0,
  failed_chunks INTEGER DEFAULT 0,
  last_error TEXT,
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  started_at TEXT DEFAULT NULL,
  finished_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS df_resource_ingestion_job_uidx_resource_ingestion_active_key ON df_resource_ingestion_job(active_key);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_job_pipeline_id ON df_resource_ingestion_job(pipeline_id);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_job_resource_file_id ON df_resource_ingestion_job(resource_file_id);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_job_asset_id ON df_resource_ingestion_job(asset_id);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_job_idempotency_key ON df_resource_ingestion_job(idempotency_key);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_job_depends_on_job_id ON df_resource_ingestion_job(depends_on_job_id);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_workspace_status ON df_resource_ingestion_job(workspace_id, status);
CREATE INDEX IF NOT EXISTS df_resource_ingestion_job_idx_resource_ingestion_status_priority ON df_resource_ingestion_job(status, priority, lease_until, created_at);

CREATE TABLE IF NOT EXISTS df_resource_retrieval_session (
  id TEXT NOT NULL,
  scope_type TEXT DEFAULT '',
  scope_id TEXT DEFAULT '',
  query TEXT NOT NULL,
  top_k INTEGER DEFAULT 0,
  strategy TEXT DEFAULT '',
  filters TEXT,
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_resource_retrieval_session_idx_resource_retrieval_scope_type ON df_resource_retrieval_session(scope_type);
CREATE INDEX IF NOT EXISTS df_resource_retrieval_session_idx_resource_retrieval_scope_id ON df_resource_retrieval_session(scope_id);

CREATE TABLE IF NOT EXISTS df_resource_retrieval_hit (
  id TEXT NOT NULL,
  retrieval_id TEXT NOT NULL,
  resource_file_id TEXT NOT NULL,
  chunk_id TEXT NOT NULL,
  score REAL DEFAULT 0,
  snippet TEXT,
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_resource_retrieval_hit_idx_resource_retrieval_hit_retrieval_id ON df_resource_retrieval_hit(retrieval_id);
CREATE INDEX IF NOT EXISTS df_resource_retrieval_hit_idx_resource_retrieval_hit_resource_file_id ON df_resource_retrieval_hit(resource_file_id);
CREATE INDEX IF NOT EXISTS df_resource_retrieval_hit_idx_resource_retrieval_hit_chunk_id ON df_resource_retrieval_hit(chunk_id);

CREATE TABLE IF NOT EXISTS df_workflow_instance (
  id TEXT NOT NULL,
  agent_id TEXT DEFAULT '',
  title TEXT DEFAULT '',
  status TEXT DEFAULT '',
  current_node TEXT DEFAULT '',
  created_by TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_workflow_instance_idx_workflow_instance_agent_id ON df_workflow_instance(agent_id);
CREATE INDEX IF NOT EXISTS df_workflow_instance_idx_workflow_instance_status ON df_workflow_instance(status);

CREATE TABLE IF NOT EXISTS df_workflow_node_state (
  instance_id TEXT NOT NULL,
  node_key TEXT NOT NULL,
  status TEXT DEFAULT '',
  run_id TEXT DEFAULT '',
  execution_id TEXT DEFAULT '',
  chat_session_id TEXT DEFAULT '',
  latest_draft_artifact_id TEXT DEFAULT '',
  confirmed_artifact_id TEXT DEFAULT '',
  stale INTEGER DEFAULT 0,
  error_message TEXT,
  metadata TEXT,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (instance_id, node_key)
);
CREATE INDEX IF NOT EXISTS df_workflow_node_state_idx_workflow_node_state_status ON df_workflow_node_state(status);

CREATE TABLE IF NOT EXISTS df_workflow_node_attachment (
  id TEXT NOT NULL,
  instance_id TEXT DEFAULT '',
  node_key TEXT DEFAULT '',
  file_id TEXT DEFAULT '',
  resource_file_id TEXT DEFAULT '',
  file_name TEXT DEFAULT '',
  mime_type TEXT DEFAULT '',
  file_size INTEGER DEFAULT 0,
  role TEXT DEFAULT '',
  source TEXT DEFAULT '',
  status TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_workflow_node_attachment_idx_workflow_attachment_node ON df_workflow_node_attachment(instance_id,node_key);
CREATE INDEX IF NOT EXISTS df_workflow_node_attachment_idx_workflow_attachment_resource_file_id ON df_workflow_node_attachment(resource_file_id);
CREATE UNIQUE INDEX IF NOT EXISTS df_workflow_node_attachment_uidx_workflow_attachment_resource_role ON df_workflow_node_attachment(instance_id,node_key,resource_file_id,role);

CREATE TABLE IF NOT EXISTS df_workflow_node_chat_session (
  session_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  node_key TEXT NOT NULL,
  agent_id TEXT DEFAULT '',
  user_id TEXT DEFAULT '',
  app_name TEXT DEFAULT '',
  title TEXT DEFAULT '',
  status TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (session_id, instance_id, node_key)
);
CREATE INDEX IF NOT EXISTS df_workflow_node_chat_session_idx_workflow_node_chat_session_node ON df_workflow_node_chat_session(instance_id,node_key);
CREATE INDEX IF NOT EXISTS df_workflow_node_chat_session_idx_workflow_node_chat_session_agent_id ON df_workflow_node_chat_session(agent_id);
CREATE INDEX IF NOT EXISTS df_workflow_node_chat_session_idx_workflow_node_chat_session_user_id ON df_workflow_node_chat_session(user_id);
CREATE INDEX IF NOT EXISTS df_workflow_node_chat_session_idx_workflow_node_chat_session_status ON df_workflow_node_chat_session(status);

CREATE TABLE IF NOT EXISTS df_workflow_node_chat_run (
  run_id TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  node_key TEXT NOT NULL,
  chat_session_id TEXT NOT NULL,
  app_name TEXT DEFAULT '',
  user_id TEXT DEFAULT '',
  status TEXT NOT NULL,
  cancel_reason TEXT DEFAULT '',
  partial_content TEXT,
  partial_reasoning TEXT,
  error_message TEXT,
  metadata TEXT,
  started_at TEXT DEFAULT NULL,
  cancel_requested_at TEXT DEFAULT NULL,
  ended_at TEXT DEFAULT NULL,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (run_id)
);
CREATE INDEX IF NOT EXISTS df_workflow_node_chat_run_idx_df_workflow_node_chat_run_scope ON df_workflow_node_chat_run(instance_id,node_key,chat_session_id);
CREATE INDEX IF NOT EXISTS df_workflow_node_chat_run_idx_df_workflow_node_chat_run_status ON df_workflow_node_chat_run(status);

CREATE TABLE IF NOT EXISTS df_workflow_artifact (
  id TEXT NOT NULL,
  instance_id TEXT DEFAULT '',
  node_key TEXT DEFAULT '',
  visibility TEXT NOT NULL DEFAULT '',
  node_path TEXT NOT NULL DEFAULT '',
  parent_node_key TEXT NOT NULL DEFAULT '',
  subgraph_scope_id TEXT NOT NULL DEFAULT '',
  subgraph_invocation_id TEXT NOT NULL DEFAULT '',
  owner_scope_type TEXT NOT NULL DEFAULT '',
  owner_scope_id TEXT NOT NULL DEFAULT '',
  type TEXT DEFAULT '',
  format TEXT DEFAULT '',
  title TEXT DEFAULT '',
  content TEXT,
  file_id TEXT DEFAULT '',
  download_url TEXT DEFAULT '',
  version INTEGER DEFAULT 0,
  status TEXT DEFAULT '',
  source_session_id TEXT DEFAULT '',
  source_message_id TEXT DEFAULT '',
  upstream_artifact_ids TEXT,
  idempotency_key TEXT DEFAULT '',
  payload_hash TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idx_workflow_artifact_instance_node ON df_workflow_artifact(instance_id,node_key);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idx_workflow_artifact_scope_visibility ON df_workflow_artifact(instance_id,owner_scope_type,owner_scope_id,visibility);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idx_workflow_artifact_parent_scope ON df_workflow_artifact(instance_id,parent_node_key,subgraph_scope_id);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idx_workflow_artifact_visibility_node ON df_workflow_artifact(instance_id,visibility,node_key);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idx_workflow_artifact_status ON df_workflow_artifact(status);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idx_workflow_artifact_node_version ON df_workflow_artifact(instance_id,node_key,version);
CREATE UNIQUE INDEX IF NOT EXISTS df_workflow_artifact_uk_workflow_artifact_scope_version ON df_workflow_artifact(instance_id,owner_scope_type,owner_scope_id,node_path,version);

CREATE TABLE IF NOT EXISTS df_workflow_artifact_idempotency (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  scope_hash TEXT NOT NULL,
  scope TEXT,
  idempotency_key TEXT NOT NULL,
  artifact_id TEXT NOT NULL DEFAULT '',
  payload_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS df_workflow_artifact_idempotency_uk_workflow_artifact_idempotency ON df_workflow_artifact_idempotency(scope_hash,idempotency_key);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_idempotency_idx_workflow_artifact_idempotency_artifact ON df_workflow_artifact_idempotency(artifact_id);

CREATE TABLE IF NOT EXISTS df_workflow_artifact_edge (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  instance_id TEXT DEFAULT '',
  from_node_key TEXT DEFAULT '',
  to_node_key TEXT DEFAULT '',
  artifact_id TEXT DEFAULT '',
  handoff_status TEXT DEFAULT '',
  created_at TEXT DEFAULT NULL
);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_edge_idx_workflow_artifact_edge_instance_id ON df_workflow_artifact_edge(instance_id);
CREATE INDEX IF NOT EXISTS df_workflow_artifact_edge_idx_workflow_artifact_edge_artifact_id ON df_workflow_artifact_edge(artifact_id);
CREATE UNIQUE INDEX IF NOT EXISTS df_workflow_artifact_edge_uk_workflow_artifact_edge_once ON df_workflow_artifact_edge(instance_id,from_node_key,to_node_key,artifact_id);

CREATE TABLE IF NOT EXISTS df_workflow_subagent_run (
  id TEXT NOT NULL,
  instance_id TEXT DEFAULT '',
  parent_node_key TEXT DEFAULT '',
  agent_id TEXT DEFAULT '',
  run_id TEXT DEFAULT '',
  execution_id TEXT DEFAULT '',
  status TEXT DEFAULT '',
  input_artifact_id TEXT DEFAULT '',
  output_artifact_id TEXT DEFAULT '',
  error_message TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_workflow_subagent_run_idx_workflow_subagent_run_instance_id ON df_workflow_subagent_run(instance_id);
CREATE INDEX IF NOT EXISTS df_workflow_subagent_run_idx_workflow_subagent_run_parent_node_key ON df_workflow_subagent_run(parent_node_key);

CREATE TABLE IF NOT EXISTS df_workflow_event (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT DEFAULT '',
  instance_id TEXT DEFAULT '',
  node_key TEXT DEFAULT '',
  type TEXT DEFAULT '',
  artifact_id TEXT DEFAULT '',
  run_id TEXT DEFAULT '',
  execution_id TEXT DEFAULT '',
  payload TEXT,
  created_at TEXT DEFAULT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS df_workflow_event_idx_workflow_event_id ON df_workflow_event(id);
CREATE INDEX IF NOT EXISTS df_workflow_event_idx_workflow_event_instance_seq ON df_workflow_event(instance_id,seq);
CREATE INDEX IF NOT EXISTS df_workflow_event_idx_workflow_event_node_key ON df_workflow_event(node_key);
CREATE INDEX IF NOT EXISTS df_workflow_event_idx_workflow_event_type ON df_workflow_event(type);
CREATE INDEX IF NOT EXISTS df_workflow_event_idx_workflow_event_artifact_id ON df_workflow_event(artifact_id);

CREATE TABLE IF NOT EXISTS df_context_event (
  id TEXT NOT NULL,
  app_name TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  scope_type TEXT NOT NULL DEFAULT '',
  scope_id TEXT NOT NULL DEFAULT '',
  context_scope_id TEXT NOT NULL DEFAULT '',
  workflow_instance_id TEXT NOT NULL DEFAULT '',
  node_key TEXT NOT NULL DEFAULT '',
  parent_node_key TEXT NOT NULL DEFAULT '',
  subgraph_scope_id TEXT NOT NULL DEFAULT '',
  subgraph_invocation_id TEXT NOT NULL DEFAULT '',
  subgraph_node_path TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  summary_snapshot TEXT,
  messages_before INTEGER NOT NULL DEFAULT 0,
  messages_kept INTEGER NOT NULL DEFAULT 0,
  summary_hash TEXT NOT NULL DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_session_time ON df_context_event(app_name,session_id,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_scope_time ON df_context_event(scope_type,scope_id,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_context_scope_time ON df_context_event(context_scope_id,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_workflow_node ON df_context_event(workflow_instance_id,node_key,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_parent_subgraph_time ON df_context_event(parent_node_key,subgraph_scope_id,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_subgraph_scope_time ON df_context_event(subgraph_scope_id,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_subgraph_invocation_time ON df_context_event(subgraph_invocation_id,created_at);
CREATE INDEX IF NOT EXISTS df_context_event_idx_context_event_summary_hash ON df_context_event(app_name,session_id,summary_hash);

CREATE TABLE IF NOT EXISTS df_progress_event (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL,
  schema_version TEXT NOT NULL DEFAULT 'digflow.progress.v1',
  run_id TEXT NOT NULL DEFAULT '',
  execution_id TEXT NOT NULL DEFAULT '',
  app_name TEXT NOT NULL DEFAULT '',
  tenant_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  scope_type TEXT NOT NULL DEFAULT '',
  scope_id TEXT NOT NULL DEFAULT '',
  workflow_instance_id TEXT NOT NULL DEFAULT '',
  node_key TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  phase TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL DEFAULT 'info',
  visibility TEXT NOT NULL DEFAULT 'user',
  title TEXT NOT NULL DEFAULT '',
  message TEXT,
  elapsed_ms INTEGER NOT NULL DEFAULT 0,
  last_activity_at TEXT DEFAULT NULL,
  metrics TEXT,
  payload TEXT,
  source_type TEXT NOT NULL DEFAULT '',
  source_event_id TEXT NOT NULL DEFAULT '',
  source_key TEXT DEFAULT NULL,
  source_seq INTEGER NOT NULL DEFAULT 0,
  created_at TEXT DEFAULT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS df_progress_event_idx_df_progress_event_id ON df_progress_event(id);
CREATE UNIQUE INDEX IF NOT EXISTS df_progress_event_idx_progress_source_key ON df_progress_event(source_key);
CREATE INDEX IF NOT EXISTS df_progress_event_idx_progress_run_seq ON df_progress_event(run_id,seq);
CREATE INDEX IF NOT EXISTS df_progress_event_idx_progress_workflow_seq ON df_progress_event(workflow_instance_id,seq);
CREATE INDEX IF NOT EXISTS df_progress_event_idx_progress_scope_seq ON df_progress_event(scope_type,scope_id,seq);
CREATE INDEX IF NOT EXISTS df_progress_event_idx_progress_workflow_node_seq ON df_progress_event(workflow_instance_id,node_key,seq);
CREATE INDEX IF NOT EXISTS df_progress_event_idx_progress_visibility_seq ON df_progress_event(visibility,seq);
CREATE INDEX IF NOT EXISTS df_progress_event_idx_progress_source ON df_progress_event(source_type,source_event_id);

CREATE TABLE IF NOT EXISTS df_workflow_template (
  id TEXT NOT NULL,
  name TEXT DEFAULT '',
  agent_id TEXT DEFAULT '',
  description TEXT,
  status TEXT DEFAULT '',
  current_version INTEGER DEFAULT 0,
  created_by TEXT DEFAULT '',
  updated_by TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_workflow_template_idx_workflow_template_name ON df_workflow_template(name);
CREATE INDEX IF NOT EXISTS df_workflow_template_idx_workflow_template_agent_id ON df_workflow_template(agent_id);
CREATE INDEX IF NOT EXISTS df_workflow_template_idx_workflow_template_status ON df_workflow_template(status);

CREATE TABLE IF NOT EXISTS df_workflow_template_version (
  template_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  source_instance_id TEXT DEFAULT '',
  patch TEXT,
  notes TEXT,
  created_by TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  PRIMARY KEY (template_id, version)
);
CREATE INDEX IF NOT EXISTS df_workflow_template_version_idx_workflow_template_version_source_instance_id ON df_workflow_template_version(source_instance_id);

CREATE TABLE IF NOT EXISTS df_skills (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT,
  version TEXT DEFAULT '1.0.0',
  tags TEXT DEFAULT NULL,
  category TEXT DEFAULT '',
  priority INTEGER DEFAULT 0,
  content TEXT NOT NULL,
  metadata TEXT DEFAULT NULL,
  scope TEXT DEFAULT 'global',
  app_name TEXT DEFAULT '',
  file_path TEXT DEFAULT '',
  is_active INTEGER DEFAULT 1,
  created_by TEXT DEFAULT '',
  updated_by TEXT DEFAULT '',
  addtime INTEGER DEFAULT 0,
  updatetime INTEGER DEFAULT 0,
  deletetime INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS df_skills_idx_name ON df_skills(name);
CREATE INDEX IF NOT EXISTS df_skills_idx_category ON df_skills(category);
CREATE INDEX IF NOT EXISTS df_skills_idx_app_name ON df_skills(app_name);
CREATE INDEX IF NOT EXISTS df_skills_idx_is_active ON df_skills(is_active);
CREATE INDEX IF NOT EXISTS df_skills_idx_addtime ON df_skills(addtime);
CREATE INDEX IF NOT EXISTS df_skills_idx_updatetime ON df_skills(updatetime);
CREATE INDEX IF NOT EXISTS df_skills_idx_deletetime ON df_skills(deletetime);

CREATE TABLE IF NOT EXISTS df_chat_message (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sessionid TEXT NOT NULL,
  segmentid INTEGER NOT NULL,
  type TEXT NOT NULL,
  role TEXT NOT NULL,
  app_name TEXT NOT NULL,
  msg TEXT NOT NULL,
  meta TEXT,
  status INTEGER DEFAULT 1,
  uuid INTEGER NOT NULL DEFAULT 0,
  inputtime INTEGER DEFAULT 0,
  updatetime INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS df_chat_message_fid ON df_chat_message(sessionid);
CREATE INDEX IF NOT EXISTS df_chat_message_idx_app_name ON df_chat_message(app_name);

CREATE TABLE IF NOT EXISTS df_chat_session (
  id TEXT NOT NULL,
  app_name TEXT NOT NULL,
  uuid INTEGER NOT NULL DEFAULT 0,
  title TEXT DEFAULT '',
  model_name TEXT DEFAULT '',
  channel TEXT DEFAULT 'web',
  agent_name TEXT DEFAULT '',
  profile_name TEXT DEFAULT '',
  workflow_name TEXT DEFAULT '',
  is_plan INTEGER DEFAULT 0,
  meta TEXT DEFAULT NULL,
  inputtime INTEGER DEFAULT 0,
  updatetime INTEGER DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS df_long_term_memory (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_name TEXT NOT NULL,
  user_id TEXT NOT NULL,
  category TEXT NOT NULL,
  content TEXT NOT NULL,
  source TEXT DEFAULT '',
  importance INTEGER DEFAULT 5,
  access_count INTEGER DEFAULT 0,
  created_at INTEGER DEFAULT 0,
  updated_at INTEGER DEFAULT 0,
  expire_at INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS df_long_term_memory_idx_ltm_app_user ON df_long_term_memory(app_name,user_id);
CREATE INDEX IF NOT EXISTS df_long_term_memory_idx_ltm_category ON df_long_term_memory(category);
CREATE INDEX IF NOT EXISTS df_long_term_memory_idx_ltm_created ON df_long_term_memory(created_at);

CREATE TABLE IF NOT EXISTS df_token_usage_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  app_code TEXT NOT NULL,
  user_id TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  node_id TEXT NOT NULL DEFAULT '',
  node_name TEXT NOT NULL DEFAULT '',
  node_type TEXT NOT NULL DEFAULT '',
  resource_code TEXT NOT NULL DEFAULT 'token',
  unit TEXT NOT NULL DEFAULT 'token',
  provider TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  total_tokens INTEGER NOT NULL DEFAULT 0,
  amount INTEGER NOT NULL DEFAULT 0,
  estimated INTEGER NOT NULL DEFAULT 0,
  metadata TEXT DEFAULT NULL,
  created_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS df_token_usage_event_idx_run_id ON df_token_usage_event(run_id);
CREATE INDEX IF NOT EXISTS df_token_usage_event_idx_request_id ON df_token_usage_event(request_id);
CREATE INDEX IF NOT EXISTS df_token_usage_event_idx_app_user ON df_token_usage_event(app_code,user_id);
CREATE INDEX IF NOT EXISTS df_token_usage_event_idx_model ON df_token_usage_event(provider,model);

CREATE TABLE IF NOT EXISTS df_loop_instance (
  id TEXT NOT NULL,
  agent_id TEXT DEFAULT '',
  loop_name TEXT DEFAULT '',
  goal TEXT,
  status TEXT DEFAULT '',
  terminal_reason TEXT DEFAULT '',
  iteration_count INTEGER DEFAULT 0,
  token_budget INTEGER DEFAULT 0,
  tokens_used INTEGER DEFAULT 0,
  scope TEXT DEFAULT '',
  metadata TEXT,
  created_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  completed_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_loop_instance_idx_loop_instance_agent_status ON df_loop_instance(agent_id, loop_name, status);
CREATE INDEX IF NOT EXISTS df_loop_instance_idx_loop_instance_scope ON df_loop_instance(scope);

CREATE TABLE IF NOT EXISTS df_loop_iteration (
  id TEXT NOT NULL,
  instance_id TEXT DEFAULT '',
  agent_id TEXT DEFAULT '',
  loop_name TEXT DEFAULT '',
  scope TEXT DEFAULT '',
  iteration INTEGER DEFAULT 0,
  status TEXT DEFAULT '',
  terminal_reason TEXT DEFAULT '',
  execution_id TEXT DEFAULT '',
  output TEXT,
  output_hash TEXT DEFAULT '',
  output_truncated INTEGER DEFAULT 0,
  token_usage TEXT,
  validation_results TEXT,
  request_metadata TEXT,
  error TEXT,
  started_at TEXT DEFAULT NULL,
  completed_at TEXT DEFAULT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX IF NOT EXISTS df_loop_iteration_idx_loop_iteration_instance_iteration ON df_loop_iteration(instance_id, iteration);
CREATE INDEX IF NOT EXISTS df_loop_iteration_idx_loop_iteration_agent_id ON df_loop_iteration(agent_id);
CREATE INDEX IF NOT EXISTS df_loop_iteration_idx_loop_iteration_scope ON df_loop_iteration(scope);
CREATE INDEX IF NOT EXISTS df_loop_iteration_idx_loop_iteration_execution_id ON df_loop_iteration(execution_id);
CREATE INDEX IF NOT EXISTS df_loop_iteration_idx_loop_iteration_output_hash ON df_loop_iteration(output_hash);

CREATE TABLE IF NOT EXISTS df_loop_active (
  active_key TEXT NOT NULL,
  agent_id TEXT DEFAULT '',
  loop_name TEXT DEFAULT '',
  scope TEXT DEFAULT '',
  owner_token TEXT DEFAULT '',
  lease_until TEXT DEFAULT NULL,
  acquired_at TEXT DEFAULT NULL,
  updated_at TEXT DEFAULT NULL,
  PRIMARY KEY (active_key)
);
CREATE INDEX IF NOT EXISTS df_loop_active_idx_loop_active_agent_id ON df_loop_active(agent_id);
CREATE INDEX IF NOT EXISTS df_loop_active_idx_loop_active_owner_token ON df_loop_active(owner_token);
CREATE INDEX IF NOT EXISTS df_loop_active_idx_loop_active_lease_until ON df_loop_active(lease_until);

-- Latest pkg/digflow patch: ResourceAsset storage_key uniqueness (SQLite)
UPDATE df_resource_asset SET storage_key = NULL WHERE storage_key IS NOT NULL AND TRIM(storage_key) = '';
CREATE UNIQUE INDEX IF NOT EXISTS df_resource_asset_uidx_resource_asset_storage_key ON df_resource_asset(storage_key);
