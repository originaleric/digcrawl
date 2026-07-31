# 2026-07-31-第2轮-Reddit/Threads/Instagram真实URL生产级测试报告

## 1. 测试结论

- 结论：通过
- 项目：DigCrawl
- 测试时间：2026-07-31 13:52-13:55
- 测试人：Codex
- 本轮目标：对今天更新的 Reddit、Threads、Instagram 3 个渠道使用真实公开 URL 做生产级 smoke，验证 build、核心包回归、真实抓取、浏览器健康试读和输出质量。

## 2. 环境与配置

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| Go build | configured | 使用当前工作区代码构建 `/private/tmp/digcrawl-prod-20260731-round-social/digcrawl` |
| 配置文件 | configured | 使用 `config/config.yaml`，不读取 `.env` |
| 本机浏览器 | configured | Threads/Instagram browser path 可启动并完成 trial read |
| Reddit | configured | 使用公开 `r/artificial/.rss` |
| Threads | configured | 使用公开 `www.threads.com` 帖子 |
| Instagram | configured | 使用公开 NASA Earth Reel |
| 登录态 | not required | 本轮全部使用公开 URL；未启用 Instagram/Threads local session |

## 3. 执行记录

| 步骤 | 命令/URL | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 构建 | `GOCACHE=/private/tmp/digcrawl-gocache go build -o /private/tmp/digcrawl-prod-20260731-round-social/digcrawl ./cmd/digcrawl` | 通过 | binary 生成；sandbox 下 Go stat cache 有非阻塞提示 |
| 核心包回归 | `GOCACHE=/private/tmp/digcrawl-gocache go test ./internal/platform/threads ./internal/platform/instagram ./internal/platform/reddit ./internal/source ./provider/platform ./collect ./config` | 通过 | 相关包测试全部通过 |
| Reddit 真实 URL | `digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-round-social/reddit --format json https://www.reddit.com/r/artificial/.rss` | 通过 | `feed.read` 成功，输出 feed JSON |
| Threads 真实 URL | `digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-round-social/threads --format json https://www.threads.com/@zuck/post/Cuw_QlKxvbq` | 通过 | `threads.post.read` 成功，browser 采集耗时约 6.3s |
| Instagram 真实 URL | `digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-round-social/instagram --format json https://www.instagram.com/reel/DUa5b_BDmmD/` | 通过 | `instagram.content.read` 成功，browser 采集耗时约 4.1s |
| Threads 健康试读 | `digcrawl browser check --config config/config.yaml --platform threads --url https://www.threads.com/@zuck/post/Cuw_QlKxvbq --trial` | 通过 | trial ok，标题为 `Mark Zuckerberg (@zuck) on Threads` |
| Instagram 健康试读 | `digcrawl browser check --config config/config.yaml --platform instagram --url https://www.instagram.com/reel/DUa5b_BDmmD/ --trial` | 通过 | trial ok，识别 NASA Earth Reel 标题 |
| 全量回归 | `GOCACHE=/private/tmp/digcrawl-gocache go test ./...` | 通过 | 全仓 Go 测试通过 |
| Diff 检查 | `git diff --check` | 通过 | 无 whitespace/error marker 问题 |

## 4. 输出质量验收

| 渠道 | 输出文件 | 验收结果 |
| --- | --- | --- |
| Reddit | `/private/tmp/digcrawl-prod-20260731-round-social/reddit/2026-07-31/Artificial Intelligence (AI).json` | `platform=feed`、`content_type=feed`、`extraction_source=rss`、正文长度 32142 |
| Threads | `/private/tmp/digcrawl-prod-20260731-round-social/threads/2026-07-31/Mark Zuckerberg (@zuck) on Threads.json` | `platform=threads`、`content_type=thread`、canonical URL 为 `https://www.threads.com/@zuck/post/Cuw_QlKxvbq`、正文长度 55、媒体 8 个、发布时间 `2023-07-16T16:45:55.000Z` |
| Instagram | `/private/tmp/digcrawl-prod-20260731-round-social/instagram/2026-07-31/NASA Earth on Instagram_ _See that ice_ That’s sea ice. 🌊🧊_Sea ice forms when ocean water gets cold enough to fr.json` | `platform=instagram`、`content_type=video`、canonical URL 为 `https://www.instagram.com/reel/DUa5b_BDmmD/`、正文长度 1892、媒体 9 个、发布时间 `2026-02-06T14:45:59.000Z` |

## 5. Findings

本轮未发现 P0/P1/P2 缺陷。

## 6. 跳过项与残余风险

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| Threads/Instagram 登录态路径 | skipped | 本轮目标是公开真实 URL；未启用 `Social.<Platform>.AuthMode=local_session` |
| Watchlist DB 写入和通知链路 | skipped | 本轮只验证 3 个渠道 provider/source 相关真实 URL smoke，未写生产 watchlist source、未触发通知 |
| Instagram 公网页稳定性 | residual-risk | Instagram 非官方公开页面可能受登录墙、风控或页面结构变化影响；本轮真实 NASA Earth Reel 成功 |

## 7. 产物清单

| 产物 | 路径 |
| --- | --- |
| 测试 binary | `/private/tmp/digcrawl-prod-20260731-round-social/digcrawl` |
| Reddit 输出 | `/private/tmp/digcrawl-prod-20260731-round-social/reddit` |
| Threads 输出 | `/private/tmp/digcrawl-prod-20260731-round-social/threads` |
| Instagram 输出 | `/private/tmp/digcrawl-prod-20260731-round-social/instagram` |
| 本报告 | `tests/2026-07-31/2026-07-31-第2轮-Reddit-Threads-Instagram真实URL生产级测试报告.md` |
