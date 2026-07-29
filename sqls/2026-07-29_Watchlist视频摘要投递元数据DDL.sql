-- DigCrawl incremental DDL
-- Title: Watchlist 视频摘要投递元数据
-- Date: 2026-07-29
-- Purpose: Persist the summary provider used to render a delivery message so video retries cannot resend stale full-text or ASR fallback messages.

-- MySQL / SQLite compatible DDL
-- MySQL database: dig_crawl (select the database before running when using MySQL)
-- Table prefix: dc_

ALTER TABLE dc_watchlist_delivery
  ADD COLUMN message_provider TEXT;
