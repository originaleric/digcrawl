# DigCrawl

**面向内容采集、平台读取和 Agent 自动化的数据采集引擎。**

DigCrawl 的核心目标不是“提供一个网关”，而是把公众号、小红书、抖音、今日头条、X、Reddit 和普通网页里的内容稳定采集下来，转成业务系统能直接消费的 Markdown、HTML、JSON、媒体文件和结构化元数据。Gateway、Collector、MCP、stdio、本地浏览器只是不同接入方式，业务主线始终是 **crawl / collect / normalize / export**。

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
| 多平台内容读取 | 内置微信公众号、小红书、抖音、今日头条、X、Reddit，以及任意 `http(s)` 网页读取 |
| 结构化内容抽取 | 输出标题、正文、作者、发布时间、互动数据、媒体列表、规范化 URL、平台诊断信息 |
| 静态解析/API 优先 + 浏览器兜底 | 微信公众号优先使用 HTTP HTML 静态解析；小红书、抖音、头条优先尝试页面上下文 API；X 优先尝试官方 Tweet lookup，再回落到公开 oEmbed 和浏览器；Reddit 优先官方 API，再按配置兜底公开页面 |
| 登录态复用 | 支持通过有头浏览器完成平台登录，后续采集复用本机 Cookie |
| 批量采集与收件箱 | 支持 URL 文件批处理、`collect-inbox` 收件箱和 `--watch` 轮询 |
| Watchlist 定时监控 | 支持按配置定期检查 RSS/sitemap、头条作者页、平台主页和 Reddit subreddit/user/post comments 的新增内容，并通过通知通道推送 |
| 多格式导出 | 同时导出 Markdown、HTML、JSON，可选下载媒体到 `.assets/` |
| OCR 图片文字提取 | 可通过配置接入 OCR provider，对正文图片、截图、平台媒体和本地 assets 提取文字并写入结构化元数据 |
| ASR 视频语音转写 | 通过 `ffmpeg` 抽音频后，支持 OpenAI-compatible API 或本地 `faster-whisper` 命令转写 |
| LLM 摘要与通知 | Watchlist 新增内容可通过 extractive、OpenAI-compatible API 或 DigFlow Runtime 生成摘要，再推送到飞书 |
| 可检索持久化 | 支持 `index.json`、SQLite `collect.db` 或二者同时启用 |
| 宿主集成 | 可通过 HTTP Gateway、反向 Collector、MCP、stdio 或 Go SDK 接入业务系统 |
| 本机安全边界 | 默认本机监听、工具白名单、高风险工具审批、域名策略、审计日志和 Artifact 管理 |

OCR 目前支持 OpenAI-compatible 视觉模型、智谱 `zhipu_ocr` / `glm-ocr` 原生接口和本地开发桩；Collect 下载媒体时会优先对本地 `.assets` 图片做 OCR，并将结果写入 `platform_metadata.ocr_results`。

ASR 能力通过 `digcrawl asr transcribe <video>` 或 Gateway 工具 `asr.transcribe` 调用：先用 `ffmpeg` 抽取 16k 单声道 wav，再按配置选择 API provider 或本地 provider。API provider 兼容 `/audio/transcriptions`；本地 provider 默认调用 `faster-whisper`，也可在 `ASR.LocalCommand` / `ASR.LocalArgs` 中替换为 whisper.cpp 等命令。

Watchlist 能力通过 `digcrawl watchlist run --once` 或 `digcrawl watchlist run` 调用：先对配置的 RSS/sitemap、头条作者页、平台主页或 Reddit 来源做最新内容发现，再复用 Collect 抓取详情，最后按 `Summary` 配置使用规则摘要、LLM 直连或 DigFlow Runtime 生成摘要，并通过 Feishu webhook 推送。状态、锁和投递结果写入 SQLite，避免重复通知。

## 更新概览

- 2026-05-21：完成 DigGate 采集链路 Phase 1-3，补齐平台识别、会话复用、浏览器/API 策略链、队列重试、限流、健康检查和今日头条增强。
- 2026-07-03：微信公众号采集改为 HTTP 静态解析优先，修复最终 URL、导出格式、UTF-8 文件名截断、结构化错误和生产验证中的边界问题。
- 2026-07-03：今日头条文章采集增强轻量 API 探测、浏览器 hydration/meta/DOM 提取、视频/西瓜跳转识别、超时策略和 JSON 文本完整性。
- 2026-07-04：X/Twitter 单帖读取新增 Tweet lookup API 优先、OAuth2 app-only token 缓存、公开 oEmbed 兜底和登录墙/不可见壳页校验。
- 2026-07-04：OCR 全库接入智谱 `zhipu_ocr` / `glm-ocr`，支持远程图片 `layout_parsing`、本地 assets `files/ocr`、图片类型 sniff 和 OCR metadata 输出。
- 2026-07-08：视频解析链路补齐 `ffmpeg` 音频抽取、ASR API provider、本地 `faster-whisper` provider、Gateway 工具 `asr.transcribe` 和 CLI 转写命令。
- 2026-07-09：新增 Watchlist 定时监控、SQLite 状态/锁、Summary provider、DigFlow 内嵌 Runtime / HTTP 摘要 adapter、Feishu 通知模块、`df_` DigFlow DDL 和无密钥 GORM 示例配置。
- 2026-07-10：补齐本机 Chromium headless shell 安装说明与 `LocalBrowser.ExtraArgs` 配置，解决 macOS sandbox 下系统 Chrome / Rod Chromium headless 启动不稳定问题，并完成头条文章 + DigFlow Runtime 摘要二轮验证。
- 2026-07-15：新增 Reddit 官方 API 优先读取、单帖评论树、subreddit/user listing、watchlist `Options`、Reddit watchlist 来源和公开页面浏览器兜底边界。

## 支持的平台

| 平台 | 典型 URL | 内置工具 |
|------|----------|----------|
| 微信公众号 | `mp.weixin.qq.com` | `wechat.article.read` |
| 小红书 | `xiaohongshu.com`、`xhslink.com` | `xiaohongshu.note.read` |
| 抖音 | `douyin.com` | `douyin.video.read` |
| 今日头条文章 | `toutiao.com`、`m.toutiao.com` | `toutiao.article.read` |
| X | `x.com`、`twitter.com` | `x.post.read` |
| Reddit | `reddit.com`、`old.reddit.com`、`new.reddit.com`、`redd.it` | `reddit.post.read`、`reddit.subreddit.read`、`reddit.user.read` |
| 普通网页 | 任意 `http(s)` URL | `browser.browse` |

平台读取会返回结构化错误，便于业务系统判断是否需要人工登录、重试或跳过，例如 `LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`、`CONTENT_TYPE_MISMATCH`、内容删除/私密、选择器失效等。

微信公众号公开文章会优先走 HTTP 静态解析，直接提取标题、作者、发布时间、正文、Markdown/HTML 和图片资源；只有网络瞬断或解析状态不确定时才进入浏览器兜底。若微信安全验证、频控、删除/私密页面或最终 URL 跳出微信域名，会返回结构化错误而不是导出占位内容。

今日头条读取面向公开文章页，先进行轻量 API 探测，再回落到浏览器 DOM / hydration / meta 提取；若 `toutiao.com/group/...` 实际跳转到西瓜、抖音或视频内容，会快速返回 `CONTENT_TYPE_MISMATCH`，不会作为文章导出。

X 读取面向公开单帖，先尝试官方 Tweet lookup API；当账号套餐或权限不足时，会回落到公开 oEmbed 端点提取正文、作者、发布时间和 canonical URL；删除、不可见、登录墙壳页不会作为正常内容导出。

Reddit 读取面向公开单帖、subreddit listing 和用户 submitted/comments。单帖优先使用 Reddit OAuth API 读取正文、元数据和可配置评论树；API 临时失败或 OAuth 未配置且显式允许时，可使用本机浏览器兜底公开页面。401/403/429、私有社区、登录墙、受限内容不会通过浏览器绕过。

## 快速开始

```bash
go build -o digcrawl ./cmd/digcrawl
go build -o digcollect ./cmd/digcollect
```

采集单个链接：

```bash
./digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,html,json
./digcrawl collect "https://www.toutiao.com/article/..." --format md,json
./digcrawl collect "https://www.reddit.com/r/golang/comments/abc123/title/" --format md,json
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

Watchlist 单轮检查：

```bash
./digcrawl watchlist run --once --config config/config.yaml
```

Watchlist 常驻调度：

```bash
./digcrawl watchlist run --config config/config.yaml
```

DigFlow 内嵌 Runtime 摘要需要先准备数据库与模型配置。真实数据库连接写入本地 `config/gorm.yml`，该文件已被 `.gitignore` 忽略；仓库提供无密钥模板 `config/gorm.example.yml`。DigFlow 自身表保持 `df_` 前缀，配置入口为 `config/digflow-gorm.yml`、`config/digflow.yml`、`config/eino.yml` 和 `agents/watchlist/agents.yml`。

```bash
cp config/gorm.example.yml config/gorm.yml

export DIGCRAWL_MYSQL_HOST="127.0.0.1"
export DIGCRAWL_MYSQL_DATABASE="dig_crawl"
export DIGCRAWL_MYSQL_USER="root"
export DIGCRAWL_MYSQL_PASSWORD="<password>"
export DIGFLOW_SUMMARY_API_KEY="<openai-compatible-api-key>"
```

Reddit API 读取需要在 `config/config.yaml` 的 `Platform.Reddit` 中配置 Reddit app 凭证，或通过环境变量注入。默认 `BrowserFallback.AllowWithoutOAuth=false`，即未配置 OAuth 时不会自动用浏览器读取 Reddit；如只读取公开页面并接受兜底策略，可显式改为 `true`。

```bash
export REDDIT_CLIENT_ID="<client-id>"
export REDDIT_CLIENT_SECRET="<client-secret>"
export REDDIT_REFRESH_TOKEN="<optional-refresh-token>"
```

新库初始化优先使用 `sqls/full/digcrawl_full_sql_ddl.sql`；只初始化 DigFlow `df_*` 表可使用 `sqls/full/digflow_embedded_runtime_full_ddl.sql`。根目录兼容文件 `sqls/digcrawl_full_sql_ddl.sql` 和 `sqls/2026-07-09_DigFlow内嵌Runtime初始化DDL.sql` 保持同口径。已执行过旧版 DigFlow DDL 的库，补跑 `sqls/2026-07-09_DigFlow最新Runtime增量DDL.sql`，其中包含最新 LoopRuntime `df_loop_*` 表和 `df_resource_asset.storage_key` 唯一约束迁移。

本地 ASR 依赖 `ffmpeg` 和 `faster-whisper`。`ffmpeg` 需能在 `PATH` 中直接调用；`faster-whisper` 建议安装到隔离 venv，再把可执行命令加入 `PATH` 或通过 `--local-command` / `ASR.LocalCommand` 显式指定。

```bash
python3 -m venv /Users/dig/.openclaw/tmp/faster-whisper-venv
/Users/dig/.openclaw/tmp/faster-whisper-venv/bin/python -m pip install --upgrade pip
/Users/dig/.openclaw/tmp/faster-whisper-venv/bin/python -m pip install faster-whisper

export PATH="/Users/dig/.openclaw/tmp/faster-whisper-venv/bin:$PATH"
export HF_HOME="/Users/dig/.openclaw/tmp/huggingface"

./digcrawl asr transcribe ./video.mp4 --provider local --model small
```

如果安装的 Python 包没有提供 `faster-whisper` CLI，可提供一个兼容命令，或在运行时指定完整路径：

```bash
./digcrawl asr transcribe ./video.mp4 \
  --provider local \
  --model small \
  --local-command /Users/dig/.openclaw/tmp/faster-whisper-venv/bin/faster-whisper
```

本机浏览器兜底依赖 Chrome/Chromium。若系统 Chrome 或 Rod 自动下载的 Chromium 在 macOS sandbox / headless 环境中启动失败，建议安装 Playwright 的 `chrome-headless-shell`，并通过 `LocalBrowser.ChromePath` 与 `LocalBrowser.ExtraArgs` 显式指定：

```bash
mkdir -p /Users/dig/projects/.cache/digcrawl-playwright
cd /Users/dig/projects/.cache/digcrawl-playwright

npm_config_cache=/private/tmp/npm-cache \
PLAYWRIGHT_BROWSERS_PATH=/Users/dig/projects/.cache/ms-playwright \
npx -y playwright install chromium

find /Users/dig/projects/.cache/ms-playwright -type f -name 'chrome-headless-shell'
```

示例配置：

```yaml
LocalBrowser:
  ChromePath: "/Users/dig/projects/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-mac-arm64/chrome-headless-shell"
  Headless: true
  UserDataDir: "/private/tmp/digcrawl-pw-headless-profile"
  ExtraArgs:
    - "--single-process"
    - "--no-zygote"
    - "--disable-dev-shm-usage"
    - "--disable-crash-reporter"
    - "--disable-crashpad"
    - "--disable-breakpad"
    - "--disable-features=Crashpad"
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
| `digcrawl asr transcribe` | ffmpeg 抽音频并用 API 或本地 ASR 转写 |
| `digcrawl watchlist run` | 执行 watchlist 定时监控、摘要和通知 |
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
internal/watchlist/ Watchlist 发现、锁、状态机、调度和通知编排
internal/summary/   Extractive、LLM direct、DigFlow Runtime/HTTP 摘要 provider
internal/notify/    Feishu webhook 等通知 provider
agents/             DigFlow 内嵌 Runtime 的宿主侧 agent manifest 与 YAML
sqls/               DigCrawl `dc_*` 与 DigFlow `df_*` MySQL DDL
internal/platform/  微信、小红书、抖音、头条、X、Reddit 的平台读取实现
internal/browser/   本地 go-rod 浏览器、登录、快照、页面操作
provider/           platform、browser、asr、example、stub 工具 Provider
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
