<h1 align="center">DigCrawl</h1>

<p align="center">
  Local-first content collection and normalization for people, pipelines, and AI agents.
</p>

<p align="center">
  Read web and supported social content, then export reliable Markdown, HTML, JSON, media metadata, and structured diagnostics.
</p>

<p align="center">
  <a href="./README.zh-CN.md">中文</a> ·
  <a href="./USAGE.md">Usage</a> ·
  <a href="./docs/DigCrawl功能与使用手册.md">Documentation</a> ·
  <a href="./.agents/skills/digcrawl/">Agent Skill</a> ·
  <a href="https://github.com/originaleric/digcrawl/releases">Releases</a>
</p>

<p align="center">
  <a href="./LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-blue.svg"></a>
  <img alt="Language: Go" src="https://img.shields.io/badge/language-Go-00ADD8">
  <img alt="Interfaces: CLI, MCP, HTTP" src="https://img.shields.io/badge/interfaces-CLI%20%7C%20MCP%20%7C%20HTTP-2aa7b8">
  <img alt="Runs locally" src="https://img.shields.io/badge/runtime-local--first-111111">
</p>

DigCrawl is a local-first content collection and normalization engine. It reads public web pages and supported social platforms; when a source requires it, it can reuse a user-approved local browser session. The result is structured content that people, data workflows, and AI agents can use directly.

```text
URL or source list
        │
        ▼
identify → collect → normalize → export / index / notify
        │                 │
        │                 └── Markdown · HTML · JSON · media metadata · diagnostics
        ▼
static HTML / API / RSS first, local browser fallback when configured
```

## DigCrawl Skill: Run It in Your Agent

DigCrawl includes an official [Agent Skill](./.agents/skills/digcrawl/) for Codex, Claude Code, and other MCP-compatible agents. The Skill gives an agent platform-aware link routing, safe handling for sign-in and rate-limit states, and clear boundaries around local browser sessions.

Once the `digcrawl` binary is installed, the setup has three steps:

```bash
# 1. Create the keyless local Skill profile.
digcrawl init --profile skill

# 2. Verify the profile and local runtime.
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"

# 3. Start the MCP server (your Agent normally starts this from its configuration).
digcrawl mcp --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

Add the same local MCP command to the Agent you use:

| Agent | Configuration example |
| --- | --- |
| Codex | [`codex/config.toml.example`](./.agents/skills/digcrawl/codex/config.toml.example) |
| Claude Code | [`claude-code/.mcp.json.example`](./.agents/skills/digcrawl/claude-code/.mcp.json.example) |
| Any MCP-compatible Agent | Configure `digcrawl mcp --config <absolute-profile-path>` as a local stdio server. |

After configuration, simply give your Agent a supported URL and ask it to read, summarize, compare, quote, or—when explicitly requested—transcribe it. The Agent uses DigCrawl locally and preserves structured outcomes such as login required, captcha, or rate limiting.

For a platform video that the MCP tool downloads during enrichment, say “keep downloaded videos”, “clean up downloaded videos after processing”, or “set the video download limit to 512 MiB”. The Skill updates only the user-owned media retention or size setting through its local configuration tool; it does not require a platform-specific path or edit the profile in chat. For a direct local file or media URL, use `digcrawl video`; `--keep <path>` controls retention for that one CLI invocation.

The Skill does not include API keys or expose browser cookies. See the [Agent Skill](./.agents/skills/digcrawl/) for routing behavior and [Use with AI Agents](#use-with-ai-agents) for security details.

## Quick Start

Install the current verified release on macOS or Linux—**Go is not required**:

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

The installer detects the operating system and CPU architecture, downloads the matching GitHub Release, verifies it against `checksums.txt`, and installs only `digcrawl` to `~/.local/bin`. It never uses `sudo` or changes shell configuration. To review or pin the installer, download it first or pass a release tag:

```bash
curl -fsSLO https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh
sh install.sh --version vX.Y.Z
```

If `~/.local/bin` is not in `PATH`, add it using your shell’s normal configuration, then restart the shell. Windows users should download the matching `.zip` from [GitHub Releases](https://github.com/originaleric/digcrawl/releases). Full manual-install and checksum instructions are in the [usage guide](./USAGE.md).

Confirm the binary, then initialize a local Agent/MCP profile:

```bash
digcrawl version
digcrawl init --profile skill
digcrawl config validate --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

To enable local video transcription, prepare the same Skill profile's isolated runtime and model once:

```bash
digcrawl asr setup --config "$HOME/.config/digcrawl/digcrawl-skill.yaml" --scope skill --yes
digcrawl asr check --config "$HOME/.config/digcrawl/digcrawl-skill.yaml" --scope skill
```

On macOS and Linux, the default profile is stored in `$XDG_CONFIG_HOME/digcrawl/` (or `~/.config/digcrawl/`). On Windows it is stored in `%APPDATA%\DigCrawl\`. `init` never overwrites an existing profile unless `--force` is supplied.

Developers who want to build from source need Go 1.26.1+; see the [development installation instructions](./USAGE.md#从源码运行). For installation, release verification, configuration, and troubleshooting, see the [usage guide](./USAGE.md).

## What You Can Do

| You want to… | Use DigCrawl to… | Start with |
| --- | --- | --- |
| Archive a single page | Extract a page or post into portable Markdown, HTML, JSON, and optional media assets. | `digcrawl collect <url>` |
| Collect a queue of URLs | Process a file or local inbox and retain an index or database record. | `digcrawl collect --file urls.txt` |
| Analyze a video file or direct URL | Extract speech transcripts and on-screen text from a local video or direct media URL. | `digcrawl video ./video.mp4` |
| Monitor new content | Discover new items from feeds, lists, profiles, or pages, then collect and notify on a schedule. | `digcrawl watchlist run --once` |
| Give an AI agent reading tools | Run a local MCP server with the official Skill and platform-aware tool routing. | `digcrawl mcp --config <profile>` |
| Integrate a host application | Use HTTP Gateway, Collector, stdio, or the Go SDK. | [transport docs](./transport/README.md) |

## Supported Sources

DigCrawl treats crawling as a stable pipeline—**crawl, collect, normalize, export**—rather than as a browser gateway. It uses static HTML, platform APIs, and RSS/Atom before falling back to a local browser where that path is enabled.

| Source group | Supported sources |
| --- | --- |
| Social content | WeChat Official Accounts, Xiaohongshu, Douyin, Toutiao, X, Reddit, Threads, Instagram, Bilibili, Weibo, Kuaishou, Zhihu, and YouTube |
| Syndication and discovery | RSS, Atom, sitemap, platform lists, profile pages, and supported content feeds |
| The open web | Any `http` or `https` page through the generic browser reader |

Platform readers return structured states such as `LOGIN_REQUIRED`, `CAPTCHA_REQUIRED`, `RATE_LIMITED`, and `CONTENT_TYPE_MISMATCH`. A blocked or private page is never silently exported as valid content.

See [supported platforms and source boundaries](./USAGE.md) for URL patterns, authentication behavior, and configuration.

## Collect, Export, and Watch

Collect one URL or a batch:

```bash
./digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,html,json
./digcrawl collect --file urls.txt --format md,json --out ./collect-output
./digcrawl collect inbox --watch
```

Analyze a local video or a directly downloadable video URL:

```bash
# Run processors enabled in the ASR and video-frame OCR configuration.
./digcrawl video ./meeting.mp4

# Run both ASR and frame OCR, then export Markdown.
./digcrawl video "https://cdn.example.com/meeting.mp4" --mode both --format md --out meeting.md

# Run speech transcription or on-screen text recognition only.
./digcrawl video ./meeting.mp4 --mode asr
./digcrawl video ./meeting.mp4 --mode ocr
```

Video URLs must point directly to downloadable media; use the relevant collection tool for platform page URLs. Remote videos are deleted after processing unless `--keep ./video.mp4` is supplied. Unless `--config` is supplied, active root ASR/OCR settings in `config/config.yaml` take priority and inactive processors can fall back to the active settings in the standard user Skill profile.

Run a watchlist once, or keep its scheduler running locally:

```bash
./digcrawl watchlist run --once --config config/config.yaml
./digcrawl watchlist serve --config config/config.yaml
```

Watchlist execution is serial by default. To opt in to bounded parallel source
runs and item processing, configure Watchlist.Scheduler.MaxConcurrentRuns and
Watchlist.Collect.MaxConcurrentItems; shared collection output paths,
index.json, and per-platform request spacing remain protected.

Typical output is organized for both humans and downstream programs:

```text
collect-output/
  index.json
  <platform-or-web>/<slug>/
    content.md
    content.html
    content.json
    .assets/
```

DigCrawl can persist collection records in `index.json`, SQLite, or both. Watchlists can use YAML, SQLite, or MySQL according to the selected configuration.

## Use with AI Agents

The repository includes the official [DigCrawl Agent Skill](./.agents/skills/digcrawl/). It teaches compatible agents how to route links to the right local MCP tool, handle sign-in and rate-limit states, and avoid exposing a user’s cookies.

Register the local MCP server with an absolute binary and profile path:

```toml
[mcp_servers.digcrawl]
command = "/absolute/path/to/digcrawl"
args = ["mcp", "--config", "/absolute/path/to/digcrawl-skill.yaml"]
```

Codex and Claude Code examples are available in the [Skill directory](./.agents/skills/digcrawl/). Other MCP clients use the same stdio command in their own configuration format.

The default Skill is intentionally keyless. It uses the host agent’s multimodal model for page screenshots and image understanding; optional local ASR remains explicit and requires local dependencies and configuration.

## Local-First Security

DigCrawl runs on the user’s machine and is designed to make its access boundaries visible:

- The HTTP Gateway listens locally by default.
- Tool allowlists, domain allow/block rules, and per-tool approval markers constrain execution.
- Browser sessions are used only through the configured local browser and are not returned as cookies or secrets.
- Sign-in, verification, private-content, and rate-limit conditions are exposed as structured results.
- Artifacts, audit records, and optional retention policies are managed separately from collected output.

Read the [runtime documentation](./runtime/README.md) before exposing a Gateway or Collector beyond a trusted local environment.

## Latest Highlights

| Date | Highlight | Why it matters |
| --- | --- | --- |
| 2026-09-04 | Unified local ASR setup, configuration scopes, and readiness checks. | Release users can install faster-whisper without Go; setup, transcription, and `asr check` share one root/Skill cache domain, while custom runners are reported as externally managed rather than falsely ready. |
| 2026-08-31 | Stabilized browser sessions and Watchlist login recovery. | Canonical sessions prevent background readers from overwriting a completed login; every Watchlist entry generates its recovery command from the running executable and loaded configuration. |
| 2026-08-30 | Added direct local-file and direct-media-URL video analysis. | `digcrawl video` reuses bounded ASR and frame OCR, cleans up URL downloads by default, and can use the user Skill profile without weakening an explicit project configuration. |
| 2026-08-29 | Added opt-in Watchlist source/item concurrency with safe collection coordination. | Discovery and processing can use bounded goroutines while content keys, output paths, index.json, and platform request spacing remain controlled. |
| 2026-08-26 | Added explicit Skill video-retention preferences and safe profile updates. | Agents can retain or clean up downloaded videos and set a download cap without editing YAML; concurrent local profile writes are serialized and cancellable. |
| 2026-08-26 | Finalized execution isolation and external-ID boundaries for video understanding. | Each Skill media operation owns a distinct workspace; external call IDs correlate requests only and cannot overwrite local media or manifests. |
| 2026-08-26 | Decoupled downloaded-video understanding for YouTube, Skill, and Collect. | Providers own download/cleanup; ASR and frame OCR accept only a local video path and return content. MP4 retention is explicit opt-in and Collect export remains a separate delivery path. |
| 2026-08-25 | Added public YouTube video/Shorts reading, caption enrichment, and Watchlist discovery. | URLs are normalized and player identity is verified before content is accepted. |
| 2026-08-24 | Video-frame OCR was production-validated with an OpenAI-compatible vision provider. | Downloaded local videos can yield burned-in subtitle and on-screen text even when ASR has no usable speech; blank frames remain normal empty results rather than provider failures. |
| 2026-08-17 | Browser recovery, CDP service management, and local ASR startup were rebuilt around explicit lifecycle and cache state. | Browser profiles are not relaunched before they are released; Playwright cache paths are discovered dynamically; existing local ASR models and upgraded runners remain usable across restarts. |
| 2026-08 | OCR uses DigCrawl’s OpenAI-compatible vision client and remains opt-in. | Image enrichment stays independent of DigFlow and does not require a bundled key; downloaded local videos can additionally use explicit, bounded frame OCR for burned-in subtitles. |
| 2026-07 | ASR supports API-compatible and local `faster-whisper` providers. | Video and audio collections can include explicit, locally controlled transcripts. |
| 2026-07 | Watchlists gained persistent sources, task scheduling, summaries, and notifications. | Sources can be monitored repeatedly instead of collected only once. |
| 2026-07 | Reddit, RSS/Atom, Bilibili, Weibo, Kuaishou, Zhihu, Threads, and Instagram support expanded. | More source types share the same normalized collection pipeline. |

For operational details and the full change history, consult the [usage guide](./USAGE.md) and project documentation.

## Architecture and Interfaces

| Layer | Responsibility |
| --- | --- |
| `collect/` | URL recognition, collection orchestration, export, indexing, and inbox processing |
| `internal/source/` | Feed, sitemap, page, and platform discovery for Watchlist sources |
| `internal/platform/` and `internal/browser/` | Platform readers, local browser sessions, and fallback extraction |
| `runtime/` | Tool registry, policy, audit, artifacts, and unified execution |
| `transport/` | HTTP Gateway, reverse Collector, MCP, stdio, and development host |
| `protocol/` | Tool manifest, call, and result contracts |
| `client/` | Go SDK for calling a DigCrawl HTTP Gateway |

Use DigCrawl as a local CLI, a long-running Watchlist service, an HTTP Gateway, a reverse Collector, an MCP server, a stdio process, or a Go library integration.

## Documentation

| Document | Contents |
| --- | --- |
| [Usage guide](./USAGE.md) | Installation, configuration, CLI commands, collect flow, APIs, and FAQ |
| [Chinese product guide](./docs/DigCrawl功能与使用手册.md) | Feature overview and practical operating guide |
| [Agent Skill](./.agents/skills/digcrawl/) | Agent routing, error handling, and MCP client examples |
| [Collect](./collect/README.md) | Output, persistence, and collection workflow |
| [Protocol](./protocol/README.md) | `ToolManifest`, `ToolCall`, and `ToolResult` |
| [Runtime](./runtime/README.md) | Policy, audit, and artifacts |
| [Transport](./transport/README.md) | HTTP, Collector, MCP, stdio, and host integration |
| [Configuration examples](./config/) | Safe-to-copy configuration templates |

## Contributing

Contributions are welcome. Please start by opening an issue describing the source, use case, expected behavior, and any platform restrictions. Never submit credentials, browser profiles, cookies, real access tokens, or private collected content.

When adding or changing a platform reader, include reproducible fixtures or tests where the platform terms and content access allow it, and preserve structured diagnostics for login, verification, rate limiting, and unsupported content types.

## Open Source Notes

DigCrawl is a local content tool. You are responsible for complying with the terms of service, permissions, and applicable law for every source you collect. Do not use it to bypass authentication, access controls, captchas, or content restrictions.

## License

[MIT](./LICENSE)
