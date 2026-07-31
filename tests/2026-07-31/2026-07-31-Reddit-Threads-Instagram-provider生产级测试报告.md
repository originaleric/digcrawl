# 2026-07-31-Reddit-Threads-Instagram provider 生产级测试报告

## 1. 测试结论

- 结论：通过，Instagram local session 真实账号路径未测
- 项目：DigCrawl
- 测试时间：2026-07-31
- 测试人：Codex
- 本轮核心输出要求：验证 Reddit、Threads、Instagram 新增/完善 provider、source、watchlist 接入、配置 opt-in、文档和构建回归。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go |
| 启动入口 | `cmd/digcrawl` |
| 测试命令 | `go test ./...`、`go build ./cmd/digcrawl`、真实外部 URL smoke |
| 配置文件 | `config/config.yaml`、`config/social.example.yml` |
| 依赖服务 | 本机 Chrome/Chromium、外部公开网页/RSS |
| 核心业务输出 | provider JSON、collect 输出、watchlist source item、结构化错误 |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| `Gateway.AllowedTools` / `Collector.AllowedTools` | configured | 已包含 `threads.post.read`、`instagram.content.read` |
| `LocalBrowser.AllowedDomains` | configured | 已包含 Threads/Instagram 域名 |
| `Social.Threads` / `Social.Instagram` | configured | 默认 `auth_mode=public`，不读取 Cookie |
| Threads public smoke | configured | 使用公开帖子 URL 验证 |
| Reddit public RSS smoke | configured | 使用 `r/artificial/.rss` 验证 |
| Instagram local session | skippable | 本轮不读取本机 Cookie，未验证登录态账号路径 |

## 4. 执行记录

| 步骤 | 命令/API/浏览器检查 | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 单元/集成回归 | `GOCACHE=/private/tmp/digcrawl-gocache go test ./...` | 通过 | 全仓包测试通过 |
| 构建 | `GOCACHE=/private/tmp/digcrawl-gocache go build -o /private/tmp/digcrawl-build-check ./cmd/digcrawl` | 通过 | binary 生成；sandbox 下 Go stat cache 写入有非阻塞提示 |
| CLI help | `/private/tmp/digcrawl-build-check browser login --help` | 通过 | help 包含 `threads, instagram` |
| Reddit RSS | `/private/tmp/digcrawl-build-check collect --config config/config.yaml --out /private/tmp/digcrawl-prod-social/reddit --format json https://www.reddit.com/r/artificial/.rss` | 通过 | `feed.read` 成功，输出 JSON |
| Threads 公开详情 | `/private/tmp/digcrawl-build-check collect --config config/config.yaml --out /private/tmp/digcrawl-prod-social/threads --format json https://www.threads.net/@zuck/post/Cuw_QlKxvbq` | 通过 | `threads.post.read` 成功，输出 JSON 和图片资产 |
| Threads 健康试读 | `/private/tmp/digcrawl-build-check browser check --platform threads --url https://www.threads.net/@zuck/post/Cuw_QlKxvbq --trial` | 通过 | trial ok，标题为 `Mark Zuckerberg (@zuck) on Threads` |
| Threads 当前域名回归 | `/private/tmp/digcrawl-build-check collect --config config/config.yaml --out /private/tmp/digcrawl-prod-social/threads-com --format json https://www.threads.com/@zuck/post/Cuw_QlKxvbq` | 通过 | `threads.post.read` 成功，`threads.net` 兼容并统一支持当前 `threads.com` |
| Threads 当前域名健康试读 | `/private/tmp/digcrawl-build-check browser check --platform threads --url https://www.threads.com/@zuck/post/Cuw_QlKxvbq --trial` | 通过 | trial ok，标题为 `Mark Zuckerberg (@zuck) on Threads` |
| Instagram 不可用页 | `/private/tmp/digcrawl-build-check collect --config config/config.yaml --out /private/tmp/digcrawl-prod-social/instagram-notfound --format json https://www.instagram.com/reel/C0QgrwRIo7h/` | 通过 | 返回 `Instagram content not found`，未作为正常内容导出 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 新 provider 可注册和路由 | collect detector 路由到新工具 | `threads.post.read`、`instagram.content.read` 已覆盖测试 | 通过 |
| watchlist source 可发现新平台详情 URL | source 从 HTML 链接生成稳定 content key | Threads/Instagram fixture 测试通过 | 通过 |
| local session 默认不启用 | 默认 public 不传 Cookie | 单测覆盖；source `Raw.auth_mode=public` 且无 `use_cookie_domain` | 通过 |
| local session 双重 opt-in | 全局与 source 同时 opt-in 才传 Cookie 域 | 单测覆盖当前 `www.threads.com` 域；旧 `threads.net` 链接仍兼容 | 通过 |
| provider 错误不误报无更新 | 不可用/登录/限流应暴露错误 | Instagram 不可用页返回结构化失败 | 通过 |
| 真实 Threads 公开详情 | 可产出正常 collect JSON | 公开帖子 smoke 通过 | 通过 |

## 6. Findings

本轮发现并已修复：

```text
[P2] browser check trial 未接入 Threads/Instagram
Location/Command: internal/platform/healthcheck/check.go；digcrawl browser check --platform threads --trial
Reproduction: 新平台 login spec 已存在，但 trial switch 只支持 xiaohongshu/douyin/toutiao。
Expected: 新平台可做健康试读或返回对应 provider typed error。
Actual: 返回“不支持试读平台 threads”。
Fix: healthcheck 接入 threads.ReadPost 和 instagram.ReadContent。

[P1] Instagram 不可用页面被当作正常内容导出
Location/Command: internal/platform/instagram/selectors.go；collect Instagram unavailable reel
Reproduction: 页面标题/正文显示 Post isn't available，但 selector 把 body text 当正文。
Expected: 不可用内容返回 CONTENT_NOT_FOUND。
Actual: 输出正常 JSON。
Fix: selector 优先识别 Post isn't available / unavailable title。

[P1] Threads 登录墙 body 抢先覆盖公开 meta
Location/Command: internal/platform/threads/selectors.go；collect public Threads post
Reproduction: 页面 body 有登录文案，但 head 存在 og:title/og:description。
Expected: public meta 足够时可采集公开摘要。
Actual: 返回 LOGIN_REQUIRED。
Fix: selector 先提取公开 meta，meta 不足时才判 LOGIN_REQUIRED。
```

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| Instagram local session 真实账号 | 本轮按默认 public 模式，不读取本机 Cookie | 后续使用本机登录态设置 `Social.Instagram.AuthMode=local_session` 和 source options `auth_mode=local_session` 后跑真实账号 |
| Threads/Instagram 评论/回复树 | 当前 MVP 以详情正文、媒体、列表发现为主 | 后续若业务需要评论摘要，再补 replies/comments 专用解析和测试 |
| Threads/Instagram profile 真实列表 | 默认 `browser_fallback=false`；真实 profile 多数依赖动态渲染 | 生产 source 建议显式配置 `browser_fallback=true`，再跑真实 watchlist source 测试 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| Threads collect smoke 输出 | `/private/tmp/digcrawl-prod-social/threads` | 公开帖子 JSON 和媒体资产 |
| Reddit RSS collect smoke 输出 | `/private/tmp/digcrawl-prod-social/reddit` | RSS JSON 输出 |
| 本报告 | `tests/2026-07-31/2026-07-31-Reddit-Threads-Instagram-provider生产级测试报告.md` | 生产级测试记录 |

## 9. 最终说明

本轮完成新增 provider/source/watchlist/配置/文档的落地和回归。默认 public 模式不读取本机 Cookie；需要使用 Threads/Instagram 登录态时必须通过本地配置和 source options 双重 opt-in。Instagram 真实登录态路径需要后续在有账号会话的本机环境补跑。
