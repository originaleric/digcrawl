# DigCrawl

**面向内容采集、平台读取和 Agent 自动化的数据采集引擎。**

DigCrawl 的核心目标不是“提供一个网关”，而是把公众号、小红书、抖音、今日头条、X 和普通网页里的内容稳定采集下来，转成业务系统能直接消费的 Markdown、HTML、JSON、媒体文件和结构化元数据。Gateway、Collector、MCP、stdio、本地浏览器只是不同接入方式，业务主线始终是 **crawl / collect / normalize / export**。

它适合这些场景：

- 运营内容归档：把文章、笔记、视频页、网页资料批量保存到本地或业务系统
- 舆情与竞品观察：按 URL 队列持续采集平台内容，沉淀可检索记录
- Agent 工具能力：让云端 Agent、IDE 或自建编排系统安全调用本机浏览器和平台读取能力
- 私有环境采集：在用户电脑或内网机器上执行读取，利用本机登录态、Cookie 和网络环境
- 数据管道入口：把多平台内容统一成稳定 JSON，再进入后续清洗、分析、RAG 或审核流程

完整参数、配置和 API 细节见 [USAGE.md](USAGE.md)。

## 业务能力

| 能力 | 业务价值 |
|------|----------|
| 多平台内容读取 | 内置微信公众号、小红书、抖音、今日头条、X，以及任意 `http(s)` 网页读取 |
| 结构化内容抽取 | 输出标题、正文、作者、发布时间、互动数据、媒体列表、规范化 URL、平台诊断信息 |
| 静态解析/API 优先 + 浏览器兜底 | 微信公众号优先使用 HTTP HTML 静态解析；小红书、抖音、头条优先尝试页面上下文 API，失败后回落到浏览器 DOM / hydration / meta 提取 |
| 登录态复用 | 支持通过有头浏览器完成平台登录，后续采集复用本机 Cookie |
| 批量采集与收件箱 | 支持 URL 文件批处理、`collect-inbox` 收件箱和 `--watch` 轮询 |
| 多格式导出 | 同时导出 Markdown、HTML、JSON，可选下载媒体到 `.assets/` |
| 可检索持久化 | 支持 `index.json`、SQLite `collect.db` 或二者同时启用 |
| 宿主集成 | 可通过 HTTP Gateway、反向 Collector、MCP、stdio 或 Go SDK 接入业务系统 |
| 本机安全边界 | 默认本机监听、工具白名单、高风险工具审批、域名策略、审计日志和 Artifact 管理 |

## 支持的平台

| 平台 | 典型 URL | 内置工具 |
|------|----------|----------|
| 微信公众号 | `mp.weixin.qq.com` | `wechat.article.read` |
| 小红书 | `xiaohongshu.com`、`xhslink.com` | `xiaohongshu.note.read` |
| 抖音 | `douyin.com` | `douyin.video.read` |
| 今日头条文章 | `toutiao.com`、`m.toutiao.com` | `toutiao.article.read` |
| X | `x.com`、`twitter.com` | `x.post.read` |
| 普通网页 | 任意 `http(s)` URL | `browser.browse` |

平台读取会返回结构化错误，便于业务系统判断是否需要人工登录、重试或跳过，例如 `LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`、`CONTENT_TYPE_MISMATCH`、内容删除/私密、选择器失效等。

微信公众号公开文章会优先走 HTTP 静态解析，直接提取标题、作者、发布时间、正文、Markdown/HTML 和图片资源；只有网络瞬断或解析状态不确定时才进入浏览器兜底。若微信安全验证、频控、删除/私密页面或最终 URL 跳出微信域名，会返回结构化错误而不是导出占位内容。

今日头条读取面向公开文章页，先进行轻量 API 探测，再回落到浏览器 DOM / hydration / meta 提取；若 `toutiao.com/group/...` 实际跳转到西瓜、抖音或视频内容，会快速返回 `CONTENT_TYPE_MISMATCH`，不会作为文章导出。

## 快速开始

```bash
go build -o digcrawl ./cmd/digcrawl
go build -o digcollect ./cmd/digcollect
```

采集单个链接：

```bash
./digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,html,json
./digcrawl collect "https://www.toutiao.com/article/..." --format md,json
./digcrawl collect "https://example.com/article" --out ./collect-output
```

轻量采集入口：

```bash
./digcollect "https://mp.weixin.qq.com/s/..." --format md,json --out ./collect-output
```

批量采集：

```bash
./digcrawl collect --file urls.txt --format md,json --out ./collect-output
```

收件箱模式：

```bash
./digcrawl collect inbox
./digcrawl collect inbox --watch
```

平台登录与会话检查：

```bash
./digcrawl browser login --platform xiaohongshu
./digcrawl browser login --platform douyin
./digcrawl browser login --platform toutiao

./digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/..." --trial
```

## 输出结果

默认输出目录是 `collect-output/`。一次采集会按内容生成稳定目录，文件名会清理非法字符并按 UTF-8 边界截断，避免长中文标题在 macOS 等文件系统上产生非法路径。常见产物包括：

```text
collect-output/
  index.json
  <platform-or-web>/
    <slug>/
      content.md
      content.html
      content.json
      .assets/

storage/digcrawl/
  collect.db
  artifacts/
  browser_cookies/
```

JSON 会尽量统一字段，方便后续进入数据库、搜索索引或 Agent 上下文：

```json
{
  "platform": "toutiao",
  "title": "...",
  "text": "...",
  "author": {"name": "..."},
  "engagement": {"likes": 0, "comments": 0},
  "media": [],
  "canonical_url": "https://www.toutiao.com/article/...",
  "platform_metadata": {
    "read_channel": "api",
    "extraction_source": "api"
  }
}
```

## 接入方式

DigCrawl 可以只作为本地 CLI 使用，也可以嵌入到更大的业务系统中。

| 接入方式 | 适用场景 |
|----------|----------|
| `digcrawl collect` | 本地单次采集、批量采集、归档脚本 |
| `digcollect` | 只需要轻量采集入口时使用 |
| HTTP Gateway | 宿主服务通过 `/manifest`、`/tools/call` 调用本机工具 |
| Collector | 本机主动反向连接云端宿主，适合 NAT、内网和用户侧执行 |
| MCP | 接入 Cursor、Claude Desktop 等支持 MCP 的客户端 |
| stdio | 管道式、本地进程式集成 |
| Go SDK | 宿主侧直接调用远端 DigCrawl HTTP Gateway |

启动 HTTP Gateway：

```bash
./digcrawl gateway --config config/config.yaml
curl -s http://127.0.0.1:8787/manifest
```

连接业务宿主：

```bash
./digcrawl connect --server https://your-host.example.com --token <collector-token>
```

## 命令一览

| 命令 | 说明 |
|------|------|
| `digcrawl collect` | 采集 URL 并导出 |
| `digcrawl collect inbox` | 处理收件箱，支持 `--watch` |
| `digcrawl collect list` | 列出 SQLite 采集记录 |
| `digcrawl collect show` | 查看单条采集详情 |
| `digcrawl browser login` | 打开有头浏览器完成平台登录并保存 Cookie |
| `digcrawl browser check` | 检查平台会话，可选试读 URL |
| `digcrawl gateway` | HTTP 工具网关 |
| `digcrawl collector` | WebSocket 反向连接 Collector |
| `digcrawl connect` | 保存宿主配置并启动 Collector |
| `digcrawl login` | 保存 Collector Token |
| `digcrawl mcp` | MCP stdio 服务 |
| `digcrawl stdio` | 逐行 JSON 协议 |
| `digcrawl dev-host` | 本地参考宿主，仅开发调试 |
| `digcollect` | 轻量本地采集入口 |

## 架构

```text
collect/            URL 检测、采集编排、导出、索引、收件箱
internal/platform/  微信、小红书、抖音、头条、X 的平台读取实现
internal/browser/   本地 go-rod 浏览器、登录、快照、页面操作
provider/           platform、browser、example、stub 工具 Provider
runtime/            registry、policy、audit、artifact、统一执行入口
transport/          http、collector、host、mcp、stdio、devhost
protocol/           ToolManifest、ToolCall、ToolResult 线协议
client/             宿主侧 HTTP SDK
config/             默认策略、采集、浏览器、平台配置
```

## 安全与控制

DigCrawl 面向本机执行，默认配置只开放相对低风险的读取工具。涉及点击、输入、本地文件读取等高风险能力时，需要在白名单中启用，并在调用时显式携带审批标记。

默认安全控制包括：

- 仅本机监听的 HTTP Gateway
- 工具 allowlist
- `requires_user_approval` 高风险工具标记
- 域名 allow / block 策略
- Artifact 独立存储与下载入口
- 审计日志
- Collector token 鉴权

## 文档

| 文档 | 内容 |
|------|------|
| [USAGE.md](USAGE.md) | 安装、配置、CLI、Collect、API、宿主接入、FAQ |
| [collect/README.md](collect/README.md) | Collect 流程、输出目录和持久化说明 |
| [protocol/README.md](protocol/README.md) | ToolManifest、ToolCall、ToolResult |
| [runtime/README.md](runtime/README.md) | Runtime、策略、审计和 Artifact |
| [transport/README.md](transport/README.md) | HTTP、Collector、MCP、stdio 等传输层 |
| [config/config.yaml](config/config.yaml) | 配置示例 |

## 相关项目

- [digeino](https://github.com/originaleric/digeino) — Eino / DigFlow 工具与 Agent 能力库，可作为宿主或客户端调用 DigCrawl

## 许可证

MIT
