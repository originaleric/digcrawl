# DigGate Gateway

宿主对接指南：通过统一 `ToolManifest` / `ToolCall` / `ToolResult` 协议暴露本地工具。

## 连接模式

| 模式 | 命令 | 适用场景 |
|------|------|----------|
| HTTP Gateway | `diggate gateway` | 内网服务、云端调本地网关 |
| WebSocket Collector | `diggate collector` | 本机无公网 IP，反向连接云端 |
| MCP (stdio) | `diggate mcp` | Cursor / Claude Desktop |
| stdio JSON | `diggate stdio` | CLI、桌面子进程 |
| Go Client SDK | `gateway/client` | 宿主 Go 项目 import |

## Provider

业务工具通过 `provider.Provider` 注册，DigGate 核心不依赖 DigEino。示例见 `provider/example`。

DigEino 工具包将作为独立 Provider 发布（`github.com/originaleric/digeino`）。
