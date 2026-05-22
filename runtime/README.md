# 运行时（runtime）

DigCrawl **工具注册、策略校验、审计、Artifact 与统一执行入口**。

## 入口

| 函数 | 用途 |
|------|------|
| `NewRuntime` | HTTP Gateway / MCP / stdio 使用的完整运行时 |
| `NewCollectRuntime` | 本地 `collect` 命令（含平台 + 浏览器 Provider） |
| `NewCollectorRuntime` | WebSocket Collector 反向连接 |

装配逻辑见 [`bootstrap.go`](bootstrap.go)：按配置加载 `provider/platform`、`browser`、`example`、`stub` 等。

## 子包

| 目录 | 说明 |
|------|------|
| `registry/` | 工具注册表与 handler 绑定 |
| `policy/` | URL 域名、路径、工具白名单策略 |
| `audit/` | 调用审计日志 |
| `artifact/` | 截图等大对象存储 |
| `executor/` | 请求解码、Cookie 域校验等辅助 |
| `version/` | 构建版本信息 |

使用说明见 [USAGE.md](../USAGE.md)。
