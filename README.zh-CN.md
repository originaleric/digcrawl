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
| 任意兼容 MCP 的 Agent | 遵循可移植的 [MCP stdio 启动契约](./.agents/skills/digcrawl/mcp/README.md)：将 `digcrawl mcp --config <profile-absolute-path>` 配置为本地 stdio 服务。 |

Agent Skills 格式让完整 `SKILL.md` bundle 可移植，MCP 让 stdio 工具契约可移植；但两项标准都不规定所有宿主共用同一个配置文件或 Skill 发现目录。因此 bundle 保持宿主无关，少量注册适配器才按宿主区分。

配置完成后，只需把支持的 URL 发给 Agent，并要求它读取、总结、比较、引用，或在明确请求时转写视频。Agent 会在本机调用 DigCrawl，并保留需要登录、验证码、频控等结构化结果。

如需保存支持平台的视频，直接告诉 Agent 要保存哪个平台的视频。Skill 仅会在用户 profile 的 `VideoDownload.Providers` 中启用该平台，并可使用用户给定的绝对目录或 profile 同级默认目录；Bilibili、抖音、Instagram、快手、Threads、头条、微博、小红书、YouTube 和知乎可用，X、Reddit、微信公众号不在此保存路径。成功后视频工具只在 `platform_metadata.retained_media_relative_path(s)` 中返回相对 `VideoDownload.Dir` 的路径，避免暴露本机绝对路径。对于本地文件或直链媒体，使用 `digcrawl video`；单次执行是否保留下载文件由 `--keep <path>` 控制。图片保存是独立的 `ImageDownload` opt-in，支持单内容 provider（包括 X、Reddit、微信公众号和浏览器网页）；它只保存受大小、数量、超时约束的一整组原图或不保存，默认不会启用 OCR，也不会替代 Collect 的 `.assets`。

Skill 不包含 API Key，也不会暴露浏览器 Cookie。路由规则详见 [Agent Skill](./.agents/skills/digcrawl/)；安全细节详见[在 AI Agent 中使用](#在-ai-agent-中使用)。

## 快速开始

在 macOS 或 Linux 上安装当前已校验的 Release——**不需要 Go**：

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

安装脚本会识别系统和 CPU 架构，下载匹配的 GitHub Release，以 `checksums.txt` 校验后将 `digcrawl` 安装到 `~/.local/bin`。它还会把完整的可移植 Agent Skill 安装到 `$XDG_DATA_HOME/digcrawl/skills/digcrawl`（未设置时为 `~/.local/share/digcrawl/skills/digcrawl`），按需创建无密钥 Skill profile，并输出标准 MCP stdio 启动契约。检测到 Claude Code 时会安装其 Skill；Codex 则会以原子方式注册（或修复）实际生效的用户级或项目级配置，且始终指向刚安装的稳定二进制，不会指向构建产物或临时目录。它不会使用 `sudo`，也不会修改 shell 配置。`--skip-codex` 保留通用安装；只想安装二进制时才使用 `--skip-agent-setup`。需要审查脚本或固定版本时，先下载脚本，或传入 Release tag：

```bash
curl -fsSLO https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh
sh install.sh --version vX.Y.Z
# 可选：保留通用 Skill，但不修改 Codex。
sh install.sh --skip-codex
# 可选：仅安装二进制。
sh install.sh --skip-agent-setup
```

若 `~/.local/bin` 不在 `PATH` 中，请按当前 shell 的常规方式添加后重启 shell。Windows 用户请从 [GitHub Releases](https://github.com/originaleric/digcrawl/releases) 下载对应的 `.zip`。手动安装和 checksum 校验详见[使用说明](./USAGE.md)。

安装器注册或修复 Codex 后，请重启 Codex Desktop 并新建 task，让它加载新的 MCP 工具目录；已在启动时加载失败的旧 task 无法追溯性地获得该 MCP server。

确认二进制可用后；若安装时传了 `--skip-agent-setup`，再手动初始化本地 Agent/MCP profile：

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

每个 Release 归档都携带完整可移植 Skill（`skills/digcrawl/`）；安装器也会在输出提示的数据目录保留稳定副本。请把完整目录复制或链接到所用宿主文档规定的用户级或项目级 Skill 目录，而不是只复制 `SKILL.md`。安装器可以修复 Codex 适配器，检测到 Claude Code 时会安装其 Skill；其它宿主的配置文件不会被猜测或改写。

所有兼容 MCP 的宿主均可按[通用 stdio 启动契约](./.agents/skills/digcrawl/mcp/README.md)，以二进制和 profile 的绝对路径注册本地 MCP 服务：

```toml
[mcp_servers.digcrawl]
command = "/absolute/path/to/digcrawl"
args = ["mcp", "--config", "/absolute/path/to/digcrawl-skill.yaml"]
```

Codex 与 Claude Code 的示例位于 [Skill 目录](./.agents/skills/digcrawl/)。首次 `digcrawl mcp` 调用会按 profile 按需启动一个私有本机 Bridge；stdio 子进程只转发协议，不会各自创建 Runtime、Chrome 或固定 CDP 端口。profile 符号链接会归并到同一 Bridge；同一持久化 `LocalBrowser.UserDataDir` 同一时刻只允许一个 Bridge 持有，如需独立配置请使用不同目录。Bridge 会持续服务，直到异常退出或执行 `digcrawl bridge stop --config <profile>`，不会在预检与实际 `tools/call` 之间因空闲超时消失；`digcrawl bridge restart --config <profile>` 采用直接停启，立即取消在途读取并在其管理的 Chrome 已释放后再启动新实例，不做平滑交接。Release 安装器会先用已验证的临时新二进制停止标准 Skill profile 的旧 Bridge，再替换安装目标。可回收的是 stdio 子进程：未初始化管道在 1 分钟后回收，已初始化会话在 15 分钟没有实际 `tools/call` 后回收；MCP `ping` 不会续租。Codex 适配器可用正值 `--idle-timeout` 覆盖后一个间隔；通用 MCP 注册不应复制该宿主专用参数。

默认 Skill 不含密钥。页面截图与图片理解优先交给宿主 Agent 的多模态模型；可选本地 ASR 是显式能力，需要自行准备依赖和配置。

浏览器读取与平台回退共用总时间预算和最多一次自动故障重试；取消、发送排队超时、登录要求与页面切换不会触发无条件重放。读取期间发生导航或文档重载时，混合结果会被丢弃；浏览器、Feed/Reddit RSS 与 B站合并媒体的实际来源会在交付、下载和保存前按调用策略校验。`BROWSER_*` 错误的 `error_details` 提供失败阶段、重试结果、`next_actions` 和 `diagnostic_id`；Agent 收到 `handled: true` 后应先完成指定前置操作，再发起新的读取。排障及升级步骤见[使用说明](./USAGE.md#浏览器超时或反复失败)。

## 本地优先与安全边界

DigCrawl 在用户机器上运行，并将访问边界保持可见：

- HTTP Gateway 默认仅监听本机。
- 工具 allowlist、域名 allow/block 规则和逐工具审批标记约束执行范围。
- 浏览器会话只通过已配置的本地浏览器使用，不会作为 Cookie 或密钥返回。
- 登录、验证、私密内容和频控会以结构化结果返回。
- Artifact、审计记录与可选的保留策略均独立于采集输出管理。

在可信本地环境以外暴露 Gateway 或 Collector 前，请先阅读 [Runtime 文档](./runtime/README.md)。

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

### 浏览器恢复与诊断

浏览器故障重试统一由 `BrowserBrowse` 执行，平台 fallback 共用最多一次额外机会，并受同一总时间预算约束。平台和 Collector 的旧重试配置不再叠加浏览器读取次数；正常内容/选择器 fallback 单独计数。读取成功后的媒体保存、OCR、ASR 失败不会重放整个工具。

成功结果的 `browser_diagnostics` 或错误的 `error_details` 提供尝试次数、恢复状态和下一步动作。`handled: true` 表示本次调用已完成重试决策，Agent 应先完成明确前置操作，再发起新的读取。发送排队超时应等待当前任务，不能据此重启浏览器。

`digcrawl doctor --config <实际 profile>` 分开报告依赖、Bridge 传输、构建一致性和浏览器操作；操作检查在 Bridge 内创建并关闭空白测试页。首次 MCP 启动前 Bridge 不可达会显示操作未执行，doctor 不会另起浏览器。`digcrawl doctor --diagnostic-id <返回的编号>` 可查看本地安全摘要。

升级时先关闭旧 DigCrawl MCP 连接，再用已安装的新二进制执行 `digcrawl bridge restart --config <实际 profile>`，随后重新连接。restart 会中断在途调用，服务不会自动切换版本。profile 占用或归属不明时明确拒绝接管，用户登录与 Cookie 数据不会被整目录删除。
