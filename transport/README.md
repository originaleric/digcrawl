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

## 宿主 HTTP 客户端

业务宿主若需**主动请求**用户本机 Gateway（而非 Collector 反向连接），请使用仓库顶层 [`client/`](../client/) 包，而非 `transport` 下的实现。

完整说明见 [USAGE.md](../USAGE.md)。
