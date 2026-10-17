<h1 align="center">DigCrawl</h1>

<p align="center"><strong>面向 AI Agent 的多平台内容抓取引擎。</strong></p>

<p align="center">抓取网页、文字、图片与视频，支持公开来源和用户已授权的登录来源。</p>

<p align="center">
  <a href="./README.md">English</a> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="#支持的来源">支持来源</a> ·
  <a href="#采集导出与监控">采集与监控</a> ·
  <a href="#文档">文档</a> ·
  <a href="https://github.com/originaleric/digcrawl/releases">Releases</a>
</p>

<p align="center">
  <a href="./LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
  <img alt="Local-first" src="https://img.shields.io/badge/runtime-local--first-111111">
  <img alt="CLI, MCP, HTTP" src="https://img.shields.io/badge/interfaces-CLI%20%7C%20MCP%20%7C%20HTTP-0071e3">
</p>

<p align="center">
  <img src="./docs/assets/readme/hero.zh-CN.svg" width="100%" alt="网页、图文和视频经 DigCrawl 抓取后交给 Agent，需登录来源使用用户授权会话">
</p>


## DigCrawl Skill：在 Agent 中运行

**复制到 Agent 对话中**（Codex、Grok Build、Claude Code 等）：

```text
帮我安装 DigCrawl Skill：https://github.com/originaleric/digcrawl
```

安装后按提示重启 Agent 或打开新对话，然后发送链接：

```text
帮我读取并总结这个链接：<链接>
```

## 快速开始

**在终端安装**（macOS / Linux，不需要 Go）：

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

脚本会校验下载文件，安装程序、完整 Skill 与默认配置。

<details>
<summary>安装位置、其他宿主与 Windows</summary>

- 自动配置 Codex；检测到 Claude Code 时安装其 Skill。其他宿主见 [MCP 接入](./.agents/skills/digcrawl/mcp/README.md)。
- 程序安装到 `~/.local/bin`；如提示找不到命令，请将该目录加入 `PATH`。
- Windows 下载 [Release](https://github.com/originaleric/digcrawl/releases)；指定版本与源码构建见[安装说明](./USAGE.md#1-安装)。

</details>

## 你可以用它做什么

| 场景 | 能力 |
| --- | --- |
| 内容抓取 | 读取网页、文章、笔记与来源图片 |
| 视频理解 | 获取可用字幕，按需转写语音、识别画面文字 |
| 持续采集 | 批量处理链接，监控来源更新并通知 |
| Agent 阅读 | 为总结、比较与研究提供来源内容 |

## 支持的来源

- **图文与社区**：微信公众号、小红书、微博、知乎、X、Reddit、Threads、Instagram。
- **视频与内容平台**：抖音、头条、B站、快手、YouTube。
- **网页与订阅**：通用网页、RSS、Atom、sitemap、平台列表与主页。

各平台的内容范围与登录要求见[平台说明](./USAGE.md#支持的内容来源)。

## 采集、导出与监控

抓取结果可保存为 Markdown、HTML、JSON 与可选媒体文件，也可通过 Watchlist 持续监控来源。


<details>
<summary>常用 CLI：单条、批量、视频与监控</summary>

```bash
# 单条与批量采集
digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,html,json
digcrawl collect --file urls.txt --out ./collect-output

# 按配置解析本地视频
digcrawl video ./meeting.mp4

# 运行一轮，或持续监控
digcrawl watchlist run --once --config config/config.yaml
digcrawl watchlist serve --config config/config.yaml
```

转写与画面识别需配置相应依赖，详见[视频解析](./USAGE.md#直接视频解析)；批量采集见 [Collect](./collect/README.md)。

</details>

## 在 AI Agent 中使用

原页面与抓取结果对照（2026-10-17）。微信保留原文摘录；视频例子未运行语音转写。

### 网页 · 微信公众号

```text
读取这篇 Agent 文章，提取正文与章节。 https://mp.weixin.qq.com/s/mbU0bE6BV5YfAsTmoY4H3A
```

<p align="center">
  <img src="./docs/assets/readme/examples/wechat.zh-CN.svg?v=0b451844" width="100%" alt="微信公众号: success">
</p>

[来源](https://mp.weixin.qq.com/s/mbU0bE6BV5YfAsTmoY4H3A) · 已取得正文、章节和 7 条图片引用。

<details>
<summary>查看返回字段节选</summary>

```json
{
  "status": "success",
  "title": "别再只卷Prompt 了，真正拉开Agent差距的是Context Engineering",
  "author": "腾讯技术工程",
  "media_references": {
    "image": 7
  },
  "excerpt": "Prompt Engineering → ReAct → Context Engineering\nContext 的七类构成要素\n长任务的三大核心技术"
}
```

[验证数据](./docs/assets/readme/examples/wechat.json)

</details>

### 图文 · 小红书

```text
读取这篇 Agent 图文笔记，提取文字和图片。 <粘贴小红书完整分享链接>
```

<p align="center">
  <img src="./docs/assets/readme/examples/xiaohongshu.zh-CN.svg?v=cb38a196" width="100%" alt="小红书: success">
</p>

[来源](https://www.xiaohongshu.com/explore/6ac20976000000001c00ca12) · [原页截图](./docs/assets/readme/examples/originals/xiaohongshu.jpg) · 使用本机授权登录，取得正文和 2 条图片引用。 请使用完整分享链接。

<details>
<summary>查看返回字段节选</summary>

```json
{
  "status": "success",
  "title": "2026 Agent新物种地图｜20款近期高热 Agent",
  "author": "略懂AI的Asher",
  "media_references": {
    "image": 2
  },
  "excerpt": "能力范式转向；组织形态重组；负责周期跨越",
  "image_observation": [
    "Personal",
    "Coding",
    "Workflow",
    "Research",
    "Creative"
  ]
}
```

[验证数据](./docs/assets/readme/examples/xiaohongshu.json)

</details>

### 视频

#### B站 · Agent 原理

```text
读取这个 Agent 教程，提取标题、简介和分P。 https://www.bilibili.com/video/BV1z3ZFYUE6K/
```

<p align="center">
  <img src="./docs/assets/readme/examples/bilibili.zh-CN.svg?v=b8146099" width="100%" alt="B站: success">
</p>

[来源](https://www.bilibili.com/video/BV1z3ZFYUE6K/) · [原页截图](./docs/assets/readme/examples/originals/bilibili.jpg) · 已取得视频资料与媒体引用；本例未请求转写。

<details>
<summary>查看返回字段节选</summary>

```json
{
  "status": "success",
  "title": "李宏毅：一堂课搞懂AI Agent原理【生成式AI的机器学习(2025)】大模型LLM",
  "author": "AI算法工程师笔记-",
  "media_references": {
    "image": 1,
    "video": 2
  },
  "excerpt": "分P1: 第二讲：一堂课搞懂AI Agent原理\n分P2: 大模型路线图-\n分P4: ML2025Agent"
}
```

[验证数据](./docs/assets/readme/examples/bilibili.json)

</details>

#### X · Operator 演示

```text
读取这条 Operator 视频帖，提取正文和视频信息。 https://x.com/OpenAI/status/1882509286439637448
```

<p align="center">
  <img src="./docs/assets/readme/examples/x.zh-CN.svg?v=5b966957" width="100%" alt="X: success">
</p>

[来源](https://x.com/OpenAI/status/1882509286439637448) · [原页截图](./docs/assets/readme/examples/originals/x.jpg) · 已取得帖子正文及视频、封面引用；本例未请求转写。

<details>
<summary>查看返回字段节选</summary>

```json
{
  "status": "success",
  "title": "A research preview of Operator, an agent that can use its own browser to perform...",
  "author": "OpenAI",
  "media_references": {
    "video": 1,
    "image": 1
  },
  "excerpt": "A research preview of Operator, an agent that can use its own browser to perform tasks for you."
}
```

[验证数据](./docs/assets/readme/examples/x.json)

</details>

#### YouTube · Agent 开发建议

```text
读取这个 Agent 视频，提取简介与章节。 https://www.youtube.com/watch?v=LP5OCa20Zpg
```

<p align="center">
  <img src="./docs/assets/readme/examples/youtube.zh-CN.svg?v=8ec739a6" width="100%" alt="YouTube: original video page and retrieved content">
</p>

[来源](https://www.youtube.com/watch?v=LP5OCa20Zpg) · [原视频截图](./docs/assets/readme/examples/originals/youtube-after-login.jpg) · 登录后已取得简介、章节与封面；字幕正文未取回。

<details>
<summary>查看登录前后返回字段</summary>

```json
{
  "before_sign_in": {
    "status": "error",
    "error": {
      "code": "LOGIN_REQUIRED",
      "message": "Sign in to confirm you’re not a bot"
    }
  },
  "after_sign_in": {
    "status": "success",
    "title": "Tips for building AI agents",
    "author": "Anthropic",
    "media_references": {
      "image": 1
    },
    "captions_available": true,
    "warnings": [
      "YouTube captions were advertised but could not be read"
    ],
    "excerpt": "00:26 Defining AI agents and workflows\n10:47 Coding agents: Potential and challenges\n16:26 Advice for developers exploring agents"
  }
}
```

[登录后验证数据](./docs/assets/readme/examples/youtube.json) · [登录前验证数据](./docs/assets/readme/examples/youtube-before-login.json)

</details>

### YouTube 登录流程

遇到 `LOGIN_REQUIRED`，让 Agent 打开专用的普通 Chrome 窗口：

```text
帮我打开 DigCrawl 的 YouTube 登录窗口。
```

自行完成 Google 登录并返回 YouTube，再告诉 Agent“已完成登录”。Agent 保存本机会话，重新读取原视频。

<p align="center">
  <img src="./docs/assets/readme/login/youtube-flow.zh-CN.svg?v=c99d80e5" width="100%" alt="YouTube 五步本机登录流程与登录前后原视频页面截图">
</p>

<details>
<summary>YouTube 手动登录命令与会话说明</summary>

```bash
digcrawl browser login --config "<MCP 实际使用的 profile 路径>" --platform youtube
```

在专用 Chrome 中登录并返回 YouTube，再在原终端按 Enter；程序关闭窗口后同步会话。Windows 用户需先关闭该专用 Chrome 的全部窗口再按 Enter。

使用与 MCP 相同的配置；日常浏览器的登录不会自动同步。登录信息与 Cookie 留在本机。若 Google 拒绝登录，请保留错误，参见[登录说明](./USAGE.md#平台登录与健康检查)。

</details>

### 需要登录时 · 小红书示例

遇到 `LOGIN_REQUIRED`，让 Agent 打开 DigCrawl 的专用登录窗口：

```text
帮我打开 DigCrawl 的小红书登录窗口。
```

自行完成登录后，回到 Agent 说“已完成登录”。Agent 保存本机会话，再重新读取原链接。

<p align="center">
  <img src="./docs/assets/readme/login/flow.zh-CN.svg?v=1009d3a7" width="100%" alt="小红书本机登录流程与真实界面截图">
</p>

<details>
<summary>手动登录命令与会话说明</summary>

```bash
digcrawl browser login --config "<MCP 实际使用的 profile 路径>" --platform xiaohongshu
```

手动执行命令时，登录后在原终端按 Enter 保存。使用与 MCP 相同的配置；日常浏览器的登录不会自动同步。登录信息与 Cookie 留在本机。

更多平台与排障见[登录说明](./USAGE.md#平台登录与健康检查)。

</details>

<details>
<summary>手动配置 Skill 与 MCP</summary>

安装完整 [Skill 目录](./.agents/skills/digcrawl/)，再按宿主配置本地服务：

- [Codex 示例](./.agents/skills/digcrawl/codex/config.toml.example)
- [Claude Code 示例](./.agents/skills/digcrawl/claude-code/.mcp.json.example)
- [其他 MCP 宿主](./.agents/skills/digcrawl/mcp/README.md)

使用程序与配置文件的绝对路径；不要只复制 `SKILL.md`。配置初始化见[接入指南](./USAGE.md#6-ide-集成mcp)。

</details>

## 本地优先与安全边界

- 在本机运行，HTTP Gateway 默认仅监听本机。
- 默认 Skill 无需 API Key，不向 Agent 暴露 Cookie。
- 需登录的来源使用用户授权的本地会话；登录与验证码由用户完成。
- 无法访问或触发频控时明确提示，不将错误页面当作正文。

### 浏览器恢复与诊断

遇到读取失败，请按错误提示操作，或查看[浏览器排障](./USAGE.md#浏览器超时或反复失败)。

<details>
<summary>检查本地环境</summary>

```bash
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

升级重连与 Bridge 生命周期见 [MCP 文档](./.agents/skills/digcrawl/mcp/README.md)。

</details>

## 架构与接入方式

| 入口 | 用途 |
| --- | --- |
| CLI / Watchlist | 本机抓取、批量处理与定时监控 |
| Skill / MCP | 为 AI Agent 提供内容读取工具 |
| HTTP / stdio / Go SDK | 集成到应用与数据工作流 |

内部模块与工具协议见 [Runtime](./runtime/README.md)、[Transport](./transport/README.md) 和 [Protocol](./protocol/README.md)。

## 文档

**使用者**

- [安装与使用](./USAGE.md) · [功能手册](./docs/DigCrawl功能与使用手册.md) · [浏览器排障](./USAGE.md#浏览器超时或反复失败)
- [Agent Skill](./.agents/skills/digcrawl/) · [MCP 接入](./.agents/skills/digcrawl/mcp/README.md)

**开发者**

- [HTTP / stdio](./transport/README.md) · [Go SDK](./client/README.md) · [配置样例](./config/)
- [Collect](./collect/README.md) · [Runtime](./runtime/README.md) · [Protocol](./protocol/README.md)

## 参与贡献

欢迎提交 Issue 和 PR，提供使用场景、复现步骤或测试。请勿提交凭据、Cookie、浏览器数据或私密内容。

## 开源说明

请遵守来源平台的条款、授权与适用法律，不绕过登录、验证码或访问限制。

## 许可证

[MIT License](./LICENSE)
