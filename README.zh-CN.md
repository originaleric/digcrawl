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
| 监控新增内容 | 从 feed、列表、主页或页面发现新增内容，再按计划采集和通知。 | `digcrawl watchlist run --once` |
| 为 AI Agent 提供阅读能力 | 以官方 Skill 和平台感知路由运行本地 MCP 服务。 | `digcrawl mcp --config <profile>` |
| 集成到宿主应用 | 使用 HTTP Gateway、Collector、stdio 或 Go SDK。 | [Transport 文档](./transport/README.md) |

## 支持的来源

DigCrawl 将采集视为稳定的 **crawl / collect / normalize / export** 管线，而不是一个单纯的浏览器网关。它优先使用静态 HTML、平台 API 与 RSS/Atom；当相应路径启用时，才回退到本地浏览器。

| 来源类别 | 已支持来源 |
| --- | --- |
| 社交内容 | 微信公众号、小红书、抖音、今日头条、X、Reddit、Threads、Instagram、B站、微博、快手、知乎 |
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

执行一轮 Watchlist，或在本机常驻运行调度器：

```bash
./digcrawl watchlist run --once --config config/config.yaml
./digcrawl watchlist serve --config config/config.yaml
```

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

Codex 与 Claude Code 的示例位于 [Skill 目录](./.agents/skills/digcrawl/)。其他 MCP 客户端也使用同一条 stdio 命令，只需按各自配置格式填写。

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
| 2026-08 | OCR 收敛为 DigCrawl 自己的 OpenAI-compatible 视觉客户端，并保持显式启用。 | 图片富化不依赖 DigFlow，也不要求仓库内置密钥。 |
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
