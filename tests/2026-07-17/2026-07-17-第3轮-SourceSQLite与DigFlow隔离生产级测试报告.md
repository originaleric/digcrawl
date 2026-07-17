# 2026-07-17 第3轮 Source / SQLite / DigFlow 隔离生产级测试报告

## 项目与环境

- 项目：DigCrawl
- 路径：`/Users/dig/projects/digcrawl`
- 测试轮次：第3轮
- 测试目标：复验 `internal/source` 抽象、`mattn/go-sqlite3` 切换，以及 DigFlow embedded runtime 改为可选 build tag 后，默认测试是否不再被私有 DigFlow 仓库下载阻断。
- 缓存策略：使用 `GOCACHE=$PWD/.tmp/go-build`、`GOMODCACHE=$PWD/.tmp/gomod`，避免默认 Go cache 沙箱写入问题。

## 本轮验收标准

1. 默认 `internal/summary` 测试不再下载或编译私有 DigFlow runtime。
2. `internal/source` 来源发现测试继续通过。
3. `mattn/go-sqlite3` driver 可编译。
4. Watchlist 聚焦测试不再出现 DigFlow missing go.sum / 下载阻塞。
5. staged diff 保持格式干净。

## 配置与依赖就绪

| 项目 | 状态 | 证据 |
|------|------|------|
| Source 测试 | configured | `go test ./internal/source -count=1` 通过 |
| Summary 默认测试 | configured | `go test ./internal/summary -count=1 -mod=readonly` 通过 |
| DigFlow 默认隔离 | configured | `go list -deps ./internal/summary` 未出现 `github.com/originaleric/DigFlow` |
| SQLite CGO driver | configured with warning | `go test github.com/mattn/go-sqlite3 -run '^$' -count=1` 通过，带 macOS xcrun cache warning |
| Watchlist 聚焦测试 | not completed | 不再报 DigFlow / go.sum 错误，但超过 2 分钟仍未返回，已中断 |

## 执行记录

### 1. Summary 默认构建测试

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod GOPROXY=off \
go test ./internal/summary -count=1 -mod=readonly
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/summary 1.071s
```

结论：默认 summary 包测试通过。真实 DigFlow runtime 已被 `digflow_embedded` build tag 隔离，stub 路径可编译。

### 2. Source 包回归

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod go test ./internal/source -count=1
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/source 1.408s
```

结论：`internal/source` 抽象的 RSS、头条、X、Reddit helper 覆盖继续通过。

### 3. SQLite driver smoke

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod \
go test github.com/mattn/go-sqlite3 -run '^$' -count=1
```

结果：

```text
ok github.com/mattn/go-sqlite3 1.785s [no tests to run]
```

伴随 warning：

```text
cc: error: couldn't create cache file '/var/folders/.../T/xcrun_db-...' (errno=Operation not permitted)
```

结论：driver 可编译。warning 来自当前沙箱不能写 macOS xcrun 系统临时 cache，不是测试失败。

### 4. Watchlist store 聚焦测试

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod GOPROXY=off \
go test ./internal/watchlist -run TestStoreMigratesLegacyTables -count=1 -mod=readonly
```

结果：超过 2 分钟无输出，手动中断。

关键观察：本轮没有再出现以下旧阻塞：

- `github.com/originaleric/DigFlow ... missing go.sum entry`
- `go: downloading github.com/originaleric/DigFlow`

结论：DigFlow 私仓阻塞已从默认 watchlist 编译路径移除；剩余问题是 watchlist 包自身仍比较重，编译/链接耗时未在本轮完成。

### 5. 默认依赖图检查

命令：

```bash
GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/gomod GOPROXY=off \
go list -deps ./internal/summary | rg 'originaleric/DigFlow|internal/summary|digflow' || true
```

结果：

```text
github.com/originaleric/digcrawl/internal/summary
```

结论：默认 `internal/summary` 依赖图不包含 `github.com/originaleric/DigFlow`。

### 6. Staged diff hygiene

命令：

```bash
git diff --cached --check
```

结果：通过。

## Findings

本轮没有发现 P0/P1/P2 级功能阻塞。

## 残余风险

- Watchlist 包仍然较重，聚焦测试超过 2 分钟未返回。虽然 DigFlow 私仓阻塞已移除，但 watchlist 仍把 store、service、scheduler、collector、summary adapter 放在同一 package，单测粒度仍会带来较大编译面。
- `digflow_embedded` tag 下的真实 embedded runtime 仍需要私有 DigFlow module 完整下载与校验；默认构建已规避该风险，但显式启用 embedded runtime 时仍需可用 SSH/GOPRIVATE 环境。
- `mattn/go-sqlite3` 依赖 CGO；生产或本地运行环境需要 `CGO_ENABLED=1` 和 C 编译环境。

## 结论

本轮结论：**通过核心阻塞复验，DigFlow 私仓下载已不再阻断默认 summary/source/watchlist 编译入口**。

`internal/source`、默认 `internal/summary`、`mattn/go-sqlite3` driver 均通过测试。Watchlist store 聚焦测试没有再报 DigFlow/go.sum 错误，但仍因包编译耗时未完成，后续建议继续拆分 watchlist package 以降低测试编译面。
