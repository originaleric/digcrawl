---
name: digcrawl
description: Read web and supported social-platform links through a locally installed DigCrawl MCP server. Use when a user asks to read, analyze, quote, compare, OCR, transcribe, or summarize a provided URL, especially WeChat articles, Xiaohongshu notes, Douyin or Toutiao videos, X posts, Reddit, RSS/Atom feeds, and supported Chinese social platforms. Use the host Agent to read returned image attachments and to summarize retrieved content; use local ASR only for an explicit video-transcription request. Preserve structured errors for unavailable, login-gated, captcha-gated, or rate-limited content.
---

# DigCrawl

Use the locally configured `digcrawl` MCP server for content retrieval. A normal read may refresh the user's local session state; it does not expose that state to the Agent. Do not archive, create watchlists, transcribe arbitrary local files, or perform browser actions through MCP.

## Read a URL

1. Confirm that the user supplied an `http` or `https` URL. Ask for one if absent.
2. Select a platform tool using `references/tool-routing.md`.
3. For a platform tool, call it with the URL and `format: ["markdown"]`. Set `include_media: false` and `include_screenshot: false` unless the user explicitly requests an image list or screenshot. When a screenshot is requested, return the current viewport image attachment together with its local artifact metadata. Attachments over `Gateway.ArtifactMaxInlineBytes` are rejected rather than being sent through MCP (default: 10 MiB).
4. Use `browser.browse` only when no platform tool applies. Call it with the URL and `mode: "full"`; do not send platform-only parameters. `http` and `https` pages work without a domain list. If the local profile configures `Gateway.AllowedDomains`, respect it as an additional restriction.
5. Present the resulting title, author, publication time, canonical URL, and Markdown before analysis. Keep the source URL in the answer.

## Image reading, ASR, and summary

Use enrichment only when the user explicitly requests it. Never request, accept, or expose a provider key in chat.

1. For image text or visual understanding, call the selected platform tool with `include_media: true` and `include_screenshot: true`. For a generic web page, call `browser.browse` with `mode: "full"` and `include_screenshot: true`. Do **not** send `include_ocr`: inspect the returned MCP image attachment yourself. The response must retain its text/Markdown alongside the attachment. Use returned `media` URLs as source references, but do not claim to have read an image that was not supplied as an attachment.
2. For video transcription, call the selected video platform tool with `include_asr: true` and `include_media: true`. If it reports that local media download or Skill ASR is disabled, explain that this one-time switch will let DigCrawl download the requested video's media locally and invoke the already configured ASR provider. Ask for explicit consent. Only after consent, call `skill.asr.enable` with `{}` and retry the original video request once in the same MCP session. The tool first verifies ffmpeg and the configured ASR provider; if they are unavailable, it does not write the opt-in—preserve that error and direct the user to complete the local dependency setup. On success it writes only the standard user Skill profile and takes effect immediately; never edit YAML, restart MCP, change the ASR provider, or request a key in chat.
3. For a requested video summary, first call the video tool with `include_asr: true` and `include_media: true`, then summarize the returned Markdown and `asr_text` yourself. Do not send `include_summary`; no local LLM key or DigFlow configuration is needed in DigCrawl.
4. Return the image-derived observations, `asr_text`, and your summary with clear source boundaries. Do not claim image reading or ASR succeeded when the requested attachment or local ASR result is unavailable.

## Handle local sessions and failures

Treat content access as local to the user's machine. Do not request, reveal, export, or upload cookies, tokens, browser profiles, or credentials.

Read `references/error-handling.md` whenever a tool returns an error or incomplete result. Preserve structured error codes such as `LOGIN_REQUIRED`, `CAPTCHA_REQUIRED`, `RATE_LIMITED`, `CONTENT_TYPE_MISMATCH`, and `DOMAIN_NOT_ALLOWED`. Explain the next safe user action; never invent article content or attempt to circumvent a platform restriction.

For `LOGIN_REQUIRED` on xiaohongshu, douyin, toutiao, threads, or instagram, explain that DigCrawl uses a separate local Cookie store rather than the user's everyday browser profile. Ask whether the user wants to open the DigCrawl login window. Only after explicit approval, and only when the host can keep an interactive local terminal session, run `digcrawl browser login --platform <platform>`. This opens a headed local browser at the platform login page; after the user reports login completion, send Enter to that same terminal session. Do not run the command automatically, request credentials, or access Cookie files. Retry the original read once only after the command exits successfully. If the host cannot keep interactive stdin, show the command for the user to run manually. The short command targets the standard user Skill profile; do not guess a custom MCP profile path. If the user deliberately registered MCP with a custom profile, ask them to run the matching `digcrawl browser login --config <profile> --platform <platform>` command themselves. For other platforms, preserve the login error and explain that no supported DigCrawl interactive-login command is available.

## Analyze retrieved content

After a successful read, summarize, answer questions, compare sources, or extract key points using only the returned content. Clearly distinguish direct source facts from any inference. If the response lacks Markdown but contains text, use the text and state that the platform did not return Markdown.

## Verify availability

Use `gateway.ping` only to diagnose whether the local DigCrawl server is available. If the MCP server is missing, direct the user to the appropriate local configuration example:

- Codex: `codex/config.toml.example`
- Claude Code: `claude-code/.mcp.json.example`

Both examples require absolute paths to the local `digcrawl` binary and the user's profile. Direct users to run `digcrawl init --profile skill` after installing a release; it writes the default profile to `$XDG_CONFIG_HOME/digcrawl/digcrawl-skill.yaml` (or `~/.config/digcrawl/` when XDG is unset; `%APPDATA%\\DigCrawl\\` on Windows). They can then run `digcrawl config validate --config <profile>` and `digcrawl doctor --config <profile>`. A copied `config/digcrawl-skill.example.yaml` remains an alternative for manual configuration. For an explicit video transcription request, use the consent flow for `skill.asr.enable`; it enables only `Skill.Media.DownloadMedia` and `Skill.ASR.Enabled` in that standard user profile. Image reading and summaries use the host Agent; the default profile has no OCR or LLM API configuration.
