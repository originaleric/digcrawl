# README visuals

- `hero.*.svg`: editable vector diagrams; source types and authorized local sessions, not a product screenshot.
- `examples/*.svg`: localized comparison cards, with the original page on the left and fields from actual DigCrawl responses on the right. These are not screenshots of an Agent interface. WeChat articles use a clearly labeled source excerpt because browser policy blocked the page screenshot. WeChat Channels pairs the user-provided video-card screenshot with the previously verified local transcript and a separately labeled host Agent summary.
- `examples/manifest.json` and individual JSON files: source URLs, tool names, retrieval status, public field excerpts, media-reference counts and response hashes. The Xiaohongshu image observation comes from the returned page screenshot. No cookies or signed media URLs are published.
- Xiaohongshu was read with the full source share link in an authorized local session. Only the canonical note URL is published; the copyable prompt asks readers to paste their own full share link.
- Bilibili, X and YouTube examples show metadata and media references without speech transcription. The WeChat Channels example includes 451 characters of actual local ASR, with a separate host Agent summary; it was validated on 2026-11-26. YouTube compares the actual pre-login `LOGIN_REQUIRED` response with the authenticated metadata read; caption text remained unavailable.

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

## WeChat Channels link guide

`wechat-channels/video-card.png` is the user-provided WeChat video-card screenshot. `wechat-channels/share-menu-redacted.png` is an edited sharing-menu screenshot: three other suggested recipients and their names were removed with imagegen; the first recipient was explicitly retained at the user’s request. The empty recipient control was then moved directly into the second slot with local pixel editing, without changing pixels elsewhere. The unredacted sharing-menu screenshot is not copied into the repository.

`wechat-channels/link-flow.*.svg` uses these screenshots to show opening the card, copying its link, sending it to the Agent, authorizing Yuanbao when required, and retrieving/transcribing the video. The complete privacy-edited sharing screenshot is shown, with “复制链接” outlined as an instructional annotation. All three complete screenshots are arranged in one row and scaled proportionally to fit their panels without cropping. Regenerate with `python3 docs/assets/readme/render_wechat_link_guide.py`.

`wechat-channels/yuanbao-authorization.png` is the complete user-provided Tencent Yuanbao browser screenshot, including its address bar, displayed without cropping at the user’s request. It illustrates the authorization prompt, not a completed sign-in or a successful saved session. The same dedicated window may show a QR code or WeChat quick authorization.
