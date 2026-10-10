# 传输层（transport）

将 `runtime` 暴露为多种**对外接入方式**，供宿主、IDE 或脚本使用。

## 子包

| 包 | 说明 | 对应 CLI |
|----|------|----------|
| `http` | HTTP 工具网关：`/health`、`/manifest`、`/tools/call` | `digcrawl gateway` |
| `collector` | WebSocket 反向连接，执行宿主下发的工具调用 | `digcrawl collector` |
| `host` | 可复用宿主侧 Collector WebSocket 端点，支持 push / pull 队列 | 宿主项目 import |
| `mcp` | MCP stdio 服务（Cursor、Claude Desktop 等） | `digcrawl mcp` |
| `bridge` | 按 profile 共享 Runtime 与 Chrome 的私有本机服务 | MCP 按需启动；`digcrawl bridge stop/restart` 管理 |
| `stdio` | stdin/stdout 逐行 JSON 协议 | `digcrawl stdio` |
| `devhost` | 基于 `host` 思路的最小参考宿主（仅本地开发调试） | `digcrawl dev-host` |

## MCP 与 Agent Skill

`digcrawl mcp --config <profile>` 通过 stdio 暴露 MCP。仓库级 Skill 位于 [`../.agents/skills/digcrawl/`](../.agents/skills/digcrawl/)，并提供 Codex、Claude Code 配置示例及最小 profile [`../config/digcrawl-skill.example.yaml`](../config/digcrawl-skill.example.yaml)。该 profile 默认只暴露链接读取工具；Cookie、artifact 与登录态均由用户本机目录保存。

MCP stdio 子进程只转发到同 profile 的私有 Bridge，Runtime 与浏览器由 Bridge 持有。Bridge 不因空闲退出；未初始化的 stdio 管道在连续空闲 1 分钟后回收，任意已初始化宿主在 15 分钟无实际 `tools/call` 后回收，`ping` 不会续租。正值 `--idle-timeout` 覆盖后一个间隔，`--idle-timeout=0` 明确关闭回收。

客户端先校验 manifest 的 `build_id` 与 Runtime 版本，构建不匹配时要求关闭旧 MCP 连接、用已安装二进制执行 `digcrawl bridge restart --config <实际 profile>` 后重新连接。doctor 通过已有 Bridge 检查实际浏览器操作，不另起浏览器。

浏览器读取层拥有总预算和最多一次故障重试，平台 fallback 共用该预算。Collector 不重放浏览器相关整项 handler；MCP 保留 `error` 的 code/message 契约，并通过同级 `error_details` 交付 `handled`、故障阶段、恢复结果、`next_actions` 和诊断编号。带 progress token 的调用会收到开始和结束通知。具体恢复步骤见 [USAGE.md](../USAGE.md#浏览器超时或反复失败)。

## 宿主 HTTP 客户端

业务宿主若需**主动请求**用户本机 Gateway（而非 Collector 反向连接），请使用仓库顶层 [`client/`](../client/) 包，而非 `transport` 下的实现。

完整说明见 [USAGE.md](../USAGE.md)。
