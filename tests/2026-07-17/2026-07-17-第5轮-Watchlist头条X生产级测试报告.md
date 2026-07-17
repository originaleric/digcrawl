# 2026-07-17 第5轮 Watchlist 头条/X 生产级测试报告

## 结论

本轮验证目标是对前序 Watchlist 头条文章与 X 单帖任务做新一轮生产级回归，确认 `internal/source` 抽象、Watchlist 调度依赖、真实平台可达性和 CLI 实际入口是否可用。

结论：**PARTIAL / BLOCKED**。

已恢复并通过：

- `internal/source` 单包测试通过，说明 source 抽象当前可编译、可测试。
- `internal/watchlist` 单包测试通过，说明 watchlist 核心编排、store、task runtime 相关单测可运行。
- `internal/summary` 单包测试通过，默认 stub 路径未被 DigFlow 私有 runtime 阻断。
- 头条文章 URL 和 X 单帖 URL 均可真实访问，返回 HTTP 200。
- 平台 source normalize probe 通过，头条文章能规范化为 `https://www.toutiao.com/article/1870280855070795/`，X 单帖能提取 status id。
- SQLite `dc_task_*` runtime DDL 可执行，4 张表和 9 个索引均创建成功。

仍阻塞：

- `go build ./cmd/digcrawl` 和 `go run ./cmd/digcrawl watchlist run --once ...` 均在进入业务逻辑前失败：`go: updates to go.mod needed; to update it: go mod tidy`。
- 因此本轮不能证明真实 `digcrawl watchlist run --once` 会完成 dry-run、写入 watchlist/task runtime 记录并生成实际 run/event。

## 测试环境

| 项 | 值 |
| --- | --- |
| 项目 | `/Users/dig/projects/digcrawl` |
| 日期 | 2026-07-17 |
| Go | `go1.26.2 darwin/arm64` |
| 测试目录 | `tests/2026-07-17/watchlist-prod-round-5/` |
| 测试配置 | `tests/2026-07-17/watchlist-prod-round-5/config.watchlist.yaml` |
| 运行模式 | Watchlist dry-run，Summary/Feishu disabled |

## 配置 Readiness

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| Watchlist 测试配置 | configured | 两个 task：头条文章、X 单帖 |
| SQLite runtime DDL | passed | `dc_task_*` runtime schema 创建成功 |
| Feishu webhook | skippable | 本轮 dry-run，不发送真实通知 |
| Summary provider | skippable | 本轮关闭摘要，避免 LLM 变量影响 watchlist 验证 |
| 平台网络 | configured | 头条和 X 均返回 HTTP 200 |
| Go module graph | blocked | CLI build/run 要求先 `go mod tidy` |

## 执行结果

### 1. 单包测试

```bash
GIT_TERMINAL_PROMPT=0 TMPDIR=/private/tmp \
GOPATH=/private/tmp/digcrawl-gopath-prod-r5 \
GOMODCACHE=/private/tmp/digcrawl-gomodcache-prod-r5 \
GOCACHE=/private/tmp/digcrawl-go-cache-prod-r5 \
go test ./internal/source -count=1
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/source 0.513s
```

```bash
go test ./internal/watchlist -count=1
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/watchlist 1.197s
```

```bash
go test ./internal/summary -count=1
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/summary 1.296s
```

### 2. CLI build / watchlist 实际入口

```bash
go build -o /private/tmp/digcrawl-prod-test-r5/digcrawl ./cmd/digcrawl
```

结果：失败。

```text
go: updates to go.mod needed; to update it:
	go mod tidy
```

```bash
go run ./cmd/digcrawl watchlist run --once --config tests/2026-07-17/watchlist-prod-round-5/config.watchlist.yaml
```

结果：失败。

```text
go: updates to go.mod needed; to update it:
	go mod tidy
```

补充诊断：执行只读 `go mod tidy -diff` 时开始重新拉取 `github.com/originaleric/DigFlow v1.1.3`、`modernc.org/sqlite` 等依赖，超过 60 秒仍未完成，已中止。当前阻塞点不是 watchlist 运行逻辑，而是 module graph 尚未收敛导致 Go 在进入 CLI 前停止。

### 3. Source normalize probe

```bash
go run tests/2026-07-17/watchlist-prod-round-5/platform_normalize_probe.go
```

结果：

```json
{
  "toutiao": {
    "canonical_url": "https://www.toutiao.com/article/1870280855070795/",
    "platform_id": "1870280855070795"
  },
  "x": {
    "canonical_url": "https://x.com/elonmusk/status/1053390822991790083",
    "platform_id": "1053390822991790083"
  }
}
```

结论：上一轮残留的头条 article source normalize 风险本轮已通过独立 probe 证明可用。

### 4. 真实平台可达性

头条文章：

```bash
curl -L --max-time 25 -A 'Mozilla/5.0 DigCrawl Production Test' \
  -D tests/2026-07-17/watchlist-prod-round-5/toutiao-article.headers \
  -o tests/2026-07-17/watchlist-prod-round-5/toutiao-article.html \
  'https://www.toutiao.com/w/1870280855070795/'
```

结果：

- HTTP 200
- HTML 大小：72914 bytes

X 单帖：

```bash
curl -L --max-time 25 -A 'Mozilla/5.0 DigCrawl Production Test' \
  -D tests/2026-07-17/watchlist-prod-round-5/x-elon-post.headers \
  -o tests/2026-07-17/watchlist-prod-round-5/x-elon-post.html \
  'https://x.com/elonmusk/status/1053390822991790083'
```

结果：

- HTTP 200
- HTML 大小：113303 bytes
- 页面 HTML 中包含 `elonmusk`、status id `1053390822991790083` 和帖子文本 `Had to been done ur welcome`

### 5. SQLite Task Runtime DDL

```bash
sed -n '/-- SQLite/,/-- End SQLite/p' sqls/2026-07-16_通用Task调度RuntimeDDL.sql \
  > tests/2026-07-17/watchlist-prod-round-5/task-runtime-sqlite.sql

sqlite3 tests/2026-07-17/watchlist-prod-round-5/runtime-schema.db \
  < tests/2026-07-17/watchlist-prod-round-5/task-runtime-sqlite.sql
```

结果：通过。

创建表：

- `dc_tasks_runtime`
- `dc_task_runs`
- `dc_task_locks`
- `dc_task_events`

创建索引：

- `idx_dc_tasks_runtime_next_run_at`
- `idx_dc_tasks_runtime_type_enabled`
- `idx_dc_task_runs_task_id`
- `idx_dc_task_runs_started_at`
- `idx_dc_task_runs_status`
- `idx_dc_task_locks_expires_at`
- `idx_dc_task_events_task_id`
- `idx_dc_task_events_run_id`
- `idx_dc_task_events_created_at`

## Findings

### P1: CLI build/run 仍被未 tidy 的 module graph 阻断，无法完成真实 Watchlist 生产执行

- 位置：`go.mod` / `go.sum`
- 复现：
  - `go build ./cmd/digcrawl`
  - `go run ./cmd/digcrawl watchlist run --once --config tests/2026-07-17/watchlist-prod-round-5/config.watchlist.yaml`
- 实际结果：
  - 两个命令均失败，输出 `go: updates to go.mod needed; to update it: go mod tidy`
- 预期结果：
  - CLI 能构建并进入 `watchlist run --once`，至少在 dry-run 模式下完成头条和 X 两个 task 的调度执行。
- 系统影响：
  - 单包测试通过不能替代真实入口验收。
  - 当前状态下用户无法通过标准 CLI 启动 watchlist，因此定时任务、source 抽象、通知链路都不能形成端到端生产证明。
- 修复方向：
  - 由实现任务执行 `go mod tidy` 或等价的 module graph 收敛操作，仔细 review `go.mod/go.sum` 的新增/删除。
  - 重点确认 DigFlow `v1.1.3` 默认构建是否仍引入不必要的 runtime/test 依赖，尤其是 `modernc.org/sqlite`。
  - 修复后必须重跑：
    - `go build ./cmd/digcrawl`
    - `go run ./cmd/digcrawl watchlist run --once --config tests/2026-07-17/watchlist-prod-round-5/config.watchlist.yaml`
    - `go test ./internal/source ./internal/watchlist ./internal/summary`

## 残余风险

- 本轮没有发送 Feishu 通知，通知链路仍需单独配置 webhook 后生产测试。
- 本轮没有启用摘要，DigFlow runtime / LLM 摘要链路仍需单独端到端测试。
- 头条 HTML 是动态页面壳，当前通过 URL normalize 和 Collect/source 链路规避页面正文解析风险；真实内容抽取质量仍需在 CLI run 跑通后验证。
- X 公开页面可访问，但平台反爬策略可能随时间变化，需要保留浏览器/登录态 fallback。

## 证据文件

- `tests/2026-07-17/watchlist-prod-round-5/config.watchlist.yaml`
- `tests/2026-07-17/watchlist-prod-round-5/platform_normalize_probe.go`
- `tests/2026-07-17/watchlist-prod-round-5/task-runtime-sqlite.sql`
- `tests/2026-07-17/watchlist-prod-round-5/runtime-schema.db`
- `tests/2026-07-17/watchlist-prod-round-5/toutiao-article.headers`
- `tests/2026-07-17/watchlist-prod-round-5/toutiao-article.html`
- `tests/2026-07-17/watchlist-prod-round-5/x-elon-post.headers`
- `tests/2026-07-17/watchlist-prod-round-5/x-elon-post.html`

## 最终判断

本轮比第4轮有实质进展：`internal/source`、`internal/watchlist`、`internal/summary`、平台 normalize、真实 HTTP 和 SQLite runtime DDL 均通过。当前唯一阻塞生产级端到端 Watchlist 验收的是 module graph 未 tidy，导致 CLI 入口无法构建/运行。

状态：**BLOCKED until go.mod/go.sum are reconciled and CLI run is retested**。
