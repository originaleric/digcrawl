# 2026-07-17 第2轮 Source 与 SQLite 切换生产级测试报告

## 项目与环境

- 项目：DigCrawl
- 路径：`/Users/dig/projects/digcrawl`
- 测试目标：验证 `internal/source` 抽象、Watchlist SQLite driver 从 `glebarez/go-sqlite` 切到 `mattn/go-sqlite3`、DigFlow 最新 commit pin 后的可运行状态。
- Go cache：使用仓库/临时目录下可写 cache，避免默认 `~/Library/Caches/go-build` 沙箱写入问题。
- 当前限制：DigFlow 是私有仓库，SSH auth 可用，但新版本 module download 未完成，`go.sum` 尚未生成新版本 checksum。

## 本轮验收标准

1. `internal/source` 抽象后的来源发现单测可运行。
2. `mattn/go-sqlite3` driver 本机可编译，确认不再依赖 `modernc.org/sqlite` 作为 Watchlist 默认 SQLite driver。
3. Watchlist store 相关测试至少能进入执行或给出明确阻塞原因。
4. DigFlow 最新版本 pin 与 SSH 可访问性要有证据。

## 配置与依赖就绪

| 项目 | 状态 | 证据 |
|------|------|------|
| Go source 测试 | configured | `go test ./internal/source -count=1` 通过 |
| SQLite CGO driver | configured with warning | `go test github.com/mattn/go-sqlite3 -run '^$' -count=1` 通过；macOS `cc` 输出 xcrun cache 写入 warning |
| DigFlow SSH auth | configured | `git ls-remote git@github.com:originaleric/DigFlow.git HEAD` 返回 `6782526df508...` |
| DigFlow module checksum | missing | `go.sum` 仍只有旧 `8e77...` checksum |
| Watchlist package test | blocked | `internal/summary` 依赖 DigFlow 新版本，缺少 go.sum entry |

## 执行记录

### 1. Source 包回归

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod go test ./internal/source -count=1
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/source 1.149s
```

结论：`internal/source` 抽象后的 RSS、头条、X、Reddit helper 相关测试通过。

### 2. mattn/go-sqlite3 driver 编译 smoke

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod go test github.com/mattn/go-sqlite3 -run '^$' -count=1
```

结果：

```text
ok github.com/mattn/go-sqlite3 1.543s [no tests to run]
```

同时出现 warning：

```text
cc: error: couldn't create cache file '/var/folders/.../T/xcrun_db-...' (errno=Operation not permitted)
```

结论：`mattn/go-sqlite3` 可以编译通过；warning 来自当前沙箱无法写 macOS xcrun 系统临时 cache，不是 driver 编译失败。

### 3. Watchlist store 聚焦测试

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod GOPROXY=off go test ./internal/watchlist -run TestStoreMigratesLegacyTables -count=1 -mod=readonly
```

结果：

```text
FAIL github.com/originaleric/digcrawl/internal/watchlist [setup failed]
collect/store/sqlite.go:13:2: github.com/originaleric/DigFlow@v1.1.2-0.20260715172040-6782526df508: missing go.sum entry for go.mod file
internal/summary/digflow_runtime.go:13:2: missing go.sum entry for module providing package github.com/originaleric/DigFlow/pkg/digflow
```

结论：Watchlist 包级测试仍被 DigFlow 私有 module 新版本 checksum 缺失阻断。这个阻断发生在包编译前置阶段，尚未执行 store 测试逻辑。

### 4. DigFlow SSH 可访问性

命令：

```bash
GIT_SSH_COMMAND='ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15' git ls-remote git@github.com:originaleric/DigFlow.git HEAD
```

结果：

```text
6782526df508267d0b63b14a8df9929c49b58bb2 HEAD
```

结论：SSH auth 可用，当前 main HEAD 与 go.mod pin 的 commit 一致。

### 5. Staged diff hygiene

命令：

```bash
git diff --cached --check
```

结果：通过。

## Findings

### [P1] Watchlist 包级测试被 DigFlow 新版本 go.sum 缺失阻断

- 位置：`go.mod` / `go.sum` / `internal/summary/digflow_runtime.go`
- 复现：运行 `go test ./internal/watchlist -run TestStoreMigratesLegacyTables -count=1 -mod=readonly`
- 期望：Watchlist 包能够完成编译并执行 store 迁移测试。
- 实际：Go 在编译 `internal/summary` 时发现 DigFlow 新版本缺少 go.sum entry。
- 影响：当前无法完成 Watchlist + SQLite driver 的包级生产验证；CI 若使用 `-mod=readonly` 也会失败。
- 建议：完成 `go mod download github.com/originaleric/DigFlow`，生成 `v1.1.2-0.20260715172040-6782526df508` 对应 go.sum entry；如下载仍慢，考虑通过本地已授权 clone 生成 module cache，或在 CI/本机配置持久化 SSH module cache。

## 输出质量评估

- `internal/source` 抽象：通过本轮可运行测试。
- SQLite driver 切换：`mattn/go-sqlite3` driver 可编译，代码层已切换到 `sqlite3` driver name。
- Watchlist 完整链路：未通过，阻塞点是 DigFlow 新版本 checksum，而不是 SQLite driver。

## 残余风险

- `go.sum` 尚未包含 DigFlow 最新版本 checksum，后续构建仍可能触发私有仓库下载。
- `mattn/go-sqlite3` 依赖 CGO；本机需要 `CGO_ENABLED=1` 与可用 C 编译环境。
- Watchlist 包仍把 store、summary、scheduler、source 放在同一个 Go package，单测任一子能力都会编译 DigFlow 和 SQLite 相关依赖，测试粒度仍偏重。

## 结论

本轮结论：**部分通过，Watchlist 包级验证被 DigFlow go.sum 缺失阻断**。

已确认 `internal/source` 和 `mattn/go-sqlite3` 基础能力可用；下一步应先解决 DigFlow 最新版本的 module download / go.sum 生成问题，再重跑 Watchlist store 与调度测试。
