# DigCrawl Bridge 残留 Owner 直接启动恢复生产模拟测试报告（第 3 轮）

## 1. 测试结论

- 结论：通过
- 项目：DigCrawl（Go 本地单机单 profile Bridge）
- 测试时间：2026-09-24
- 测试人：Codex
- 本轮核心输出要求：直接执行 `bridge serve` 时，若旧 Bridge 已释放生命周期锁但 owner 记录还指向仍存活的私有进程组，必须阻止替代实例；旧组结束后，同一条直接启动路径必须能立即恢复，不能等待自己已经持有的锁。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；本地 Unix socket Bridge |
| 启动入口 | `cmd/digcrawl bridge serve` / `transport/bridge.Serve` |
| 测试命令 | `go test`, `go vet` |
| 配置文件 | 测试临时 profile，仅允许 `gateway.ping` |
| 依赖服务 | 无外部网络、浏览器或账户依赖 |
| 核心业务输出 | Bridge 的 `/manifest` 可通过本地 Unix socket 读取 |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| Go 测试环境 | configured | 本地完整测试通过。 |
| Unix socket 权限 | configured | 测试使用受限目录；真实监听场景使用短 `/tmp` 路径，避免 macOS socket 地址长度限制。 |
| 外部网页/Chrome | skippable | 本轮验证的是 Bridge 启动恢复门，不启动浏览器也不访问外部平台。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 残留组启动门回归 | `go test ./transport/bridge -run 'Test(StartWaitsForRecordedBridgeGroupAfterOwnerLockRelease|ServeRecoversAfterRecordedBridgeGroupStops)$' -count=3` | 通过 | 连续 3 次确认：旧组存活时 `Serve` 返回有限 deadline；启动检查本身不删除 owner 记录；旧组终止并回收后，直接 `Serve` 对 `/manifest` 就绪并原子发布新的 owner 记录。 |
| 全量回归 | `go test -p 1 ./...` | 通过 | 所有包通过。 |
| 静态检查 | `go vet ./...` | 通过 | 无诊断。 |
| 补丁格式检查 | `git diff --check` | 通过 | 无空白错误。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 活动旧组保护 | 活动旧组不得被新 Bridge 绕过 | 直接 `Serve` 在 150ms 测试预算内返回 deadline | 通过 |
| 旧组结束恢复 | 旧组不再运行后可直接启动 | `Serve` 建立 listener，`/manifest` 可读 | 通过 |
| 新 owner 身份 | 恢复实例应取代过期记录 | owner 记录被当前 `Serve` 重新发布，结束时正常清理 | 通过 |
| stop 语义 | stop 仍要同时等锁与被终止组退出 | `waitForBridgeGroupStopped` 保持组合检查 | 通过 |

## 6. Findings

本轮已修复并复测以下 P2：残留 owner 恢复函数把“前任组已退出”和“生命周期锁已释放”绑定检查，导致直接 `Serve` 在持有新 owner 锁后等待自己，最终超时。

修复将两种职责显式分开：`waitForBridgeGroupStopped` 仍用于 stop 的锁加组确认；残留记录恢复改用 `waitForBridgeGroupRelease`，只检查经调用方确认属于前任的私有进程组。复核中还发现普通 MCP 启动检查不持有生命周期锁，若它删除 owner 文件，可能与并发直接 `Serve` 的新记录发布竞争；因此恢复检查保持只读，只有持有生命周期锁的 `Serve` 可以原子替换记录。本轮未发现新的阻塞性问题。

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| 已在旧版本中进入内核不可中断状态的历史进程 | 其运行代码不含本修复，不能用新二进制追溯回收 | 由 macOS 回收后再做真实平台抓取；新版正常 stop/restart 不依赖该恢复方式。 |
| 真正的进程/Chrome 树 | 本轮使用隔离的 `sleep` 进程组验证启动门 | 第 2 轮已覆盖真实 Bridge restart 与子树终止；后续改动应保留两类测试。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-09-24/2026-09-24-第3轮-Bridge残留Owner直接启动恢复生产模拟测试报告.md` | 第 3 轮生产模拟证据。 |
| 回归测试 | `transport/bridge/owner_unix_test.go` | 活动旧组阻止、旧组释放后直接启动成功。 |

## 9. 最终说明

本轮满足“直接启动不自锁，同时不绕过仍在运行的旧进程组”的单机生命周期要求。状态源仍只有生命周期锁；owner 文件只是崩溃窗口中的前任进程组恢复记录，没有引入 supervisor、心跳或平滑交接。
