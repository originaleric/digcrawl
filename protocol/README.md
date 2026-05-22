# 协议层（protocol）

DigCrawl 工具网关的**线协议与类型定义**，与传输形态（HTTP / WebSocket / MCP / stdio）无关。

## 主要内容

| 类型 / 概念 | 说明 |
|-------------|------|
| `ToolManifest` | 工具清单：名称、描述、输入 schema、能力标签、风险等级 |
| `ToolCall` | 宿主或 Agent 发起的工具调用请求 |
| `ToolResult` | 执行结果：输出 JSON、错误、Artifact 引用 |
| Collector 消息 | WebSocket 反向连接上的心跳、拉取、回传等线格式 |

## 相关代码

- [`types.go`](types.go) — 结构体定义
- [`wire.go`](wire.go) — JSON 序列化与消息类型常量

使用说明见仓库根目录 [USAGE.md](../USAGE.md)。
