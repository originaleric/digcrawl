# DigCrawl 功能与使用手册

本文面向日常使用和业务接入，说明 DigCrawl 当前能做什么、如何配置、如何运行，以及遇到平台登录、动态页面、摘要通知和持久化时应该怎么维护。完整字段级配置仍以根目录 `README.md` 和 `USAGE.md` 为准。

## 1. 核心定位

DigCrawl 是本地内容采集与平台读取引擎，核心链路是：

```text
source/list discover -> collect detail -> normalize -> media/OCR/ASR enrich -> summary -> notify/export/persist
```

它可以作为 CLI 工具单独运行，也可以作为本地 Gateway、MCP、stdio 或 Go SDK 供 Agent/宿主系统调用。需要登录态的平台通过本机浏览器和本地 Cookie store 完成读取，不把账号密码写入仓库。

## 2. 主要能力

| 能力 | 说明 |
|------|------|
| 单 URL 采集 | `digcrawl collect <url>` 输出 Markdown、HTML、JSON，并可下载媒体 |
| 多平台 provider | 微信公众号、小红书、抖音、头条文章/视频、X、Reddit、Threads、Instagram、B站、微博、快手、知乎、YouTube、RSS/Atom、普通网页 |
| Watchlist | 定期扫描来源页、列表页、RSS、subreddit/user 等，发现新增内容后采集详情 |
| 定时任务 | `watchlist serve` 读取 `Tasks` 和 watchlist source 周期，支持 interval/daily |
| 持久化清单 | Watchlist source 可来自 YAML、数据库或 mixed，数据库支持 SQLite/MySQL |
| 摘要 Agent | 支持 extractive、OpenAI-compatible API、DigFlow Runtime summary |
| 飞书通知 | Watchlist 新增内容或无更新通知可推送到 Feishu |
| OCR/ASR | 图片 OCR、视频下载后 ffmpeg + ASR 转写，视频摘要可把 ASR 文本交给 summary agent |
| 本机浏览器 | 复用 Chrome/Chromium、Cookie、allowed domain 和本地登录态 |

## 3. 平台与抓取路径

| 平台 | 详情读取 | 列表/Watchlist 发现 |
|------|----------|---------------------|
| 微信公众号 | HTTP HTML 静态解析优先，必要时浏览器兜底 | `wechat_list` 通过链接发现文章 |
| 小红书 | 页面上下文/API 尝试 + 浏览器兜底 | `xiaohongshu_user` 发现笔记 |
| 抖音 | 页面上下文/API 尝试 + 浏览器兜底 | `douyin_user` 发现视频/图文 |
| 今日头条文章 | API 探测 + 多策略浏览器兜底 | `toutiao_author`、`?tab=wtt`、文章/微头条列表 |
| 今日头条视频 | 视频 provider + 下载/ASR/summary 链路 | `toutiao_video`、`?tab=video` 视频列表 |
| X/Twitter | Tweet lookup API、oEmbed、浏览器兜底 | `x_user`、`x_post` |
| Reddit | OAuth API、RSS fallback、可选浏览器兜底 | subreddit/user/comment source |
| Threads | 非官方公开网页/浏览器路径 | `threads_user`、`threads_post` |
| Instagram | 非官方公开网页/浏览器路径 | `instagram_user`、`instagram_post`、`instagram_reel` |
| B站 | 视频 API-first，浏览器兜底 | `bilibili_user` API-first，失败后 HTML/browser fallback |
| 微博 | post API-first，浏览器兜底 | `weibo_user` API-first，失败后 HTML/browser fallback |
| 快手 | Web GraphQL API-first，本机 Cookie/browser 兜底 | `kuaishou_user` API-first，失败后 HTML/browser fallback |
| 知乎 | 浏览器读取 | `zhihu_user` browser/html fallback，平台 selector 等待动态内容 |
| YouTube | 浏览器读取页面元数据与公开字幕轨 | `youtube_channel` 发现视频和 Shorts；不导出临时播放流 URL |
| RSS/Atom | Feed service | Feed service |

本机浏览器路径底层统一走 `internal/browser.BrowserBrowse`。头条、新增平台和列表 fallback 复用同一套 Chrome/Chromium 配置、Cookie store、allowed domain 和登录态；差别在于头条详情 provider 已有更完整的多策略 fallback，新平台详情 provider 后续会逐步补齐。Threads/Instagram 默认是 `auth_mode=public`，不读取本机 Cookie；只有全局 `Social.<Platform>.AuthMode=local_session` 和 source options `auth_mode=local_session` 同时开启时才复用登录态。

### Agent Skill 与 MCP

仓库内的 [`.agents/skills/digcrawl`](../.agents/skills/digcrawl/) 提供了面向 Codex、Claude Code 及其他 MCP 客户端的只读链接读取规范。开源用户从 [GitHub Releases](https://github.com/originaleric/digcrawl/releases) 下载对应系统与架构的二进制、校验 `checksums.txt` 并放入 `PATH` 后，使用以下命令生成并检查用户级最小 profile：

```bash
digcrawl init --profile skill
digcrawl config validate --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

macOS / Linux 默认目录为 `$XDG_CONFIG_HOME/digcrawl/`（XDG 未设置时为 `~/.config/digcrawl/`），Windows 为 `%APPDATA%\DigCrawl\`。`init` 同时生成 profile 同级的跨平台绝对 `Skill.Media.Dir`，因此无需在文档或 host 中写死用户名路径。`init` 不会覆盖已有文件，只有传入 `--force` 才会替换。也可以复制 [`config/digcrawl-skill.example.yaml`](../config/digcrawl-skill.example.yaml) 到任意用户目录自行维护；不论哪种方式，MCP 注册始终使用二进制与 profile 的绝对路径：

```bash
digcrawl mcp --config /absolute/path/to/digcrawl-skill.yaml
```

Codex、Claude Code、WorkBuddy 等宿主只需按各自格式登记这条 stdio 命令，DigCrawl 不会自动改写宿主配置。Homebrew tap 发布后可使用 `brew install --cask originaleric/tap/digcrawl`，它仍基于同一 GitHub Release。

Skill 默认只读取链接；归档、Watchlist、浏览器交互和任意本地文件转写仍不暴露。图片文字和视觉分析由 Codex、Claude Code 等宿主读取 DigCrawl 返回的页面截图附件完成：平台工具使用 `include_media` 与 `include_screenshot`，但不使用本地 `include_ocr`。`media` URL 是来源引用，若没有 MCP 图片附件，宿主不得声称已识别该图片。视频 ASR 与抽帧 OCR 是显式本地能力：Provider 下载受限临时 MP4，通用 core 仅接收该本地文件并返回理解结果，不感知平台、artifact 或 Collect；默认处理完成即清理。用户明确说“保存影片”“处理后删除影片”或明确要求调整单次下载上限时，宿主调用 `skill.media.configure` 覆写现有 `RetainDownloadedVideo` 或 `MaxBytes`，而不是直接编辑 YAML；文件保留时写入 `Skill.Media.Dir`，视频结果的 `platform_metadata.retained_media_relative_path` 则提供相对此目录的定位路径，绝不返回本机绝对路径。通用 Skill 媒体获取会在该目录内为每次执行创建私有子目录；外部 call ID 只用于请求关联，不能命名或覆盖本地媒体、固定下载文件或 manifest。Collect 的文件导出保持独立，不是这些 core 的输入前提。用户同意调用 `skill.asr.enable` 时，DigCrawl 会先检查 ffmpeg 与当前 ASR provider；缺失依赖时不会写入 opt-in 状态，宿主应保留错误并提示用户完成本地配置。视频摘要始终由宿主基于返回的 Markdown 和成功的 `asr_text` 完成；默认 profile 仅保留 `Skill.Media` 与 `Skill.ASR`，不包含 OCR、Summary 或模型 API Key，也不使用 DigFlow。`Gateway.AllowedDomains` 留空时允许任意 `http/https` 目标；配置后作为可选的访问收紧策略。用户明确请求截图时，返回当前视口图片；`Gateway.ArtifactMaxInlineBytes` 控制 MCP 附件上限，默认 `10485760`（10 MiB）。Cookie、artifact、下载媒体和登录态始终保留在用户本机。遇到小红书、抖音、头条、Threads 或 Instagram 的 `LOGIN_REQUIRED` 时，先经用户同意再运行 `digcrawl browser login --platform <platform>`；它会打开 DigCrawl 独立的有头登录窗口，不读取日常浏览器 profile，完成登录后在终端按 Enter 保存 Cookie。

## 4. 常用命令

构建：

```bash
go build -o digcrawl ./cmd/digcrawl
go build -o digcollect ./cmd/digcollect
```

采集单个链接：

```bash
./digcrawl collect "https://mp.weixin.qq.com/s/..." --format md,json
./digcrawl collect "https://www.toutiao.com/article/..." --format md,json
./digcrawl collect "https://www.toutiao.com/video/..." --format md,json
./digcrawl collect "https://www.bilibili.com/video/BV..." --format md,json
./digcrawl collect "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --format md,json
./digcrawl collect "https://m.weibo.cn/detail/..." --format md,json
```

批量采集：

```bash
./digcrawl collect --file urls.txt --format md,json --out ./collect-output
```

Watchlist 单轮运行：

```bash
./digcrawl watchlist run --once --config config/config.yaml
```

Watchlist 常驻服务：

```bash
./digcrawl watchlist serve --config config/config.yaml
```

安装 macOS/Linux 用户级常驻服务：

```bash
DIGCRAWL_CONFIG="$PWD/config/config.yaml" \
./scripts/digcrawl-watchlist-service.sh install

./scripts/digcrawl-watchlist-service.sh start
./scripts/digcrawl-watchlist-service.sh status
./scripts/digcrawl-watchlist-service.sh logs
```

## 5. Watchlist 清单维护

推荐把长清单放在数据库中，YAML 只保留调度、通知、持久化和 provider 配置。

```yaml
Watchlist:
  Source:
    Mode: database   # yaml | database | mixed
  Persistence:
    Driver: mysql    # sqlite | mysql
    GormConfigPath: config/gorm.yml
```

常用 source kind：

```text
rss, sitemap, web_list,
toutiao_author, toutiao_article, toutiao_video,
x_user, x_post,
reddit_subreddit, reddit_user, reddit_post_comments,
threads_user, threads_post,
instagram_user, instagram_post, instagram_reel,
douyin_user, douyin_video,
xiaohongshu_user, xiaohongshu_note,
wechat_list, wechat_article,
bilibili_user, bilibili_video,
youtube_channel, youtube_video,
weibo_user, weibo_post,
kuaishou_user, kuaishou_video,
zhihu_user, zhihu_answer, zhihu_article, zhihu_zvideo
```

CLI 维护示例：

```bash
./digcrawl watchlist source add --config config/config.yaml \
  --id bilibili-up --name "B站 UP 主" \
  --platform bilibili --kind bilibili_user \
  --url https://space.bilibili.com/123456 \
  --every 6h --notify feishu-default \
  --options '{"limit":20}'

./digcrawl watchlist source list --config config/config.yaml
./digcrawl watchlist source test-discover --config config/config.yaml --id bilibili-up
./digcrawl watchlist source disable --config config/config.yaml --id bilibili-up
./digcrawl watchlist source enable --config config/config.yaml --id bilibili-up
./digcrawl watchlist source delete --config config/config.yaml --id bilibili-up
```

HTTP 运维接口需要配置非空 `Gateway.AuthToken`，并携带 Bearer token：

```bash
curl -H "Authorization: Bearer $DIGCRAWL_GATEWAY_TOKEN" \
  http://127.0.0.1:8787/admin/watchlist/sources
```

## 6. 定时与通知

`watchlist serve` 会按 `Watchlist.Scheduler.TickInterval` 扫描到期任务。默认串行执行；如需显式启用并行，可设置 `Watchlist.Scheduler.MaxConcurrentRuns`（来源任务并发数）与 `Watchlist.Collect.MaxConcurrentItems`（单个来源内的条目并发数）。两项未设置或设置为 `1` 时均保持串行。并发采集仍会保留唯一输出路径、串行更新 index.json，并复用 Collect 的按平台限流。任务可以使用 interval 或 daily：

```yaml
Tasks:
  - ID: toutiao-daily
    Type: watchlist.collect
    Schedule:
      Mode: daily
      At: "09:00"
```

需要没有新增内容时也通知，可在 source 或 task options 中配置：

```yaml
Options:
  notify_no_update: true
```

飞书通知只应把真实 app secret、chat id 放在本地配置文件中；仓库中只保留 `.example.yml` 模板。

## 7. 摘要、OCR 与 ASR

摘要链路用于把采集结果转成适合推送或归档的短摘要。当前支持：

- `extractive`：本地规则摘要，不依赖外部模型。
- OpenAI-compatible API：适合直接调用外部 LLM。
- `digflow_runtime`：DigCrawl 作为宿主内嵌 DigFlow Runtime，通过 YAML 编排 summary agent。

视频内容建议走：

```text
video provider -> media download -> ffmpeg extract audio -> ASR -> summary agent -> notify
```

不要直接把 `asr_text` 全文推送给用户；应把 ASR 文本交给 summary agent，再输出标题、要点和判断。

本地 ASR 依赖：

```bash
ffmpeg
faster-whisper
```

示例：

```bash
./digcrawl asr transcribe ./video.mp4 --provider local --model small
```

## 8. 本机浏览器与登录态

浏览器能力由 `LocalBrowser` 配置控制：

```yaml
LocalBrowser:
  Enabled: true
  ChromePath: "/path/to/chrome-headless-shell"
  Headless: true
  CookieStoreDir: "storage/digcrawl/cookies"
  AllowedDomains:
    - "www.toutiao.com"
    - "www.threads.net"
    - "www.threads.com"
    - "www.instagram.com"
    - "m.weibo.cn"
    - "www.bilibili.com"
```

登录态处理建议：

- 需要登录的平台，先用有头浏览器完成登录。
- Cookie 写入本地 cookie store 后，后续 headless 抓取复用。
- Threads/Instagram 默认不读取 Cookie；如确需登录态，参考 `config/social.example.yml` 做全局和 source 双重 opt-in。
- 不把账号、密码、app secret、API key 写入 Git。

## 9. 数据库与 DDL

MySQL 全量初始化：

```bash
sqls/full/digcrawl_full_sql_ddl.sql
```

SQLite 全量初始化：

```bash
sqls/full/digcrawl_full_sqlite_ddl.sql
```

常见增量：

```text
sqls/2026-07-16_通用Task调度RuntimeDDL.sql
sqls/2026-07-18_持久化Watchlist清单DDL.sql
sqls/2026-07-29_Watchlist视频摘要投递元数据DDL.sql
```

DigCrawl 自身表使用 `dc_` 前缀，DigFlow runtime 表保持 `df_` 前缀。

## 10. 运行建议

- 单次归档用 `collect`。
- 长期监控用 `watchlist serve` + 数据库清单。
- 头条、B站、微博等列表源优先配置作者主页或 tab 列表页，不要只配置已经发布的详情页。
- 视频摘要链路要启用 ASR 和 summary agent。
- 生产级验证优先跑 `go test ./...`，再用真实来源做 `watchlist source test-discover` 和 `watchlist run --once`。
- 平台 API 或浏览器被风控时，应返回结构化错误或 fallback，不应静默推送“无更新”。
