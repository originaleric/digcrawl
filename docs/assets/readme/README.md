# README visuals

- `hero.*.svg`: editable vector diagrams; source types and authorized local sessions, not a product screenshot.
- `examples/*.svg`: localized comparison cards, with the original page on the left and fields from actual DigCrawl responses on the right. These are not screenshots of an Agent interface. WeChat uses a clearly labeled source excerpt because browser policy blocked its page screenshot.
- `examples/manifest.json` and individual JSON files: source URLs, tool names, retrieval status, public field excerpts, media-reference counts and response hashes. The Xiaohongshu image observation comes from the returned page screenshot. No cookies or signed media URLs are published.
- Xiaohongshu was read with the full source share link in an authorized local session. Only the canonical note URL is published; the copyable prompt asks readers to paste their own full share link.
- Video examples show metadata and media references. They do not claim speech transcription. YouTube compares the actual pre-login `LOGIN_REQUIRED` response with the authenticated metadata read; caption text remained unavailable.

`examples/originals/*.jpg` contains cropped original-page screenshots: Xiaohongshu from the DigCrawl screenshot attachment, the YouTube page after session synchronization from a DigCrawl screenshot attachment, and Bilibili, X and the pre-login YouTube page from the browser. Original page text and images remain their respective authors’ content; source links are recorded in `examples/manifest.json`.

Regenerate cards after editing verified data or screenshots with `python3 docs/assets/readme/render_examples.py`. README image URLs include a content hash to refresh cached previews.

The SVGs support light and dark system themes. English and Chinese use the same layout.

## Logo sources

- `logos/dig.png`: user-provided `DigLogo108.png`, copied unchanged and embedded in each hero SVG.
- `logos/codex.svg`, `logos/grok.svg`, `logos/claude.svg`: `@lobehub/icons-static-svg` version 1.95.1 (`codex.svg`, `grok.svg`, and `claude-color.svg`), embedded as vector paths. Source: https://github.com/lobehub/lobe-icons
- `logos/wechat.svg`, `logos/xiaohongshu.svg`, `logos/bilibili.svg`, `logos/x.svg`, `logos/youtube.svg`: Simple Icons 15.0.0, distributed under CC0; see `logos/LICENSE-simple-icons.txt`. Source: https://github.com/simple-icons/simple-icons
- Lobe Icons MIT license: `logos/LICENSE-lobe-icons.txt`. Product names and logos belong to their respective owners; their appearance identifies compatible agents.

## Login guide

`login/flow.*.svg` pairs the supported local login workflow with actual Xiaohongshu screens. `login/xiaohongshu-login.jpg` shows an anonymous, empty platform login form captured in the browser on 2026-10-17, for illustration; it is not a capture of completed authentication. It excludes the QR code and account details. The post screenshot comes from the earlier successful DigCrawl read after local session saving. Regenerate with `python3 docs/assets/readme/render_login.py`.

`login/youtube-flow.*.svg` shows the five-step manual Chrome sign-in and session synchronization flow. It pairs the original login-required page with the video page captured after session synchronization. `examples/originals/youtube-after-login.jpg` crops the public player, title and channel from that attachment; it is a source-page screenshot, not proof of a signed-in account or retrieved caption text.
