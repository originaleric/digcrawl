# DigGate

**A local tool gateway for any agent.**

DigGate 是一个面向任意 Agent 编排系统的本地工具网关，用于安全地暴露本地浏览器、本地文件、私有 API、企业内网和桌面能力。它与 Eino / DigFlow 解耦；[DigEino](https://github.com/originaleric/digeino) 可作为工具 Provider 接入。

## 快速开始

```bash
go run ./cmd/diggate gateway --config config/config.yaml
curl -s http://127.0.0.1:8787/manifest | jq .
```

## CLI

| 命令 | 说明 |
|------|------|
| `diggate gateway` | HTTP Tool Gateway（默认 `:8787`） |
| `diggate collector` | WebSocket 反向连接 Collector |
| `diggate mcp` | MCP stdio（Cursor / Claude Desktop） |
| `diggate stdio` | 逐行 JSON 协议 |
| `diggate dev-host` | 本地参考宿主（仅开发） |

## 架构

```text
protocol/     ToolManifest, ToolCall, ToolResult, wire
runtime/      registry, policy, audit, execute
transport/    http, collector, mcp, stdio  (gateway/*)
provider/     Provider 接口 + 示例工具
```

业务工具（浏览器、公众号、社媒采集等）通过 **Provider** 注册，默认内置 `example` Provider（`gateway.ping`、`file.read`）。完整工具集见 DigEino Provider（后续 `go get github.com/originaleric/digeino` 适配）。

## HTTP API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/manifest` | 工具清单 |
| POST | `/tools/call` | 执行工具 |
| GET | `/artifacts/{id}` | 下载 Artifact |

鉴权（可选）：`Authorization: Bearer <token>` 或 `X-DigGate-Token`

## MCP（Cursor）

```json
{
  "mcpServers": {
    "diggate": {
      "command": "diggate",
      "args": ["mcp", "--config", "/path/to/config.yaml"]
    }
  }
}
```

## 实现 Provider

```go
type Provider interface {
    Name() string
    Register(ctx context.Context, reg *registry.Registry) error
}
```

在启动时通过 `gateway.NewRegistry(cfg, gateway.RegistryOptions{Providers: []provider.Provider{...}})` 注入。

## 相关项目

- [digeino](https://github.com/originaleric/digeino) — Eino / DigFlow 工具 Provider
- `local-content-collector` / `digcollect` — 无宿主本地采集 CLI（规划中）

## License

MIT
