---
name: digcrawl
description: Read web and supported social-platform links through a locally installed DigCrawl MCP server. Use when a user asks to read, summarize, analyze, quote, compare, or answer questions about a provided URL, especially WeChat articles, Xiaohongshu notes, Douyin or Toutiao videos, X posts, Reddit, RSS/Atom feeds, and supported Chinese social platforms. Preserve structured errors for unavailable, login-gated, captcha-gated, or rate-limited content.
---

# DigCrawl

Use the locally configured `digcrawl` MCP server for content retrieval in this phase: do not archive, download media, create watchlists, transcribe local files, or perform browser actions. A normal read may refresh the user's local session state; it does not expose that state to the Agent.

## Read a URL

1. Confirm that the user supplied an `http` or `https` URL. Ask for one if absent.
2. Select a platform tool using `references/tool-routing.md`.
3. For a platform tool, call it with the URL and `format: ["markdown"]`. Set `include_media: false` and `include_screenshot: false` unless the user explicitly requests an image list or screenshot. When a screenshot is requested, return the current viewport image attachment together with its local artifact metadata. Attachments over `Gateway.ArtifactMaxInlineBytes` are rejected rather than being sent through MCP (default: 10 MiB).
4. Use `browser.browse` only when no platform tool applies. Call it with the URL and `mode: "full"`; do not send platform-only parameters. `http` and `https` pages work without a domain list. If the local profile configures `Gateway.AllowedDomains`, respect it as an additional restriction.
5. Present the resulting title, author, publication time, canonical URL, and Markdown before analysis. Keep the source URL in the answer.

## Handle local sessions and failures

Treat content access as local to the user's machine. Do not request, reveal, export, or upload cookies, tokens, browser profiles, or credentials.

Read `references/error-handling.md` whenever a tool returns an error or incomplete result. Preserve structured error codes such as `LOGIN_REQUIRED`, `CAPTCHA_REQUIRED`, `RATE_LIMITED`, `CONTENT_TYPE_MISMATCH`, and `DOMAIN_NOT_ALLOWED`. Explain the next safe user action; never invent article content or attempt to circumvent a platform restriction.

## Analyze retrieved content

After a successful read, summarize, answer questions, compare sources, or extract key points using only the returned content. Clearly distinguish direct source facts from any inference. If the response lacks Markdown but contains text, use the text and state that the platform did not return Markdown.

## Verify availability

Use `gateway.ping` only to diagnose whether the local DigCrawl server is available. If the MCP server is missing, direct the user to the appropriate local configuration example:

- Codex: `codex/config.toml.example`
- Claude Code: `claude-code/.mcp.json.example`

Both examples require absolute paths to the local `digcrawl` binary and the user's copied `config/digcrawl-skill.example.yaml` profile. The profile's `LocalBrowser.CookieStoreDir` and `Gateway.ArtifactDir` placeholders must be replaced with user-owned absolute paths.
