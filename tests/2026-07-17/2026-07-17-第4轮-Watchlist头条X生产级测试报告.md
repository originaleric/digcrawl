# 2026-07-17 第4轮 Watchlist 头条 / X 生产级测试报告

## 结论

本轮结论：`PARTIAL / BLOCKED`。

已验证：

- `internal/watchlist` 核心包测试通过，且耗时约 1.1s。
- 头条指定文章 URL 真实 HTTP 探测可达，返回 `HTTP/2 200`，保存 72,914 bytes HTML。
- X / Elon Musk 指定帖子真实 HTTP 探测可达，返回 `HTTP/2 200`，保存 113,303 bytes HTML，并包含 canonical URL、帖子 ID、作者与正文元数据。
- 通用 Task runtime SQLite DDL 可初始化，生成 `dc_tasks_runtime`、`dc_task_runs`、`dc_task_locks`、`dc_task_events`。

未完成：

- `go build ./cmd/digcrawl` 超过 3 分钟无输出，已中止。
- `go test ./internal/source` 超过 2 分钟无输出，已中止。
- 轻量 platform normalize probe 超过 90 秒无输出，已中止。
- 因完整 Go 重依赖编译阻塞，未能执行真实 `digcrawl watchlist run --once` 或 `watchlist serve`。

## 本轮目标

按用户要求对前序测试任务做新一轮生产级测试，覆盖：

- Watchlist / Tasks runtime 基础能力。
- 真实头条来源。
- 真实 X 来源。
- 尽量验证修复后 source / watchlist 能否进入真实执行路径。

## 环境摘要

| 项目 | 结果 |
| --- | --- |
| 仓库 | `/Users/dig/projects/digcrawl` |
| 日期 | 2026-07-17 |
| 测试目录 | `tests/2026-07-17/watchlist-prod-round-4/` |
| 真实 Feishu 通知 | skipped，测试配置使用 `Watchlist.DryRun=true` |
| Summary / LLM | skipped，测试配置使用 `Summary.Enabled=false` |
| 持久化 | SQLite 测试库 |

## 配置 Readiness

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| `Watchlist.Enabled` | configured | 测试配置为 `true` |
| `Watchlist.DryRun` | configured | 测试配置为 `true`，避免真实推送 |
| `Watchlist.Scheduler.Enabled` | configured | 测试配置为 `true` |
| `Tasks` | configured | 配置头条指定文章和 X 指定帖子 |
| SQLite runtime DDL | passed | 成功初始化 4 张 `dc_task_*` 表 |
| 头条网络可达性 | passed | `HTTP/2 200` |
| X 网络可达性 | passed | `HTTP/2 200` |
| Go 完整 CLI 构建 | blocked | 超过 3 分钟未完成 |
| `internal/source` 包测试 | blocked | 超过 2 分钟未完成 |

## 执行记录

### 1. 完整 CLI 构建

命令：

```bash
GOCACHE=/private/tmp/digcrawl-go-cache \
GOMODCACHE=/private/tmp/digcrawl-gomodcache \
go build -o /private/tmp/digcrawl-prod-test/digcrawl ./cmd/digcrawl
```

结果：

- 超过 3 分钟没有输出。
- 手动中止，exit code `130`。

影响：

- 无法用真实二进制执行 `digcrawl watchlist run --once` 或 `watchlist serve`。

### 2. Watchlist 核心包测试

命令：

```bash
GOCACHE=/private/tmp/digcrawl-go-cache \
GOPROXY=off \
go test ./internal/watchlist -count=1 -mod=readonly
```

结果：

- `ok github.com/originaleric/digcrawl/internal/watchlist 1.130s`
- 过程中仍有 macOS sandbox 下 `xcrun_db` cache warning，但测试通过。

### 3. Source 包测试

命令：

```bash
GOCACHE=/private/tmp/digcrawl-go-cache \
GOMODCACHE=/private/tmp/digcrawl-gomodcache \
go test ./internal/source -count=1 -mod=readonly
```

结果：

- 超过 2 分钟无输出。
- 手动中止，exit code `130`。

### 4. 头条指定文章真实 HTTP 探测

命令：

```bash
curl -L --max-time 25 \
  -A 'Mozilla/5.0 DigCrawl Production Test' \
  -D tests/2026-07-17/watchlist-prod-round-4/toutiao-article.headers \
  -o tests/2026-07-17/watchlist-prod-round-4/toutiao-article.html \
  'https://www.toutiao.com/w/1870280855070795/'
```

结果：

- `HTTP/2 200`
- HTML 大小：72,914 bytes
- Headers / HTML 已归档。
- `strings/rg` 未在 HTML 中稳定命中 `1870280855070795` 或 canonical article link，说明当前返回内容可能是动态页面或风控壳；固定文章 watchlist 代码路径理论上应走 URL 规范化，不依赖 HTML 链接发现，但本轮未能通过 Go probe 完整验证。

### 5. X / Elon Musk 指定帖子真实 HTTP 探测

命令：

```bash
curl -L --max-time 25 \
  -A 'Mozilla/5.0 DigCrawl Production Test' \
  -D tests/2026-07-17/watchlist-prod-round-4/x-elon-post.headers \
  -o tests/2026-07-17/watchlist-prod-round-4/x-elon-post.html \
  'https://x.com/elonmusk/status/1053390822991790083'
```

结果：

- `HTTP/2 200`
- HTML 大小：113,303 bytes
- 页面包含：
  - canonical URL: `https://x.com/elonmusk/status/1053390822991790083`
  - author: `elonmusk`
  - JSON-LD / Relay data 中的帖子 ID `1053390822991790083`
  - 正文：`Had to been done ur welcome`

### 6. SQLite Task Runtime DDL

命令：

```bash
sed -n '/^-- SQLite DDL$/,$p' \
  sqls/2026-07-16_通用Task调度RuntimeDDL.sql \
  > tests/2026-07-17/watchlist-prod-round-4/task-runtime-sqlite.sql

sqlite3 tests/2026-07-17/watchlist-prod-round-4/runtime-schema.db \
  < tests/2026-07-17/watchlist-prod-round-4/task-runtime-sqlite.sql

sqlite3 tests/2026-07-17/watchlist-prod-round-4/runtime-schema.db ".tables"
```

结果：

```text
dc_task_events
dc_task_locks
dc_task_runs
dc_tasks_runtime
```

## Findings

### P1: 完整 CLI / source 编译链路仍阻塞真实 Watchlist 生产执行

位置/命令：

- `go build ./cmd/digcrawl`
- `go test ./internal/source`
- `go run ./tests/2026-07-17/watchlist-prod-round-4/platform_normalize_probe.go`

复现：

- 使用可写 `GOCACHE` 和 `GOMODCACHE`。
- 完整 CLI build 超过 3 分钟无输出。
- `internal/source` 测试超过 2 分钟无输出。
- normalize probe 超过 90 秒无输出。

期望：

- 修复后应能在可接受时间内完成构建，进入真实 `digcrawl watchlist run --once` 或 `watchlist serve`。

实际：

- 本轮只能验证核心包、真实 HTTP 可达性和 SQLite runtime DDL，无法完成完整 watchlist 运行。

修复方向：

- 继续削减 `cmd/digcrawl` / `internal/source` 默认编译面，尤其检查是否仍被不必要的重依赖拖住。
- 为生产测试提供预编译二进制或稳定 Go module/cache 初始化步骤。
- 给 source/watchlist 增加更轻的集成测试入口，避免验证固定头条/X URL 时编译完整 collect/browser/DigFlow 链路。

### P2: 头条真实页面可达，但本轮无法证明固定文章 source probe 运行成功

位置：

- `tests/2026-07-17/watchlist-prod-round-4/toutiao-article.html`
- `tests/2026-07-17/watchlist-prod-round-4/platform_normalize_probe.go`

复现：

- 头条 URL 返回 `HTTP/2 200` 和 72,914 bytes HTML。
- HTML 内未稳定命中文章 ID / canonical article link。
- Go normalize probe 因编译超时未完成。

期望：

- 固定头条文章 watchlist source 能稳定归一到 `https://www.toutiao.com/article/1870280855070795/` 并进入后续 collect。

实际风险：

- 当前只证明网络可达，未证明本轮真实 Go source path 完整跑通。

修复方向：

- 先解决 Go 编译阻塞，再重跑 `internal/source` 固定头条文章测试或真实 watchlist dry-run。
- 若头条返回动态/风控壳，固定文章路径应优先依赖 URL 规范化和 `toutiao.article.read`，不要依赖页面 HTML 链接发现。

## 产物

- `tests/2026-07-17/watchlist-prod-round-4/config.watchlist.yaml`
- `tests/2026-07-17/watchlist-prod-round-4/toutiao-article.headers`
- `tests/2026-07-17/watchlist-prod-round-4/toutiao-article.html`
- `tests/2026-07-17/watchlist-prod-round-4/x-elon-post.headers`
- `tests/2026-07-17/watchlist-prod-round-4/x-elon-post.html`
- `tests/2026-07-17/watchlist-prod-round-4/task-runtime-sqlite.sql`
- `tests/2026-07-17/watchlist-prod-round-4/runtime-schema.db`
- `tests/2026-07-17/watchlist-prod-round-4/platform_normalize_probe.go`
- `tests/2026-07-17/2026-07-17-第4轮-Watchlist头条X生产级测试报告.md`

## 最终状态

`PARTIAL / BLOCKED`

本轮确认头条与 X 真实目标可达，核心 watchlist 包和 SQLite runtime schema 可用；但完整 CLI/source 编译仍阻塞真实 watchlist dry-run，因此还不能判定头条 + X 的完整生产链路通过。
