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
| Any MCP-compatible Agent | Follow the portable [MCP stdio contract](./.agents/skills/digcrawl/mcp/README.md): configure `digcrawl mcp --config <absolute-profile-path>` as a local stdio server. |

The Agent Skills format makes the complete `SKILL.md` bundle portable; MCP makes its stdio tool contract portable. Neither standard defines one universal host configuration file or Skill discovery path, so the bundle is intentionally host-neutral and the small registration adapters remain host-specific.

After configuration, simply give your Agent a supported URL and ask it to read, summarize, compare, quote, or—when explicitly requested—transcribe it. The Agent uses DigCrawl locally and preserves structured outcomes such as login required, captcha, or rate limiting.

For a supported platform video, explicitly ask the Agent to save that platform's videos. The Skill enables only the requested provider in the user-owned local `VideoDownload` profile and can use a user-specified absolute directory or its profile-local default. Bilibili, Douyin, Instagram, Kuaishou, Threads, Toutiao, Weibo, Xiaohongshu, YouTube, and Zhihu are eligible; X, Reddit, and WeChat are not in this video-retention path. Provider image retention is separate and opt-in through `ImageDownload`: it supports single-content providers (including X, Reddit, WeChat, and browser web pages), saves a complete bounded image set or none, and never enables OCR by itself. For a direct local file or media URL, use `digcrawl video`; `--keep <path>` controls retention for that one CLI invocation.

The Skill does not include API keys or expose browser cookies. See the [Agent Skill](./.agents/skills/digcrawl/) for routing behavior and [Use with AI Agents](#use-with-ai-agents) for security details.

## Quick Start

Install the current verified release on macOS or Linux—**Go is not required**:

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

The installer detects the operating system and CPU architecture, downloads the matching GitHub Release, verifies it against `checksums.txt`, and installs `digcrawl` to `~/.local/bin`. It also installs the complete portable Agent Skill under `$XDG_DATA_HOME/digcrawl/skills/digcrawl` (or `~/.local/share/digcrawl/skills/digcrawl`), creates the keyless Skill profile when needed, and prints the standard MCP stdio launch contract. It installs the Claude Code Skill when Claude is detected, and atomically registers (or repairs) the Codex adapter at its effective user or project configuration, always using the stable installed binary rather than a build or temporary path. It never uses `sudo` or changes shell configuration. `--skip-codex` leaves the portable setup intact; use `--skip-agent-setup` for a binary-only install. To review or pin the installer, download it first or pass a release tag:

```bash
curl -fsSLO https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh
sh install.sh --version vX.Y.Z
# Optional: keep the portable Skill but do not modify Codex.
sh install.sh --skip-codex
# Optional: install the binary only.
sh install.sh --skip-agent-setup
```

If `~/.local/bin` is not in `PATH`, add it using your shell’s normal configuration, then restart the shell. Windows users should download the matching `.zip` from [GitHub Releases](https://github.com/originaleric/digcrawl/releases). Full manual-install and checksum instructions are in the [usage guide](./USAGE.md).

After the installer registers or repairs Codex, restart Codex Desktop and create a new task so it loads the new MCP tool catalog. Existing tasks cannot retroactively acquire an MCP server that failed during their startup.

Confirm the binary and, if you used `--skip-agent-setup`, initialize a local Agent/MCP profile manually:

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

Public X posts are read through the public Syndication endpoint first, then retain the existing Tweet API, oEmbed, and browser fallbacks. This obtains post text, public images, and the best available direct MP4 reference when the endpoint supplies it; it neither uses cookies nor reads private or login-gated posts. `ImageDownload` is a separate, per-provider opt-in for retaining original images locally. It is independent from OCR and from Collect's `.assets` output: a requested image set is saved atomically within configured size, item, and timeout bounds, or not retained at all.

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

Each Release archive contains the full portable Skill at `skills/digcrawl/`; the installer keeps a stable copy in its printed data directory. Copy or link that whole directory into the user or project Skill location documented by your host, rather than copying only `SKILL.md`. The installer can repair the Codex adapter and installs the Claude Code Skill when detected, but it does not guess other hosts' configuration files.

For every MCP-capable host, register the local server with the absolute binary and profile path specified by the [generic stdio contract](./.agents/skills/digcrawl/mcp/README.md):

```toml
[mcp_servers.digcrawl]
command = "/absolute/path/to/digcrawl"
args = ["mcp", "--config", "/absolute/path/to/digcrawl-skill.yaml"]
```

Codex and Claude Code examples are available in the [Skill directory](./.agents/skills/digcrawl/). The first `digcrawl mcp` call starts one private, profile-scoped local Bridge; stdio children forward to that singleton rather than each owning a Runtime, Chrome, or fixed CDP port. Profile symlink aliases resolve to that same Bridge. A persistent `LocalBrowser.UserDataDir` may be owned by only one Bridge at a time; use a distinct directory for an intentionally separate configuration, rather than silently mixing their policies. `bridge serve` is an internal worker command; `mcp` is the only normal start entry point. The lifecycle has one OS file lock per profile and one worker-owned process group: the lock admits exactly one worker, and that worker records its private group only so a later starter can wait for Chrome to release the profile after an interrupted stop. `digcrawl bridge stop --config <profile>` directly kills the current worker group; it does not drain MCP work, make an HTTP shutdown request, or perform a rolling handoff. If a previously signalled group outlives its parent, stop holds the same lock and waits or reports its deadline—it never trusts an old numeric PID group enough to signal a potentially recycled group. A browser CDP transport loss during a read retires that connection and replays the idempotent read once within its original deadline. OCR is enrichment: when a configured OCR provider is unavailable, retrieved page text remains successful and `platform_metadata.ocr_status` is `degraded` with a stable reason. The release installer uses its verified staged binary to stop the standard profile's old Bridge before replacing the installed binary. The CLI instead reclaims unused stdio proxies: uninitialized pipes expire after one minute, and initialized sessions expire after 15 minutes without an actual `tools/call`; only `initialize` and `tools/call` count as activity, while MCP `ping` packets do not keep an inactive child alive. A positive `--idle-timeout` changes that initialized-session interval while retaining the one-minute initialization grace. Pass `--idle-timeout=0` only for a host that genuinely requires an unbounded stdio connection.

The default Skill is intentionally keyless. It uses the host agent’s multimodal model for page screenshots and image understanding; optional local ASR remains explicit and requires local dependencies and configuration.

Browser reads and platform fallbacks share one total browser deadline and at most one automatic fault retry. Cancellation, send-queue contention, login requirements, and page changes do not trigger unconditional replay. Navigation or document reload during extraction discards the mixed result. Confirmed browser, Feed/Reddit RSS, and merged Bilibili media sources are checked against the call policy before delivery, downloads, or storage. For `BROWSER_*` failures, `error_details` includes the failed stage, recovery outcome, `next_actions`, and `diagnostic_id`; an agent receiving `handled: true` should complete the indicated prerequisite before starting a new read. See the [troubleshooting and upgrade steps](./USAGE.md#浏览器超时或反复失败).

## Local-First Security

DigCrawl runs on the user’s machine and is designed to make its access boundaries visible:

- The HTTP Gateway listens locally by default.
- Tool allowlists, domain allow/block rules, and per-tool approval markers constrain execution.
- Browser sessions are used only through the configured local browser and are not returned as cookies or secrets.
- Sign-in, verification, private-content, and rate-limit conditions are exposed as structured results.
- Artifacts, audit records, and optional retention policies are managed separately from collected output.

Read the [runtime documentation](./runtime/README.md) before exposing a Gateway or Collector beyond a trusted local environment.

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

### Browser recovery and diagnostics

`BrowserBrowse` owns browser fault retries. Platform fallbacks share at most one additional attempt within one total browser deadline; platform and Collector retry settings no longer multiply these attempts. Normal content/selector fallbacks are counted separately. Media retention, OCR and ASR failures after a successful read do not replay the whole handler.

Read `browser_diagnostics` on success and `error_details` on failure for attempt counts, recovery status and next actions. `handled: true` means the invocation already made its retry decision; agents must complete the indicated prerequisite before a new read. A CDP send-queue timeout is local contention and does not restart a healthy browser.

`digcrawl doctor --config <actual profile>` checks dependencies, Bridge transport, build identity and browser operations separately. The existing Bridge creates and closes a blank test page; doctor never starts a competing browser. Before the first MCP connection an unreachable Bridge means the operation check was not executed. `digcrawl doctor --diagnostic-id <returned ID>` reads a bounded safe local summary.

After upgrading, close old DigCrawl MCP connections, run the installed binary's `digcrawl bridge restart --config <actual profile>`, then reconnect. Restart interrupts in-flight calls; clients never automatically swap Bridge versions. Occupied or unverified profiles require explicit resolution; login data and cookies are not deleted as a directory.
