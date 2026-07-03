# DigCrawl 使用指南

DigCrawl 是面向任意 Agent 编排系统的**本地工具网关**：在用户机器上安全暴露浏览器、本地文件、私有 API 等能力，供云端宿主或 IDE 通过 HTTP、WebSocket、MCP、stdio 调用。

本文档覆盖安装、配置、全部 CLI 命令、Collect 采集、宿主接入与扩展开发。**快速概览见 [README.md](README.md)。**

---

## 1. 安装与构建

### 环境要求

- Go 1.24+（以 `go.mod` 为准）
- 使用浏览器类工具时需本机 Chrome/Chromium（go-rod 可自动查找，也可在配置中指定 `ChromePath`）

### 从源码运行

```bash
git clone https://github.com/originaleric/digcrawl.git
cd digcrawl
go build -o digcrawl ./cmd/digcrawl
go build -o digcollect ./cmd/digcollect
```

### 安装到 PATH（可选）

```bash
go install ./cmd/digcrawl
go install ./cmd/digcollect
# 二进制通常在 $GOPATH/bin 或 $HOME/go/bin
```

### 配置文件位置

| 场景 | 默认路径 |
|------|----------|
| `gateway` / `collector` / `mcp` / `collect` 等 | 命令行 `--config`，未指定时为 `config/config.yaml` |
| `connect` / `login` | `~/.digcrawl/config.yaml`（可用 `--config` 覆盖） |

首次 `connect` 或 `login` 会自动创建 `~/.digcrawl/` 目录并写入配置。

---

## 2. 配置文件说明

参考仓库内 [`config/config.yaml`](config/config.yaml)。主要段落如下。

### Gateway（HTTP 网关）

| 字段 | 说明 |
|------|------|
| `ListenAddr` | 监听地址，默认 `127.0.0.1:8787` |
| `InstanceID` | 实例标识，出现在 manifest 中 |
| `AuthToken` | 非空时要求请求携带 Bearer / `X-DigCrawl-Token` |
| `AllowedTools` | 工具名白名单；空表示不限制 |
| `AllowedReadPaths` | `file.read` 允许读取的本地路径前缀 |
| `AllowedDomains` | 与策略合并的 URL 域名白名单 |
| `ArtifactDir` / `ArtifactTTLMinutes` | 大对象（截图等）落盘目录与 TTL |
| `RequireApproval` | 是否强制要求高风险工具在 `ToolCall` 中携带 `approved: true` |

### Collector（反向连接宿主）

| 字段 | 说明 |
|------|------|
| `ServerURL` | 宿主根地址，如 `https://api.example.com` |
| `WSPath` | WebSocket 路径，默认 `/digcrawl/v1/collector/ws` |
| `Token` | 与宿主约定的鉴权令牌 |
| `InstanceID` | 本机 Collector 实例 ID（空则自动生成） |
| `PullIntervalSec` | `>0` 时由 Collector 主动拉取任务；`0` 为推送模式 |
| `MaxConcurrentCalls` | 并发执行工具数上限 |
| `RateLimitCooldownMs` | 相同 `RateLimitKey` 工具调用完成后的冷却时间 |
| `CallQueueSize` | Collector 会话内有界任务队列长度，队列满时返回 `RATE_LIMITED` |
| `CallQueueWaitMs` | 保留字段，兼容旧配置；当前 worker queue 入队不阻塞 read loop |
| `RetryMaxAttempts` / `RetryDelayMs` | 对 transient 工具失败做有限重试 |

### Collect（本地采集）

| 字段 | 说明 |
|------|------|
| `OutputDir` | 导出根目录，默认 `collect-output` |
| `InboxDir` | 收件箱目录，默认 `collect-inbox` |
| `DefaultFormats` | 默认导出格式：`md`、`html`、`json` |
| `DownloadMedia` | 是否下载正文中的媒体到 `.assets/` |
| `PersistMode` | 持久化方案：`files`（仅文件）/ `index`（默认，+ index.json）/ `sqlite`（+ collect.db）/ `both` |
| `DBPath` | SQLite 库路径，默认 `storage/digcrawl/collect.db` |
| `IndexEnabled` | `index`/`both` 模式下是否写 `index.json`（同 URL 覆盖更新） |
| `RateLimitMs` | inbox 批处理时 URL 之间的间隔（毫秒） |
| `PlatformRateLimits` | 按平台覆盖批处理间隔，如 `xiaohongshu`、`douyin`、`toutiao` |
| `InboxPollIntervalSec` | `inbox --watch` 轮询间隔 |

### LocalBrowser（本地浏览器）

| 字段 | 说明 |
|------|------|
| `Enabled` | 为 `false` 时禁用浏览器及依赖它的 Provider |
| `AllowedDomains` | 允许访问的域名列表（Collect / 平台工具均受约束） |
| `Headless` | 是否无头模式 |
| `CookieStoreDir` | 按域名持久化 Cookie，便于登录态采集 |
| `CDPMode` / `CDPPort` | 启动浏览器时开启 CDP 调试端口 |
| `CDPConnectExisting` | 连接已有 CDP 浏览器（如手动启动的 Chrome） |
| `UserDataDir` | 持久化浏览器 profile 根目录 |
| `ProxyURL` | 浏览器代理地址，页面 fetch/API fallback 共用 |

### Platform（平台读取）

| 字段 | 说明 |
|------|------|
| `BrowseRetryMaxAttempts` | Browser 兜底读取 transient 错误最大尝试次数 |
| `BrowseRetryDelayMs` | Browser 重试间隔 |
| `ReadMetricsEnabled` | 是否输出 `[platform-read]` 结构化日志 |
| `HealthProbes` | `digcrawl browser check --trial` 的平台默认试读 URL |

微信公众号公开文章默认优先使用 HTTP HTML 静态解析，成功时会在 `platform_metadata` 中标记 `read_channel=http`、`extraction_source=html`。只有网络 transient 或解析状态不确定时才进入 Browser 兜底；验证码、频控、删除、私密、登录要求和最终 URL 跳出微信域名会直接返回结构化错误。

### Providers（内置 Provider 开关）

| 字段 | 说明 |
|------|------|
| `PlatformEnabled` | 公众号 / 小红书 / 抖音 / X 平台工具 |
| `BrowserEnabled` | `browser.browse` / `snapshot` / `action` |
| `ExampleEnabled` | `gateway.ping`、`file.read` |
| `StubEnabled` | 示例第三方 Provider（开发用） |

---

## 3. 命令行总览

```text
digcrawl gateway [flags]       # HTTP 工具网关
digcrawl collector [flags]     # WebSocket 反向连接宿主
digcrawl connect [flags]       # 保存宿主配置并启动 Collector
digcrawl login [flags]         # 仅保存 Token / 宿主地址
digcrawl mcp [flags]           # MCP stdio（Cursor / Claude Desktop）
digcrawl stdio [flags]         # 逐行 JSON 协议
digcrawl dev-host [flags]      # 本地参考宿主（仅开发调试）
digcrawl collect <url>         # 采集单个 URL
digcrawl collect inbox         # 处理收件箱
digcrawl browser login         # 平台扫码/交互登录并保存 Cookie
digcrawl browser check         # 检查平台会话，可选试读
digcollect <url>              # 轻量采集入口，等价于 digcrawl collect <url>
digcrawl help
```

全局习惯：多数子命令支持 `--config <path>` 指定配置文件。

---

## 4. HTTP 网关（gateway）

在本机启动工具网关，默认只监听本机地址。若要暴露给局域网或公网，请显式设置 `AuthToken`、`AllowedTools`、`AllowedDomains` 并确认风险。

```bash
# 使用仓库示例配置
digcrawl gateway --config config/config.yaml

# 覆盖监听地址
digcrawl gateway --config config/config.yaml --addr 127.0.0.1:8787
```

验证：

```bash
curl -s http://127.0.0.1:8787/health
curl -s http://127.0.0.1:8787/manifest | jq .
```

若配置了 `AuthToken`：

```bash
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:8787/manifest
```

### 调用工具示例

```bash
curl -s -X POST http://127.0.0.1:8787/tools/call \
  -H "Content-Type: application/json" \
  -d '{
    "type": "tool_call",
    "id": "demo-1",
    "tool": "gateway.ping",
    "input": {}
	  }'
```

`browser.action`、`file.read` 等清单中标记 `requires_user_approval: true` 的工具默认需要在 `ToolCall` 里传入 `"approved": true`。否则 runtime 会返回 `USER_APPROVAL_REQUIRED`，用于避免宿主或 HTTP 客户端直接执行高风险交互。

---

## 5. 连接云端宿主（collector / connect / login）

典型部署：**用户本机运行 DigCrawl Collector**，通过 WebSocket **反向连接**到你的业务宿主；宿主下发 `tool_call`，在本机执行后回传结果。

### 方式 A：一步连接（推荐）

```bash
digcrawl connect --server https://your-host.example.com --token <collector-token>
```

- 将 `ServerURL`、`Token` 写入 `~/.digcrawl/config.yaml`
- 随即以该配置启动 `collector`

仅保存配置、不启动：

```bash
digcrawl connect --server https://your-host.example.com --token <token> --save-only
```

### 方式 B：分步配置

```bash
# 先保存令牌（可选同时写 server）
digcrawl login --token <collector-token>
digcrawl login --server https://your-host.example.com --token <token>

# 再启动 Collector
digcrawl collector --config ~/.digcrawl/config.yaml
```

### Collector 常用参数

```bash
digcrawl collector \
  --config ~/.digcrawl/config.yaml \
  --server https://your-host.example.com \
  --token <token> \
  --instance-id my-laptop \
  --pull-interval 5
```

| 参数 | 说明 |
|------|------|
| `--server` | 覆盖配置中的 `Collector.ServerURL` |
| `--token` | 覆盖 `Collector.Token` |
| `--instance-id` | 覆盖 `Collector.InstanceID` |
| `--pull-interval` | 拉取间隔（秒）；`-1` 表示使用配置文件 |

### 开发用参考宿主

生产环境请在**你自己的项目**中实现 WebSocket 宿主；仓库仅提供最小参考：

```bash
digcrawl dev-host --addr :8790 --token dev
```

再在另一终端：

```bash
digcrawl collector --server http://127.0.0.1:8790 --token dev
```

---

## 6. IDE 集成（MCP）

在 Cursor、Claude Desktop 等支持 MCP 的客户端中，将 DigCrawl 注册为 stdio 服务：

```json
{
  "mcpServers": {
    "digcrawl": {
      "command": "/path/to/digcrawl",
      "args": ["mcp", "--config", "/path/to/config/config.yaml"]
    }
  }
}
```

启动后，IDE 内的 Agent 可通过 MCP 调用本机已注册工具（与 `gateway` 共用同一套 runtime / Provider）。

---

## 7. stdio JSON 协议

适用于需要管道集成的宿主或脚本：

```bash
digcrawl stdio --config config/config.yaml
```

stdin/stdout 上逐行 JSON，消息类型与 [`protocol/`](protocol/) 包一致（如 manifest 查询、tool_call、tool_result）。适合与自定义编排器对接。

---

## 8. 本地内容采集（collect）

Collect 在**无云端宿主**的情况下，把链接抓取并导出到本地目录，适合知识库、归档、批处理链接。

### 支持的内容来源

| 类型 | 检测方式 | 底层工具 |
|------|----------|----------|
| 微信公众号 | `mp.weixin.qq.com` 等 | `wechat.article.read` |
| 小红书 | `xiaohongshu.com`、`xhslink.com` 等 | `xiaohongshu.note.read` |
| 抖音 | `douyin.com` 等 | `douyin.video.read` |
| 今日头条 | `toutiao.com`、`m.toutiao.com` 等 | `toutiao.article.read` |
| X / Twitter | `x.com`、`twitter.com` 等 | `x.post.read` |
| 任意网页 | 其他 `http(s)` URL | `browser.browse`（通用网页） |

小红书、抖音、头条采用 **API 优先 + Browser 兜底** 的 strategy chain：先尝试页面运行时 `fetch(credentials:"include")`，失败时回落到 hydration/meta/DOM 提取。输出 JSON 的 `platform_metadata` 会包含 `read_channel`、`extraction_source`、`api_http_status`、`api_error_code` 等诊断字段。`LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`、内容删除/私密会作为结构化错误返回。

### 单条 URL

```bash
digcrawl collect "https://mp.weixin.qq.com/s/xxxx"
digcrawl collect "https://www.toutiao.com/article/xxxx"
digcrawl collect "https://example.com/article" --format md,html,json --out ./my-output
```

### 平台登录与健康检查

部分平台在未登录或 Cookie 过期时会返回 `LOGIN_REQUIRED`。可使用有头浏览器完成登录并保存 Cookie：

```bash
digcrawl browser login --platform xiaohongshu
digcrawl browser login --platform douyin
digcrawl browser login --platform toutiao
```

登录后可检查会话，并可选试读一个 URL：

```bash
digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/xxxx" --trial
```

| 参数 | 说明 |
|------|------|
| `--config` | 配置文件路径 |
| `--out` | 覆盖 `Collect.OutputDir` |
| `--format` | 逗号分隔：`md`、`html`、`json` |
| `--file` | 文本文件，每行一个 URL |

导出的文章文件名会清理非法字符，并按 UTF-8 边界截断长标题；`--format md,json,html` 会同时保留 Markdown、JSON 所需文本字段和 HTML。

### 批量文件

```bash
digcrawl collect --file urls.txt --format md,json --out ./collect-output
```

### 收件箱（inbox）

将待采集链接放入 `collect-inbox/`（默认名，可在配置中修改）：

- 支持 `.url`、`.txt` 等文本文件，每行一个 URL
- 处理成功后移至 `processed/`
- 失败记录移至 `failed/`

```bash
# 处理一次
digcrawl collect inbox

# 持续轮询（Ctrl+C 停止）
digcrawl collect inbox --watch
```

批处理受 `Collect.RateLimitMs` 节流，避免对目标站请求过快。
若配置了 `Collect.PlatformRateLimits`，会按 URL 检测到的平台使用更细粒度的间隔，并加 ±20% jitter。

### 查询与迁移（SQLite）

当 `PersistMode` 为 `sqlite` 或 `both` 时：

```bash
digcrawl collect list
digcrawl collect list --q wechat --limit 50
digcrawl collect show <collect-id>
digcrawl collect show "https://mp.weixin.qq.com/s/..."
digcrawl collect migrate-index   # 将已有 index.json 导入 collect.db
```

### 输出目录结构

```text
collect-output/
  index.json                 # PersistMode=index|both
  2026-05-21/
    文章标题.md
    文章标题.html
    文章标题.json
    文章标题.assets/         # 媒体与截图
      media.json
      001.jpg

storage/digcrawl/
  collect.db                 # PersistMode=sqlite|both

collect-inbox/
  pending.url                # 待处理（示例文件名）
  processed/                 # 已成功
  failed/                    # 失败记录
```

更细的实现说明见 [`collect/README.md`](collect/README.md)。

---

## 9. 内置工具清单

通过 `GET /manifest` 或 MCP 可查看完整 schema；下表为常用工具名。

### 网关示例（example）

| 工具名 | 说明 |
|--------|------|
| `gateway.ping` | 健康检查，返回运行时信息 |
| `file.read` | 读取 `AllowedReadPaths` 下的本地文件 |

### 平台内容（platform）

| 工具名 | 说明 |
|--------|------|
| `wechat.article.read` | 微信公众号文章 |
| `xiaohongshu.note.read` | 小红书笔记 |
| `douyin.video.read` | 抖音视频页 |
| `toutiao.article.read` | 今日头条文章 |
| `x.post.read` | X 帖子 |

### 浏览器（browser）

| 工具名 | 说明 |
|--------|------|
| `browser.browse` | 打开 URL，提取正文 / 截图 / 摘要 |
| `browser.snapshot` | 无障碍树快照（可交互元素） |
| `browser.action` | 点击、输入等（高风险，建议需用户确认） |

### 开发桩（stub，需 `StubEnabled: true`）

用于验证 Provider 加载流程的占位工具，**不要在生产开启**。

---

## 10. HTTP API 参考

基址：`http://<ListenAddr>`（默认 `http://127.0.0.1:8787`）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `GET` | `/manifest` | 工具清单（ToolManifest） |
| `POST` | `/tools/call` | 执行工具（请求体为 `ToolCall` JSON） |
| `GET` | `/artifacts/{id}` | 下载 Artifact（如截图） |

鉴权（当 `Gateway.AuthToken` 非空时）：

- Header：`Authorization: Bearer <token>`
- 或：`X-DigCrawl-Token: <token>`

---

## 11. 宿主项目接入（Go SDK）

若你的业务服务需要**主动调用**用户机器上的 Gateway（而非 Collector 反向连接），可在宿主侧使用 HTTP 客户端：

```go
import (
    "context"
    "encoding/json"
    "github.com/originaleric/digcrawl/client"
    "github.com/originaleric/digcrawl/protocol"
)

func callPing(ctx context.Context, gatewayURL, token string) error {
    c := client.New(gatewayURL, token)
    manifest, err := c.Manifest(ctx)
    if err != nil {
        return err
    }
    _ = manifest

    result, err := c.Call(ctx, protocol.ToolCall{
        ID:   "host-1",
        Tool: "gateway.ping",
        Input: json.RawMessage(`{}`),
    })
    if err != nil {
        return err
    }
    _ = result
    return nil
}
```

**两种集成模式对比：**

| 模式 | 谁发起连接 | 适用场景 |
|------|------------|----------|
| **Collector** | 本机连宿主 WebSocket | SaaS 统一下发任务、用户无需公网 IP |
| **HTTP Client** | 宿主连用户 Gateway URL | 用户自行暴露端口 / 内网穿透 |

宿主侧如需实现 Collector WebSocket 服务，可直接复用 `transport/host` 包：

```go
srv := host.NewServer("collector-token", "/digcrawl/v1/collector/ws")
http.ListenAndServe(":8790", srv.Handler())
```

业务系统可通过 `srv.Enqueue(instanceID, call)` 支持拉取模式，或用 `srv.Push(instanceID, call)` 立即推送任务。`digcrawl dev-host` 仍保留为本地调试参考。

---

## 12. 扩展自定义 Provider

实现 [`provider.Provider`](provider/provider.go) 接口：

```go
type Provider interface {
    Name() string
    Register(ctx context.Context, reg *registry.Registry) error
}
```

可选实现 `HealthReporter`（注册前健康检查）、`ToolManifester`（仅列出工具描述）。

在自定义启动代码中：

```go
rt, err := runtime.NewRuntime(cfg) // 或自行 NewRegistry 后组装 Runtime
```

默认通过 `runtime.DefaultProviders` 按配置加载内置 Provider；若需追加自定义 Provider，请参考 `runtime/bootstrap.go` 中的 `RegistryOptions.Providers` 字段注入。

---

## 13. 架构与目录

```text
protocol/           协议类型与 JSON 线格式
runtime/            注册表、策略、审计、Artifact、引导装配
transport/          http、collector、mcp、stdio、devhost
client/             宿主侧 HTTP SDK
collect/            检测、执行、导出、索引、收件箱
provider/           platform、browser、example、stub
internal/platform/  各平台页面解析
internal/browser/   go-rod 本地浏览器
```

数据与状态默认落在项目目录下：

- `storage/digcrawl/artifacts` — Artifact 文件
- `storage/digcrawl/browser_cookies` — 浏览器 Cookie
- `collect-output/`、`collect-inbox/` — Collect 产出与收件箱

---

## 14. 常见问题

### 浏览器工具报错「域名不在白名单」

在 `config.yaml` 的 `LocalBrowser.AllowedDomains` 与 `Gateway.AllowedDomains` 中加入目标域名，或放宽策略（注意安全边界）。

### Collect 通用网页内容不完整

`browser.browse` 对动态站点依赖页面加载与选择器；可在工具 input 中指定 `wait_selector`、`content_selector`（通过 Gateway 直接调用时）。Collect 路径使用默认 full 模式。

### Collector 连不上宿主

检查 `ServerURL` 是否含协议（`https://`）、防火墙、宿主 WebSocket 路径是否与 `WSPath` 一致、Token 是否匹配。

### 与 DigEino 的关系

[DigEino](https://github.com/originaleric/digeino) 是 Eino / DigFlow 生态的工具 Provider 实现；DigCrawl 与之**解耦**，本仓库已内置 Collect 与平台/browser 工具。你可单独把 DigEino 作为 Provider 接到自建 runtime，也可仅使用 DigCrawl 独立发行版。

---

## 15. 相关链接

- 仓库：<https://github.com/originaleric/digcrawl>
- 示例配置：[`config/config.yaml`](config/config.yaml)
- Collect 模块：[`collect/README.md`](collect/README.md)
- 更新记录：[`updates/`](updates/)

---

## 16. 本地抓取测试方案

建议按“基础网页 → 头条 → 登录态平台 → 批量”的顺序测试，方便定位是导出、URL 归一化、登录态、API 通道还是 Browser 兜底的问题。

### 16.1 构建与基础检查

```bash
/Users/dig/.gvm/gos/go1.24.8/bin/go build -o digcrawl ./cmd/digcrawl
./digcrawl --help
```

### 16.2 通用网页 fallback

```bash
./digcrawl collect "https://example.com" --format md,json --out ./collect-output-test
```

预期：生成 md/json，说明 collect、browser 与 export 基础链路可用。

### 16.3 今日头条文章

```bash
./digcrawl collect "https://www.toutiao.com/article/你的文章ID/" \
  --format md,json --out ./collect-output-test
```

检查 JSON：

- `platform` 应为 `toutiao`
- `content_type` 应为 `article`
- `platform_metadata.read_channel` 为 `api` 或 `browser`
- `platform_metadata.extraction_source` 有值
- `title` / `text` 不为空

### 16.4 登录后试读

```bash
./digcrawl browser login --platform xiaohongshu
./digcrawl browser login --platform douyin
./digcrawl browser login --platform toutiao

./digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/你的文章ID/" --trial
```

如果未登录或风控，会看到结构化错误，如 `LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`。

### 16.5 小红书与抖音 URL

小红书优先使用带 `xsec_token` 的 URL：

```bash
./digcrawl collect "https://www.xiaohongshu.com/explore/笔记ID?xsec_token=xxx&xsec_source=pc_search" \
  --format md,json --out ./collect-output-test
```

抖音测试标准视频和 `modal_id`：

```bash
./digcrawl collect "https://www.douyin.com/video/视频ID" \
  --format md,json --out ./collect-output-test

./digcrawl collect "https://www.douyin.com/user/SEC_UID?modal_id=视频ID" \
  --format md,json --out ./collect-output-test
```

预期：`canonical_url` 归一到作品详情 URL，`platform_metadata.risk_flags` 可反映 `modal_id_resolved`、`xsec_token_missing` 等诊断信息。

### 16.6 批量文件

准备 `test-urls.txt`：

```text
https://example.com
https://www.toutiao.com/article/你的文章ID/
https://www.douyin.com/video/视频ID
```

执行：

```bash
./digcrawl collect --file test-urls.txt --format md,json --out ./collect-output-test
```

预期：批量采集按 `Collect.RateLimitMs` / `Collect.PlatformRateLimits` 节流；失败 URL 不影响其他 URL 继续处理。

### 16.7 查看输出与诊断字段

```bash
find ./collect-output-test -maxdepth 3 -type f | sort
```

重点查看每个 JSON 的：

```json
{
  "platform": "...",
  "content_type": "...",
  "title": "...",
  "platform_metadata": {
    "read_channel": "api 或 browser",
    "extraction_source": "...",
    "api_http_status": 200,
    "api_error_code": "...",
    "api_fetch_mode": "browser_context_fetch",
    "api_signature_mode": "page_runtime"
  }
}
```
