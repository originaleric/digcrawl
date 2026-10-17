# 2026-09-24-第4轮-Bridge单锁私有Worker生命周期生产模拟测试报告

## 1. 测试结论

- 结论：通过
- 项目：DigCrawl（Go，本地单人单机 MCP Bridge）
- 测试时间：2026-09-24
- 测试人：Codex
- 本轮核心输出要求：生命周期必须收敛为一个 profile 一把锁、一个受管 worker 进程组；不能保留 transition lock、平滑交接或多入口启动。停止必须直接终止当前 worker，不能在旧记录或 PID/PGID 重用时误杀无关进程，也不能将仍在退出的组误报为已停止。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；本地 Unix socket MCP Bridge |
| 正常启动入口 | `digcrawl mcp`；`bridge serve` 仅接受内部 `--managed-worker` 调用 |
| 显式运维入口 | `digcrawl bridge stop`、`digcrawl bridge restart` |
| 测试命令 | `go test`、`go test -race`、`go vet` |
| 配置文件 | 临时单 profile，仅启用 `gateway.ping` |
| 外部依赖 | 本轮无需网页、账户、Chrome 或网络服务 |
| 核心业务输出 | 一个 profile 仅有一个可通过私有 Unix socket 读取 `/manifest` 的 worker |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| Go 本地测试环境 | configured | 全仓串行回归、race 与 vet 均可执行。 |
| Unix 进程组 | configured | macOS 测试以 `setsid` 私有 worker 和受控 `sleep` 子进程验证。 |
| Windows 运行环境 | skippable | 当前主机为 macOS；已完成 Windows 交叉编译，Windows 继续使用精确进程树终止。 |
| 外部平台/Chrome | skippable | 本轮只验证生命周期内核，不把网络或登录状态混入结果。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 禁止手工共享组 worker | `go test ./cmd/digcrawl -run 'TestBridgeServeRejectsUnmanagedForegroundWorker$' -count=5` | 通过 | 直接 `bridge serve` 被拒绝；只有 `NewExecutor` 创建的 `setsid` worker 可运行。 |
| 多个 MCP 同时启动 | `go test ./cmd/digcrawl -run 'TestConcurrentExecutorsLeaveOneManagedBridgeWorker$' -count=5` | 通过 | 12 个并发启动者完成后，进程表中同一 socket 恰好一个 worker。 |
| 直接 stop/restart 和子树释放 | `go test ./cmd/digcrawl -run 'TestBridgeRestartReplacesRunningBridge$' -count=5`；`go test ./transport/bridge -run 'TestStopKillsExactBridgeProcessGroupAndChild$' -count=8` | 通过 | restart 更换 owner PID；stop 一次 SIGKILL 覆盖 worker 与模拟 Chrome 子进程。 |
| 父进程先退出的残留组 | `go test ./transport/bridge -run 'TestStopWaitsForRecordedGroupAfterBridgeParentIsGone$' -count=8` | 通过 | stop 持有同一把锁等待组释放，超时返回失败，绝不把活动组当作已停止。 |
| 陈旧记录安全性 | `go test ./transport/bridge -run 'TestStopDoesNotSignalStaleOwnerRecord$' -count=8` | 通过 | 记录 PID 不再是目标 socket 的 Bridge 时，stop 不向该 PGID 发信号。 |
| 竞态检查 | `go test -race ./transport/bridge ./cmd/digcrawl` | 通过 | 无数据竞争诊断。 |
| 全量回归 | `go test -p 1 ./...` | 通过 | 所有包通过。 |
| 静态与跨平台检查 | `go vet ./...`；`GOOS=windows GOARCH=amd64 go test -c ./transport/bridge`；`git diff --check` | 通过 | 无 vet、交叉编译或空白错误。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 生命周期状态源 | 每个 profile 只有一个可竞争的状态源 | 仅 `<socket>.lock`；不再创建 `.transition.lock` | 通过 |
| 正常启动入口 | 不允许手工/共享进程组绕过所有权模型 | CLI 拒绝未标记的 `bridge serve`；正常 `mcp` 路径创建私有 worker | 通过 |
| 并发启动 | 不残留多个 Bridge/Chrome 树 | 12 个并发 caller 稳定后仅一个 socket worker | 通过 |
| stop 语义 | 当前 owner 直接终止；退出未完成不得放行替代实例 | 同锁覆盖“信号发送”和“组释放等待” | 通过 |
| 陈旧恢复记录 | 可等待资源释放，不能成为无条件 kill target | 信号前核对 PID、socket 命令和 PGID；不匹配时有限失败 | 通过 |

## 6. Findings

本轮未发现阻塞性问题。

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| 已由旧版本创建且处于内核不可中断状态的历史进程 | 新代码无法让内核回收一个不可中断的旧进程 | 新版不会将此状态误报为 stop 成功；若操作系统本身永不回收，命令会在 deadline 失败而不是启动第二个 worker。 |
| Windows 真机树终止 | 本轮主机为 macOS | 在 Windows 发布前补一次真机 `taskkill /T /F` 验收；不影响本次 macOS 主路径结论。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-09-24/2026-09-24-第4轮-Bridge单锁私有Worker生命周期生产模拟测试报告.md` | 本轮生产模拟证据。 |
| 生命周期实现 | `transport/bridge/bridge.go`、`transport/bridge/owner_unix.go`、`transport/bridge/worker_unix.go` | 单锁、受管 worker、只读恢复门。 |
| 入口和集成测试 | `cmd/digcrawl/bridge_cmd.go`、`cmd/digcrawl/mcp_stdio_test.go` | 入口约束与 12 并发启动验证。 |
| 使用说明 | `README.md` | 更新后的单锁生命周期说明。 |

## 9. 最终说明

本轮用操作系统已有的单实例锁和私有进程组完成单机单用户生命周期：没有自建状态机、transition lock、心跳、租约、控制端点或平滑交接。停止是直接停；无法确认资源已释放时是有限失败，不以“猜测 PID 仍属于旧 Bridge”的方式继续补救。当前实现满足本轮简单性和可验证性要求。
