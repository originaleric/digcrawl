<h1 align="center">DigCrawl</h1>

<p align="center"><strong>Multi-platform content retrieval for AI agents.</strong></p>

<p align="center">Retrieve pages, text, images, and video from public and user-authorized signed-in sources.</p>

<p align="center">
  <a href="./README.zh-CN.md">中文</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#supported-sources">Sources</a> ·
  <a href="#collect-export-and-watch">Collect &amp; watch</a> ·
  <a href="#documentation">Docs</a> ·
  <a href="https://github.com/originaleric/digcrawl/releases">Releases</a>
</p>

<p align="center">
  <a href="./LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
  <a href="https://www.npmjs.com/package/digcrawl-skill"><img alt="npm: digcrawl-skill" src="https://img.shields.io/npm/v/digcrawl-skill.svg"></a>
  <img alt="Local-first" src="https://img.shields.io/badge/runtime-local--first-111111">
  <img alt="CLI, MCP, HTTP" src="https://img.shields.io/badge/interfaces-CLI%20%7C%20MCP%20%7C%20HTTP-0071e3">
</p>

<p align="center">
  <img src="./docs/assets/readme/hero.en.svg" width="100%" alt="DigCrawl retrieves pages, posts, images and video for your agent using user-authorized sessions">
</p>


## DigCrawl Skill: Run It in Your Agent

**Paste into your agent chat** (Codex, Grok Build, Claude Code, or similar):

```text
Install the DigCrawl Skill for me: https://github.com/originaleric/digcrawl
```

Follow the instructions to restart your agent or open a new chat, then send a link:

```text
Read and summarize this link: <URL>
```

## Quick Start

**Install with npx** (macOS / Linux / Windows; Node.js 20.19+):

```bash
npx digcrawl-skill install
```

Installs the program, complete Skill, and default profile. Configures Codex and installs the Claude Code Skill when detected.

**Or use the install script** (macOS / Linux; no Node.js or Go required):

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

Both methods verify downloads. Restart your agent and open a new chat after installation.

<details>
<summary>Choose an agent, install locations, and alternatives</summary>

```bash
npx digcrawl-skill install codex
npx digcrawl-skill install claude-code
npx digcrawl-skill status
```

- Configures Codex automatically and installs the Claude Code Skill when detected. See [MCP setup](./.agents/skills/digcrawl/mcp/README.md) for other hosts.
- Installs to `~/.local/bin`; add it to `PATH` if the command is not found.
- Windows users can also download a [Release](https://github.com/originaleric/digcrawl/releases). See [installation](./USAGE.md#1-安装) for version pinning and source builds.

</details>

## What You Can Do

| Use case | Capability |
| --- | --- |
| Content retrieval | Read pages, articles, posts, and source images |
| Video understanding | Retrieve available captions; optionally transcribe speech and recognize on-screen text |
| Ongoing collection | Process URL batches, monitor updates, and notify |
| Agent reading | Supply source content for summaries, comparisons, and research |

## Supported Sources

- **Posts and communities**: WeChat Official Accounts, Xiaohongshu, Weibo, Zhihu, X, Reddit, Threads, Instagram.
- **Video and content platforms**: Douyin, Toutiao, Bilibili, Kuaishou, YouTube.
- **Web and feeds**: Web pages, RSS, Atom, sitemaps, platform lists, and profiles.

See [source details](./USAGE.md#支持的内容来源) for platform capabilities and login requirements.

## Collect, Export, and Watch

Save retrieved content as Markdown, HTML, JSON, and optional media files, or monitor sources continuously with Watchlist.


<details>
<summary>Common CLI commands: pages, batches, video, and watchlists</summary>

```bash
# Single pages and batches
digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,html,json
digcrawl collect --file urls.txt --out ./collect-output

# Analyze local video using the configured processors
digcrawl video ./meeting.mp4

# Run once or monitor continuously
digcrawl watchlist run --once --config config/config.yaml
digcrawl watchlist serve --config config/config.yaml
```

Transcription and OCR require the relevant dependencies; see [video analysis](./USAGE.md#直接视频解析) and [Collect](./collect/README.md).

</details>

## Use with AI Agents

Original pages beside actual retrieval results (2026-10-17). WeChat uses a source excerpt; video examples did not run speech transcription.

### Article · WeChat

```text
Read this agent article and extract its text and sections. https://mp.weixin.qq.com/s/mbU0bE6BV5YfAsTmoY4H3A
```

<p align="center">
  <img src="./docs/assets/readme/examples/wechat.en.svg?v=c65d6772" width="100%" alt="WeChat: success">
</p>

[Source](https://mp.weixin.qq.com/s/mbU0bE6BV5YfAsTmoY4H3A) · Retrieved the article text, sections, and 7 image references.

<details>
<summary>View returned field excerpts</summary>

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

[Verification data](./docs/assets/readme/examples/wechat.json)

</details>

### Image Note · Xiaohongshu

```text
Read this agent image note and extract its text and images. <paste the full Xiaohongshu share link>
```

<p align="center">
  <img src="./docs/assets/readme/examples/xiaohongshu.en.svg?v=63ceefe9" width="100%" alt="Xiaohongshu: success">
</p>

[Source](https://www.xiaohongshu.com/explore/6ac20976000000001c00ca12) · [Original screenshot](./docs/assets/readme/examples/originals/xiaohongshu.jpg) · Used an authorized local session to retrieve the note text and 2 image references. Use the full share link.

<details>
<summary>View returned field excerpts</summary>

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

[Verification data](./docs/assets/readme/examples/xiaohongshu.json)

</details>

### Video

#### Bilibili · Agent Fundamentals

```text
Read this agent tutorial and extract its title, description, and parts. https://www.bilibili.com/video/BV1z3ZFYUE6K/
```

<p align="center">
  <img src="./docs/assets/readme/examples/bilibili.en.svg?v=bb1ce02b" width="100%" alt="Bilibili: success">
</p>

[Source](https://www.bilibili.com/video/BV1z3ZFYUE6K/) · [Original screenshot](./docs/assets/readme/examples/originals/bilibili.jpg) · Retrieved video metadata and media references; transcription was not requested.

<details>
<summary>View returned field excerpts</summary>

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

[Verification data](./docs/assets/readme/examples/bilibili.json)

</details>

#### X · Operator Demo

```text
Read this Operator video post and extract its text and video information. https://x.com/OpenAI/status/1882509286439637448
```

<p align="center">
  <img src="./docs/assets/readme/examples/x.en.svg?v=02a9c970" width="100%" alt="X: success">
</p>

[Source](https://x.com/OpenAI/status/1882509286439637448) · [Original screenshot](./docs/assets/readme/examples/originals/x.jpg) · Retrieved the post text, video reference, and cover reference; no transcription was requested.

<details>
<summary>View returned field excerpts</summary>

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

[Verification data](./docs/assets/readme/examples/x.json)

</details>

#### YouTube · Agent Building Advice

```text
Read this agent video and extract its description and chapters. https://www.youtube.com/watch?v=LP5OCa20Zpg
```

<p align="center">
  <img src="./docs/assets/readme/examples/youtube.en.svg?v=ceb49958" width="100%" alt="YouTube: original video page and retrieved content">
</p>

[Source](https://www.youtube.com/watch?v=LP5OCa20Zpg) · [Original video screenshot](./docs/assets/readme/examples/originals/youtube-after-login.jpg) · Retrieved the description, chapters, and cover after sign-in; caption text was not retrieved.

<details>
<summary>View fields before and after sign-in</summary>

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

[After sign-in evidence](./docs/assets/readme/examples/youtube.json) · [Before sign-in evidence](./docs/assets/readme/examples/youtube-before-login.json)

</details>

### YouTube Sign-In

On `LOGIN_REQUIRED`, ask your agent to open a dedicated, ordinary Chrome window:

```text
Open the DigCrawl sign-in window for YouTube.
```

Sign in to Google yourself, return to YouTube, then tell the agent “I’ve finished signing in.” The agent saves the local session and reads the original video again.

<p align="center">
  <img src="./docs/assets/readme/login/youtube-flow.en.svg?v=fb10ec86" width="100%" alt="Five-step local YouTube sign-in flow with original video screenshots before and after session synchronization">
</p>

<details>
<summary>YouTube manual sign-in and session details</summary>

```bash
digcrawl browser login --config "<profile path used by MCP>" --platform youtube
```

Sign in through the dedicated Chrome window and return to YouTube, then press Enter in the original terminal. DigCrawl closes the window and synchronizes the session. On Windows, close all dedicated Chrome windows before pressing Enter.

Use the same profile as MCP; your everyday browser session is not copied automatically. Credentials and cookies stay on your machine. Preserve any Google sign-in rejection; see [sign-in guidance](./USAGE.md#平台登录与健康检查).

</details>

### When Sign-In Is Required · Xiaohongshu Example

On `LOGIN_REQUIRED`, ask your agent to open DigCrawl’s dedicated sign-in window:

```text
Open the DigCrawl sign-in window for Xiaohongshu.
```

Sign in yourself, then tell the agent “I’ve finished signing in.” The agent saves the local session and reads the original link again.

<p align="center">
  <img src="./docs/assets/readme/login/flow.en.svg?v=b147dc73" width="100%" alt="Local Xiaohongshu sign-in flow with actual platform screenshots">
</p>

<details>
<summary>Manual sign-in and session details</summary>

```bash
digcrawl browser login --config "<profile path used by MCP>" --platform xiaohongshu
```

When running the command yourself, press Enter in that terminal after signing in. Use the same profile as MCP; your everyday browser session is not copied automatically. Credentials and cookies stay on your machine.

See [sign-in guidance](./USAGE.md#平台登录与健康检查) for other platforms and troubleshooting.

</details>

<details>
<summary>Manual Skill and MCP configuration</summary>

Install the complete [Skill directory](./.agents/skills/digcrawl/), then configure the local server for your host:

- [Codex example](./.agents/skills/digcrawl/codex/config.toml.example)
- [Claude Code example](./.agents/skills/digcrawl/claude-code/.mcp.json.example)
- [Other MCP hosts](./.agents/skills/digcrawl/mcp/README.md)

Use absolute program and profile paths, and copy the whole Skill rather than only `SKILL.md`. See [setup](./USAGE.md#6-ide-集成mcp) for profile initialization.

</details>

## Local-First Security

- Runs on your machine; HTTP Gateway listens on localhost by default.
- The default Skill needs no API key and does not expose cookies to the agent.
- Signed-in sources use user-authorized local sessions; users handle login and captchas.
- Access failures and rate limits are reported explicitly; error pages are not treated as content.

### Browser recovery and diagnostics

Follow the returned error guidance or see [browser troubleshooting](./USAGE.md#浏览器超时或反复失败).

<details>
<summary>Check the local environment</summary>

```bash
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

See the [MCP documentation](./.agents/skills/digcrawl/mcp/README.md) for upgrade reconnection and Bridge lifecycle details.

</details>

## Architecture and Interfaces

| Interface | Use |
| --- | --- |
| CLI / Watchlist | Local retrieval, batches, and scheduled monitoring |
| Skill / MCP | Content reading tools for AI agents |
| HTTP / stdio / Go SDK | Integration with applications and data workflows |

See [Runtime](./runtime/README.md), [Transport](./transport/README.md), and [Protocol](./protocol/README.md) for internal modules and tool contracts.

## Documentation

**For users**

- [Installation and usage](./USAGE.md) · [Product guide](./docs/DigCrawl功能与使用手册.md) · [Browser troubleshooting](./USAGE.md#浏览器超时或反复失败)
- [Agent Skill](./.agents/skills/digcrawl/) · [MCP setup](./.agents/skills/digcrawl/mcp/README.md)

**For developers**

- [HTTP / stdio](./transport/README.md) · [Go SDK](./client/README.md) · [Configuration examples](./config/)
- [Collect](./collect/README.md) · [Runtime](./runtime/README.md) · [Protocol](./protocol/README.md)

## Contributing

Issues and PRs are welcome. Include your use case, reproduction steps, or tests. Never commit credentials, cookies, browser data, or private content.

## Open Source Notes

Respect source terms, permissions, and applicable law. Do not bypass login, captchas, or access restrictions.

## License

[MIT License](./LICENSE)
