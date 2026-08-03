---
name: digcrawl
description: Read web and supported social-platform links through a locally installed DigCrawl MCP server. Use when a user asks to read, analyze, quote, compare, OCR, transcribe, or summarize a provided URL, especially WeChat articles, Xiaohongshu notes, Douyin or Toutiao videos, X posts, Reddit, RSS/Atom feeds, and supported Chinese social platforms. OCR and ASR require explicit user intent and locally configured providers; summarize returned content in the host Agent by default. Preserve structured errors for unavailable, login-gated, captcha-gated, or rate-limited content.
---

# DigCrawl

Use the locally configured `digcrawl` MCP server for content retrieval. A normal read may refresh the user's local session state; it does not expose that state to the Agent. Do not archive, create watchlists, transcribe arbitrary local files, or perform browser actions.

## Read a URL

1. Confirm that the user supplied an `http` or `https` URL. Ask for one if absent.
2. Select a platform tool using `references/tool-routing.md`.
3. For a platform tool, call it with the URL and `format: ["markdown"]`. Set `include_media: false` and `include_screenshot: false` unless the user explicitly requests an image list or screenshot. When a screenshot is requested, return the current viewport image attachment together with its local artifact metadata. Attachments over `Gateway.ArtifactMaxInlineBytes` are rejected rather than being sent through MCP (default: 10 MiB).
4. Use `browser.browse` only when no platform tool applies. Call it with the URL and `mode: "full"`; do not send platform-only parameters. `http` and `https` pages work without a domain list. If the local profile configures `Gateway.AllowedDomains`, respect it as an additional restriction.
5. Present the resulting title, author, publication time, canonical URL, and Markdown before analysis. Keep the source URL in the answer.

## Explicit OCR and ASR

Use these only when the user explicitly requests extraction. They are driven entirely by the copied local profile's `Skill` section; never request, accept, or expose a provider key in chat.

1. For OCR, call the selected platform tool with `include_ocr: true` (and `include_media: true`). For a generic web page, call `browser.browse` with `include_ocr: true`; a requested screenshot is also an OCR candidate.
2. For video transcription, call the selected video platform tool with `include_asr: true` and `include_media: true`. The profile must set `Skill.Media.DownloadMedia: true`, use an absolute `Skill.Media.Dir`, and enable/configure `Skill.ASR`.
3. For a requested video summary, first call the video tool with `include_asr: true` and `include_media: true`, then summarize the returned Markdown and `asr_text` yourself. This is the default: no LLM key or DigFlow configuration is needed in DigCrawl.
4. Use `include_summary: true` only when the user explicitly requests DigCrawl's local LLM fallback and the local profile enables `Skill.Summary`. It calls only the profile's OpenAI-compatible BYOK endpoint after ASR succeeds; it never uses DigFlow. A missing transcript, invalid local LLM response, overlong summary, or verbatim transcript is an error.
5. Return `platform_metadata.ocr_results`, `asr_text`, and `summary` when present. Do not claim OCR, ASR, or a summary succeeded when the configured local provider reports an error.

## Handle local sessions and failures

Treat content access as local to the user's machine. Do not request, reveal, export, or upload cookies, tokens, browser profiles, or credentials.

Read `references/error-handling.md` whenever a tool returns an error or incomplete result. Preserve structured error codes such as `LOGIN_REQUIRED`, `CAPTCHA_REQUIRED`, `RATE_LIMITED`, `CONTENT_TYPE_MISMATCH`, and `DOMAIN_NOT_ALLOWED`. Explain the next safe user action; never invent article content or attempt to circumvent a platform restriction.

## Analyze retrieved content

After a successful read, summarize, answer questions, compare sources, or extract key points using only the returned content. Clearly distinguish direct source facts from any inference. If the response lacks Markdown but contains text, use the text and state that the platform did not return Markdown.

## Verify availability

Use `gateway.ping` only to diagnose whether the local DigCrawl server is available. If the MCP server is missing, direct the user to the appropriate local configuration example:

- Codex: `codex/config.toml.example`
- Claude Code: `claude-code/.mcp.json.example`

Both examples require absolute paths to the local `digcrawl` binary and the user's copied `config/digcrawl-skill.example.yaml` profile. The profile's `LocalBrowser.CookieStoreDir` and `Gateway.ArtifactDir` placeholders must be replaced with user-owned absolute paths. Enable `Skill.Media`, `Skill.OCR`, and `Skill.ASR` only for explicit extraction requests. `Skill.Summary` is an explicit local OpenAI-compatible fallback; the host Agent normally performs summaries.
