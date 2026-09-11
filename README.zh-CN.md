<h1 align="center">DigCrawl</h1>

<p align="center">
  面向个人、数据管道与 AI Agent 的本地优先内容采集与规范化引擎。
</p>

<p align="center">
  读取网页和已支持的内容平台，并导出可靠的 Markdown、HTML、JSON、媒体元数据与结构化诊断信息。
</p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="./USAGE.md">使用说明</a> ·
  <a href="./docs/DigCrawl功能与使用手册.md">功能手册</a> ·
  <a href="./.agents/skills/digcrawl/">Agent Skill</a> ·
  <a href="https://github.com/originaleric/digcrawl/releases">Releases</a>
</p>

<p align="center">
  <a href="./LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
  <img alt="Language: Go" src="https://img.shields.io/badge/language-Go-00ADD8">
  <img alt="Interfaces: CLI, MCP, HTTP" src="https://img.shields.io/badge/interfaces-CLI%20%7C%20MCP%20%7C%20HTTP-2aa7b8">
  <img alt="Runs locally" src="https://img.shields.io/badge/runtime-local--first-111111">
</p>

DigCrawl 是一个本地优先的内容采集与规范化引擎。它读取公开网页和已支持的内容平台；当来源确有需要时，可复用用户已授权的本地浏览器会话。输出结果可由人、数据工作流和 AI Agent 直接使用。

```text
URL 或来源列表
        │
        ▼
识别 → 采集 → 规范化 → 导出 / 索引 / 通知
        │                 │
        │                 └── Markdown · HTML · JSON · 媒体元数据 · 诊断信息
        ▼
优先静态 HTML / API / RSS；按配置需要时回退到本地浏览器
```

## DigCrawl Skill：在 Agent 中运行

DigCrawl 内置官方 [Agent Skill](./.agents/skills/digcrawl/)，可用于 Codex、Claude Code 和其他兼容 MCP 的 Agent。Skill 为 Agent 提供平台感知的链接路由、登录与频控状态的安全处理，以及清晰的本地浏览器会话边界。

安装 `digcrawl` 二进制后，只需三步即可接入：

```bash
# 1. 创建无密钥的本地 Skill profile。
digcrawl init --profile skill

# 2. 检查 profile 和本地运行环境。
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"

# 3. 启动 MCP 服务（通常由 Agent 的配置自动启动）。
digcrawl mcp --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

将同一条本地 MCP 命令添加到你使用的 Agent：

| Agent | 配置示例 |
| --- | --- |
| Codex | [`codex/config.toml.example`](./.agents/skills/digcrawl/codex/config.toml.example) |
| Claude Code | [`claude-code/.mcp.json.example`](./.agents/skills/digcrawl/claude-code/.mcp.json.example) |
| 任意兼容 MCP 的 Agent | 将 `digcrawl mcp --config <profile-absolute-path>` 配置为本地 stdio 服务。 |

配置完成后，只需把支持的 URL 发给 Agent，并要求它读取、总结、比较、引用，或在明确请求时转写视频。Agent 会在本机调用 DigCrawl，并保留需要登录、验证码、频控等结构化结果。

如需保存支持平台的视频，直接告诉 Agent 要保存哪个平台的视频。Skill 仅会在用户 profile 的 `VideoDownload.Providers` 中启用该平台，并可使用用户给定的绝对目录或 profile 同级默认目录；Bilibili、抖音、Instagram、快手、Threads、头条、微博、小红书、YouTube 和知乎可用，X、Reddit、微信公众号不在此保存路径。成功后视频工具只在 `platform_metadata.retained_media_relative_path(s)` 中返回相对 `VideoDownload.Dir` 的路径，避免暴露本机绝对路径。对于本地文件或直链媒体，使用 `digcrawl video`；单次执行是否保留下载文件由 `--keep <path>` 控制。

Skill 不包含 API Key，也不会暴露浏览器 Cookie。路由规则详见 [Agent Skill](./.agents/skills/digcrawl/)；安全细节详见[在 AI Agent 中使用](#在-ai-agent-中使用)。

## 快速开始

在 macOS 或 Linux 上安装当前已校验的 Release——**不需要 Go**：

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

安装脚本会识别系统和 CPU 架构，下载匹配的 GitHub Release，以 `checksums.txt` 校验后仅将 `digcrawl` 安装到 `~/.local/bin`。它不会使用 `sudo`，也不会修改 shell 配置。需要审查脚本或固定版本时，先下载脚本，或传入 Release tag：

```bash
curl -fsSLO https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh
sh install.sh --version vX.Y.Z
```

若 `~/.local/bin` 不在 `PATH` 中，请按当前 shell 的常规方式添加后重启 shell。Windows 用户请从 [GitHub Releases](https://github.com/originaleric/digcrawl/releases) 下载对应的 `.zip`。手动安装和 checksum 校验详见[使用说明](./USAGE.md)。

确认二进制可用后，初始化本地 Agent/MCP profile：

```bash
digcrawl version
digcrawl init --profile skill
digcrawl config validate --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

macOS 与 Linux 默认将 profile 保存在 `$XDG_CONFIG_HOME/digcrawl/`；未设置时为 `~/.config/digcrawl/`。Windows 默认路径为 `%APPDATA%\DigCrawl\`。除非显式传入 `--force`，`init` 不会覆盖已有 profile。

需要从源码构建的开发者请准备 Go 1.26.1+，并查看[源码运行说明](./USAGE.md#从源码运行)。安装、Release 校验、完整配置和排障请查看[使用说明](./USAGE.md)。

## 你可以用它做什么

| 你的目标 | DigCrawl 能做什么 | 从这里开始 |
| --- | --- | --- |
| 归档单个页面 | 将文章或帖子提取为可移植的 Markdown、HTML、JSON 和可选媒体 assets。 | `digcrawl collect <url>` |
| 批量采集 URL | 处理 URL 文件或本地收件箱，并保留索引或数据库记录。 | `digcrawl collect --file urls.txt` |
| 解析视频文件或直链 | 从本地视频或直接下载 URL 提取语音转写与画面文字。 | `digcrawl video ./video.mp4` |
| 监控新增内容 | 从 feed、列表、主页或页面发现新增内容，再按计划采集和通知。 | `digcrawl watchlist run --once` |
| 为 AI Agent 提供阅读能力 | 以官方 Skill 和平台感知路由运行本地 MCP 服务。 | `digcrawl mcp --config <profile>` |
| 集成到宿主应用 | 使用 HTTP Gateway、Collector、stdio 或 Go SDK。 | [Transport 文档](./transport/README.md) |

## 支持的来源

DigCrawl 将采集视为稳定的 **crawl / collect / normalize / export** 管线，而不是一个单纯的浏览器网关。它优先使用静态 HTML、平台 API 与 RSS/Atom；当相应路径启用时，才回退到本地浏览器。

| 来源类别 | 已支持来源 |
| --- | --- |
| 社交内容 | 微信公众号、小红书、抖音、今日头条、X、Reddit、Threads、Instagram、B站、微博、快手、知乎、YouTube |
| 订阅与发现 | RSS、Atom、sitemap、平台列表、主页和已支持的内容 feed |
| 通用网页 | 任意 `http` 或 `https` 页面，可由通用浏览器读取工具处理 |

平台读取会返回 `LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED` 和 `CONTENT_TYPE_MISMATCH` 等结构化状态。不可访问或私密内容不会被静默导出为正常内容。

URL 模式、登录行为与平台边界请查看[使用说明](./USAGE.md)。

## 采集、导出与监控

采集单个 URL 或批量 URL：

```bash
./digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,html,json
./digcrawl collect --file urls.txt --format md,json --out ./collect-output
./digcrawl collect inbox --watch
```

解析本地视频或可直接下载的视频 URL：

```bash
# 根据已启用的 ASR / 视频帧 OCR 配置自动处理。
./digcrawl video ./meeting.mp4

# 同时执行 ASR 与抽帧 OCR，并导出 Markdown。
./digcrawl video "https://cdn.example.com/meeting.mp4" --mode both --format md --out meeting.md

# 仅转写语音，或仅识别画面文字。
./digcrawl video ./meeting.mp4 --mode asr
./digcrawl video ./meeting.mp4 --mode ocr
```

视频 URL 必须是可直接下载的媒体地址；平台页面 URL 仍应使用对应的采集工具。远程视频会在处理结束后删除；如需保存，传入 `--keep ./video.mp4`。未传入 `--config` 时，`config/config.yaml` 中已启用的根级 ASR/OCR 优先；未启用的处理器可回退到标准用户 Skill profile 中已启用的配置。

执行一轮 Watchlist，或在本机常驻运行调度器：

```bash
./digcrawl watchlist run --once --config config/config.yaml
./digcrawl watchlist serve --config config/config.yaml
```

Watchlist 默认串行执行。如需显式启用有界并发，可配置
Watchlist.Scheduler.MaxConcurrentRuns 与
Watchlist.Collect.MaxConcurrentItems；内容 key、Collect 输出路径、
index.json 和按平台请求间隔仍会受到统一保护。

典型输出目录同时适合人工查看和下游程序消费：

```text
collect-output/
  index.json
  <platform-or-web>/<slug>/
    content.md
    content.html
    content.json
    .assets/
```

DigCrawl 可将采集记录写入 `index.json`、SQLite 或两者；Watchlist 可按配置使用 YAML、SQLite 或 MySQL。

## 在 AI Agent 中使用

仓库内包含官方 [DigCrawl Agent Skill](./.agents/skills/digcrawl/)。它指导兼容的 Agent 将链接路由到正确的本地 MCP 工具，处理登录与频控状态，并避免暴露用户 Cookie。

在 Agent 配置中，以二进制和 profile 的绝对路径注册本地 MCP 服务：

```toml
[mcp_servers.digcrawl]
command = "/absolute/path/to/digcrawl"
args = ["mcp", "--config", "/absolute/path/to/digcrawl-skill.yaml"]
```

Codex 与 Claude Code 的示例位于 [Skill 目录](./.agents/skills/digcrawl/)。未显式传超时参数的 `digcrawl mcp` 会从 MCP `initialize` 识别 Codex 并回收其遗留会话，因此已有 Codex 注册在宿主正常重启后即可生效；其他已初始化 MCP 客户端保持标准长连接。Codex 示例仍显式设置同一超时；只有明确要关闭回收时才传 `--idle-timeout=0`。

默认 Skill 不含密钥。页面截图与图片理解优先交给宿主 Agent 的多模态模型；可选本地 ASR 是显式能力，需要自行准备依赖和配置。

## 本地优先与安全边界

DigCrawl 在用户机器上运行，并将访问边界保持可见：

- HTTP Gateway 默认仅监听本机。
- 工具 allowlist、域名 allow/block 规则和逐工具审批标记约束执行范围。
- 浏览器会话只通过已配置的本地浏览器使用，不会作为 Cookie 或密钥返回。
- 登录、验证、私密内容和频控会以结构化结果返回。
- Artifact、审计记录与可选的保留策略均独立于采集输出管理。

在可信本地环境以外暴露 Gateway 或 Collector 前，请先阅读 [Runtime 文档](./runtime/README.md)。

## 最新动态

| 时间 | 更新 | 对使用者的意义 |
| --- | --- | --- |
| 2026-09-06 | 新增按平台显式 opt-in 的本地视频保存。 | 用户可只为支持的平台保存一个经过校验的本地视频；API 正文仍是来源真相，保存失败也不会阻止 Collect 导出已成功读取的内容。 |
| 2026-09-04 | 统一本地 ASR 安装、配置域与健康检查。 | 发布版无需 Go 即可安装 faster-whisper；setup、转写与 `asr check` 共用 root/Skill 缓存域，自定义 runner 会明确标记为外部管理，不再误报 ready。 |
| 2026-08-31 | 稳定浏览器登录态与 Watchlist 登录恢复。 | 规范会话不会再被后台读取覆盖；所有 Watchlist 入口都基于运行中的 executable 和已加载配置生成恢复命令。 |
| 2026-08-30 | 新增本地文件与直链媒体的视频解析。 | `digcrawl video` 复用有边界的 ASR 与抽帧 OCR；直链默认清理，并能使用用户 Skill profile 而不削弱显式项目配置。 |
| 2026-08-29 | 新增可选的 Watchlist 来源/条目并发与安全采集协调。 | 发现和处理可使用有界 goroutine；内容去重、输出路径、index.json 与平台请求间隔仍受保护。 |
| 2026-08-26 | 新增显式的 Skill 视频保留偏好与安全 profile 更新。 | Agent 可在用户明确要求时保存或清理下载影片、调整上限，无需编辑 YAML；本地并发 profile 写入会串行化且可取消。 |
| 2026-08-26 | 收口视频理解的执行隔离与外部 ID 边界。 | Skill 媒体目录每次执行独立；外部 call ID 仅关联请求，不能覆盖本地媒体或 manifest。 |
| 2026-08-26 | 解耦 YouTube、Skill 与 Collect 的下载视频理解。 | Provider 管理下载与清理；ASR/抽帧 OCR 只接收本地视频路径并返回内容。MP4 保留必须显式 opt-in，Collect 导出保持独立交付路径。 |
| 2026-08-25 | 新增公开视频/Shorts 的 YouTube 读取、字幕富化与 Watchlist 发现。 | URL 会归一化，且仅在播放器身份确认后接受内容；不会导出会过期的播放 URL。 |
| 2026-08-24 | 完成 OpenAI-compatible 视觉 provider 下的视频抽帧 OCR 生产验证。 | 已下载本地视频即使没有可用语音，也可提取内嵌字幕和画面文字；无文字帧会作为正常空结果处理，不再被误报为 provider 失败。 |
| 2026-08-17 | 重建浏览器恢复、CDP 服务管理与本地 ASR 启动的状态边界。 | 浏览器 profile 未释放前不会被重启；Playwright 缓存路径可动态发现；已有本地 ASR 模型和升级后的 runner 可跨重启继续使用。 |
| 2026-08 | OCR 收敛为 DigCrawl 自己的 OpenAI-compatible 视觉客户端，并保持显式启用。 | 图片富化不依赖 DigFlow，也不要求仓库内置密钥；已下载的本地视频可额外显式开启有上限的抽帧 OCR，用于识别内嵌字幕。 |
| 2026-07 | ASR 支持 API-compatible 与本地 `faster-whisper` provider。 | 视频和音频采集可按需包含本地可控的转写结果。 |
| 2026-07 | Watchlist 增加持久化来源、任务调度、摘要和通知。 | 来源可持续监控，而非仅一次性采集。 |
| 2026-07 | 扩展 Reddit、RSS/Atom、B站、微博、快手、知乎、Threads 和 Instagram 支持。 | 更多来源复用同一套规范化采集管线。 |

完整运行细节和更新记录请参阅[使用说明](./USAGE.md)与项目文档。

## 架构与接入方式

| 层 | 职责 |
| --- | --- |
| `collect/` | URL 识别、采集编排、导出、索引和收件箱处理 |
| `internal/source/` | Watchlist 的 feed、sitemap、网页和平台来源发现 |
| `internal/platform/` 与 `internal/browser/` | 平台读取、本地浏览器会话和回退提取 |
| `runtime/` | 工具注册、策略、审计、Artifact 与统一执行入口 |
| `transport/` | HTTP Gateway、反向 Collector、MCP、stdio 与开发宿主 |
| `protocol/` | Tool manifest、call、result 协议 |
| `client/` | 调用 DigCrawl HTTP Gateway 的 Go SDK |

DigCrawl 可作为本地 CLI、常驻 Watchlist 服务、HTTP Gateway、反向 Collector、MCP 服务、stdio 进程或 Go 库集成使用。

## 文档

| 文档 | 内容 |
| --- | --- |
| [使用说明](./USAGE.md) | 安装、配置、CLI、Collect、API、宿主接入和 FAQ |
| [功能与使用手册](./docs/DigCrawl功能与使用手册.md) | 功能总览与日常操作说明 |
| [Agent Skill](./.agents/skills/digcrawl/) | Agent 路由、错误处理和 MCP 客户端样例 |
| [Collect](./collect/README.md) | 输出、持久化和采集流程 |
| [Protocol](./protocol/README.md) | `ToolManifest`、`ToolCall` 和 `ToolResult` |
| [Runtime](./runtime/README.md) | 策略、审计与 Artifact |
| [Transport](./transport/README.md) | HTTP、Collector、MCP、stdio 和宿主接入 |
| [配置样例](./config/) | 可安全复制的配置模板 |

## 参与贡献

欢迎贡献。请先创建 issue，说明内容来源、使用场景、预期行为和相关平台限制。请勿提交凭据、浏览器 profile、Cookie、真实访问 Token 或私密采集内容。

新增或修改平台读取器时，请在平台条款和内容访问条件允许的范围内提供可复现的 fixture 或测试；并保留登录、验证、频控和不支持内容类型的结构化诊断。

## 开源说明

DigCrawl 是本地内容工具。对每一个采集来源，使用者都有责任遵守其服务条款、授权范围及适用法律。请勿用它绕过身份验证、访问控制、验证码或内容限制。

## 许可证

[MIT](./LICENSE)
