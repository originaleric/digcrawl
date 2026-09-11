# 传输层（transport）

将 `runtime` 暴露为多种**对外接入方式**，供宿主、IDE 或脚本使用。

## 子包

| 包 | 说明 | 对应 CLI |
|----|------|----------|
| `http` | HTTP 工具网关：`/health`、`/manifest`、`/tools/call` | `digcrawl gateway` |
| `collector` | WebSocket 反向连接，执行宿主下发的工具调用 | `digcrawl collector` |
| `host` | 可复用宿主侧 Collector WebSocket 端点，支持 push / pull 队列 | 宿主项目 import |
| `mcp` | MCP stdio 服务（Cursor、Claude Desktop 等） | `digcrawl mcp` |
| `stdio` | stdin/stdout 逐行 JSON 协议 | `digcrawl stdio` |
| `devhost` | 基于 `host` 思路的最小参考宿主（仅本地开发调试） | `digcrawl dev-host` |

## MCP 与 Agent Skill

`digcrawl mcp --config <profile>` 通过 stdio 暴露 MCP。仓库级 Skill 位于 [`../.agents/skills/digcrawl/`](../.agents/skills/digcrawl/)，并提供 Codex、Claude Code 配置示例及最小 profile [`../config/digcrawl-skill.example.yaml`](../config/digcrawl-skill.example.yaml)。该 profile 默认只暴露链接读取工具；Cookie、artifact 与登录态均由用户本机目录保存。

标准 MCP stdio 会话会保持到客户端关闭 stdin。未显式传 `--idle-timeout` 时，`digcrawl mcp` 仅回收尚未发送 MCP `initialize` 且 stdin 连续空闲 1 分钟的管道；收到 Codex 的 initialize 后会自动启用 15 分钟空闲回收，其他已初始化客户端保持标准会话。正值 `--idle-timeout` 为任意宿主显式启用回收，`--idle-timeout=0` 明确关闭回收；`codex/config.toml.example` 已采用显式选项。

## 宿主 HTTP 客户端

业务宿主若需**主动请求**用户本机 Gateway（而非 Collector 反向连接），请使用仓库顶层 [`client/`](../client/) 包，而非 `transport` 下的实现。

完整说明见 [USAGE.md](../USAGE.md)。
