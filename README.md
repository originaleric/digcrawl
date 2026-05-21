# DigGate

**面向任意 Agent 的本地工具网关。**

DigGate 在用户本机安全暴露浏览器、本地文件、私有 API 与企业内网能力，供云端 Agent 宿主、IDE（MCP）或自定义编排器调用。它与 [DigEino](https://github.com/originaleric/digeino) / Eino 解耦：Collector、Gateway、MCP、stdio、本地浏览器和平台采集能力均由 DigGate 独立承载；DigEino 如需本地能力，可作为宿主或客户端调用 DigGate。

📖 **完整使用说明：[USAGE.md](USAGE.md)**

---

## 快速开始

```bash
go test ./...
go build -o diggate ./cmd/diggate
go build -o digcollect ./cmd/digcollect

# 启动 HTTP 网关（默认 127.0.0.1:8787）
./diggate gateway --config config/config.yaml

# 查看工具清单
curl -s http://127.0.0.1:8787/manifest | jq .
```

连接你的业务宿主（本机 Collector 反向 WebSocket）：

```bash
./diggate connect --server https://your-host.example.com --token <token>
```

本地采集一篇文章：

```bash
./diggate collect "https://mp.weixin.qq.com/s/..."
```

也可以使用轻量采集入口：

```bash
./digcollect "https://mp.weixin.qq.com/s/..." --format md,json --out ./collect-output
```

---

## 能做什么

| 能力 | 说明 |
|------|------|
| **HTTP Gateway** | `GET /manifest`、`POST /tools/call`、Artifact 下载 |
| **Collector** | 本机主动连接宿主，执行下发的工具调用 |
| **Host Endpoint** | 宿主侧可复用 `transport/host`，支持 Collector push / pull |
| **MCP / stdio** | 对接 Cursor、Claude Desktop 或管道式宿主 |
| **Collect** | 无宿主落盘：公众号 / 小红书 / 抖音 / X / 任意 `http(s)` 网页 → Markdown / HTML / JSON |
| **Provider 扩展** | 平台、浏览器、示例与自定义 Provider |
| **安全边界** | 默认本机监听、工具白名单、高风险工具审批、域名策略、审计与 Artifact |

---

## 命令一览

| 命令 | 说明 |
|------|------|
| `diggate gateway` | HTTP 工具网关 |
| `diggate collector` | WebSocket 反向连接 Collector |
| `diggate connect` | 保存宿主配置并启动 Collector |
| `diggate login` | 保存 Collector Token |
| `diggate mcp` | MCP stdio（IDE） |
| `diggate stdio` | 逐行 JSON 协议 |
| `diggate dev-host` | 本地参考宿主（仅开发） |
| `diggate collect` | 采集 URL 并导出 |
| `diggate collect inbox` | 收件箱批处理 / `--watch` 轮询 |
| `digcollect` | 轻量本地采集入口，等价于 `diggate collect` |

参数、配置项、API 与接入方式见 **[USAGE.md](USAGE.md)**。

---

## 架构

```text
protocol/           ToolManifest、ToolCall、ToolResult
runtime/            registry、policy、audit、artifact、bootstrap
transport/          http · collector · host · mcp · stdio · devhost
client/             宿主侧 HTTP SDK
collect/            检测、执行、导出、索引、收件箱
provider/           platform · browser · example · stub
internal/platform/  各平台采集实现
internal/browser/   本地 go-rod 浏览器
```

---

## 内置工具（摘要）

- **平台**：`wechat.article.read`、`xiaohongshu.note.read`、`douyin.video.read`、`x.post.read`
- **浏览器**：`browser.browse`、`browser.snapshot`、`browser.action`
- **示例**：`gateway.ping`、`file.read`

完整列表与请求字段见 `GET /manifest` 或 [USAGE.md §9](USAGE.md#9-内置工具清单)。

默认配置只开放 `gateway.ping`、平台读取、`browser.browse`、`browser.snapshot` 等相对低风险工具。`browser.action`、`file.read` 等标记 `requires_user_approval` 的工具需要显式加入白名单，并在 `ToolCall` 中携带 `"approved": true`。

---

## 文档

| 文档 | 内容 |
|------|------|
| [USAGE.md](USAGE.md) | 安装、配置、CLI、Collect、API、宿主接入、FAQ |
| [updates/](updates/) | 按日期的变更摘要 |
| [collect/README.md](collect/README.md) | Collect 流程与输出目录 |
| [protocol/README.md](protocol/README.md) · [runtime/README.md](runtime/README.md) · [transport/README.md](transport/README.md) | 各层中文说明 |
| [config/config.yaml](config/config.yaml) | 配置示例 |
| [.github/workflows/ci.yml](.github/workflows/ci.yml) | CI：测试并构建 `diggate` / `digcollect` |

---

## 相关项目

- [digeino](https://github.com/originaleric/digeino) — Eino / DigFlow 工具与 Agent 能力库，可作为宿主或客户端调用 DigGate

## 许可证

MIT
