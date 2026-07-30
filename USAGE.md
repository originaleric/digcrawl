# DigCrawl 使用指南

DigCrawl 是面向任意 Agent 编排系统的**本地工具网关**：在用户机器上安全暴露浏览器、本地文件、私有 API 等能力，供云端宿主或 IDE 通过 HTTP、WebSocket、MCP、stdio 调用。

本文档覆盖安装、配置、全部 CLI 命令、Collect 采集、宿主接入与扩展开发。**快速概览见 [README.md](README.md)。**

---

## 1. 安装与构建

### 环境要求

- Go 1.24+（以 `go.mod` 为准）
- 使用浏览器类工具时需本机 Chrome/Chromium（go-rod 可自动查找，也可在配置中指定 `ChromePath`）。在 macOS sandbox 或 CI-like headless 环境中，建议使用 Playwright `chrome-headless-shell` 并显式配置启动参数。

### 本机 Headless Chromium（推荐兜底）

当系统 Chrome、Chrome for Testing 或 Rod 自动下载的 Chromium 无法 headless 启动时，可安装 Playwright 的 `chrome-headless-shell`：

```bash
mkdir -p /Users/dig/projects/.cache/digcrawl-playwright
cd /Users/dig/projects/.cache/digcrawl-playwright

npm_config_cache=/private/tmp/npm-cache \
PLAYWRIGHT_BROWSERS_PATH=/Users/dig/projects/.cache/ms-playwright \
npx -y playwright install chromium

find /Users/dig/projects/.cache/ms-playwright -type f -name 'chrome-headless-shell'
```

将找到的 `chrome-headless-shell` 路径写入 `LocalBrowser.ChromePath`。在 macOS sandbox 下通常还需要 `--single-process` / `--no-zygote` 以及禁用 crashpad：

```yaml
LocalBrowser:
  ChromePath: "/Users/dig/projects/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-mac-arm64/chrome-headless-shell"
  Headless: true
  UserDataDir: "/private/tmp/digcrawl-pw-headless-profile"
  ExtraArgs:
    - "--single-process"
    - "--no-zygote"
    - "--disable-dev-shm-usage"
    - "--disable-crash-reporter"
    - "--disable-crashpad"
    - "--disable-breakpad"
    - "--disable-features=Crashpad"
```

### 从源码运行

```bash
git clone https://github.com/originaleric/digcrawl.git
cd digcrawl
go build -o digcrawl ./cmd/digcrawl
go build -o digcollect ./cmd/digcollect
```

### 安装到 PATH（可选）

```bash
go install ./cmd/digcrawl
go install ./cmd/digcollect
# 二进制通常在 $GOPATH/bin 或 $HOME/go/bin
```

### 配置文件位置

| 场景 | 默认路径 |
|------|----------|
| `gateway` / `collector` / `mcp` / `collect` 等 | 命令行 `--config`，未指定时为 `config/config.yaml` |
| `connect` / `login` | `~/.digcrawl/config.yaml`（可用 `--config` 覆盖） |
| 本地 GORM 数据库连接 | `config/gorm.yml`，由 `config/gorm.example.yml` 复制生成；包含密码，默认不入 Git |
| DigFlow 内嵌 Runtime 配置 | `config/digflow.yml`、`config/digflow-gorm.yml`、`config/eino.yml`、`agents/watchlist/agents.yml` |

首次 `connect` 或 `login` 会自动创建 `~/.digcrawl/` 目录并写入配置。

---

## 2. 配置文件说明

参考仓库内 [`config/config.yaml`](config/config.yaml)。主要段落如下。

### Gateway（HTTP 网关）

| 字段 | 说明 |
|------|------|
| `ListenAddr` | 监听地址，默认 `127.0.0.1:8787` |
| `InstanceID` | 实例标识，出现在 manifest 中 |
| `AuthToken` | 非空时要求请求携带 Bearer / `X-DigCrawl-Token` |
| `AllowedTools` | 工具名白名单；空表示不限制 |
| `AllowedReadPaths` | `file.read` 允许读取的本地路径前缀 |
| `AllowedDomains` | 与策略合并的 URL 域名白名单 |
| `ArtifactDir` / `ArtifactTTLMinutes` | 大对象（截图等）落盘目录与 TTL |
| `RequireApproval` | 是否强制要求高风险工具在 `ToolCall` 中携带 `approved: true` |

### Collector（反向连接宿主）

| 字段 | 说明 |
|------|------|
| `ServerURL` | 宿主根地址，如 `https://api.example.com` |
| `WSPath` | WebSocket 路径，默认 `/digcrawl/v1/collector/ws` |
| `Token` | 与宿主约定的鉴权令牌 |
| `InstanceID` | 本机 Collector 实例 ID（空则自动生成） |
| `PullIntervalSec` | `>0` 时由 Collector 主动拉取任务；`0` 为推送模式 |
| `MaxConcurrentCalls` | 并发执行工具数上限 |
| `RateLimitCooldownMs` | 相同 `RateLimitKey` 工具调用完成后的冷却时间 |
| `CallQueueSize` | Collector 会话内有界任务队列长度，队列满时返回 `RATE_LIMITED` |
| `CallQueueWaitMs` | 保留字段，兼容旧配置；当前 worker queue 入队不阻塞 read loop |
| `RetryMaxAttempts` / `RetryDelayMs` | 对 transient 工具失败做有限重试 |

### Collect（本地采集）

| 字段 | 说明 |
|------|------|
| `OutputDir` | 导出根目录，默认 `collect-output` |
| `InboxDir` | 收件箱目录，默认 `collect-inbox` |
| `DefaultFormats` | 默认导出格式：`md`、`html`、`json` |
| `DownloadMedia` | 是否下载正文中的媒体到 `.assets/` |
| `PersistMode` | 持久化方案：`files`（仅文件）/ `index`（默认，+ index.json）/ `sqlite`（+ collect.db）/ `both` |
| `DBPath` | SQLite 库路径，默认 `storage/digcrawl/collect.db` |
| `IndexEnabled` | `index`/`both` 模式下是否写 `index.json`（同 URL 覆盖更新） |
| `RateLimitMs` | inbox 批处理时 URL 之间的间隔（毫秒） |
| `PlatformRateLimits` | 按平台覆盖批处理间隔，如 `xiaohongshu`、`douyin`、`toutiao`、`reddit`、`bilibili`、`weibo`、`kuaishou`、`zhihu` |
| `InboxPollIntervalSec` | `inbox --watch` 轮询间隔 |

### LocalBrowser（本地浏览器）

| 字段 | 说明 |
|------|------|
| `Enabled` | 为 `false` 时禁用浏览器及依赖它的 Provider |
| `AllowedDomains` | 允许访问的域名列表（Collect / 平台工具均受约束） |
| `ChromePath` | 指定 Chrome/Chromium/headless shell 可执行文件路径；为空时由 go-rod 自动查找或下载 |
| `Headless` | 是否无头模式 |
| `CookieStoreDir` | 按域名持久化 Cookie，便于登录态采集 |
| `CDPMode` / `CDPPort` | 启动浏览器时开启 CDP 调试端口 |
| `CDPConnectExisting` | 连接已有 CDP 浏览器（如手动启动的 Chrome） |
| `UserDataDir` | 持久化浏览器 profile 根目录 |
| `ProxyURL` | 浏览器代理地址，页面 fetch/API fallback 共用 |
| `ExtraArgs` | 额外 Chromium 启动参数；适合配置 `--single-process`、`--no-zygote`、禁用 crashpad 等 headless 兼容参数 |

### Platform（平台读取）

| 字段 | 说明 |
|------|------|
| `BrowseRetryMaxAttempts` | Browser 兜底读取 transient 错误最大尝试次数 |
| `BrowseRetryDelayMs` | Browser 重试间隔 |
| `ReadMetricsEnabled` | 是否输出 `[platform-read]` 结构化日志 |
| `HealthProbes` | `digcrawl browser check --trial` 的平台默认试读 URL |

微信公众号公开文章默认优先使用 HTTP HTML 静态解析，成功时会在 `platform_metadata` 中标记 `read_channel=http`、`extraction_source=html`。只有网络 transient 或解析状态不确定时才进入 Browser 兜底；验证码、频控、删除、私密、登录要求和最终 URL 跳出微信域名会直接返回结构化错误。

X 单帖读取默认先尝试官方 Tweet lookup API；当 API 读权限或套餐不足时，会尝试公开 oEmbed 端点，成功时 `platform_metadata.read_channel=api`、`platform_metadata.extraction_source=oembed`。Browser 仅作为最后兜底，登录墙、删除、不可见和空壳页会返回结构化错误。

Reddit 读取默认先走官方 OAuth API，支持 `reddit.post.read`、`reddit.subreddit.read`、`reddit.user.read`。单帖工具会读取 selftext、外链、score、upvote ratio、评论数、评论树和 `more` 评论占位；评论读取受 `comment_limit`、`comment_depth`、`comment_sort` 控制。API 不可用时会先尝试 Reddit `.rss` 公开 feed，RSS 不可用时才按配置进入浏览器兜底。浏览器兜底只用于公开页面，401/403/429、私有社区、登录墙和受限内容不会通过浏览器绕过。

RSS / Atom 由通用 Feed service 提供，不再是 Watchlist 私有解析逻辑。`feed.read` 可直接读取 `.rss`、`.atom`、`.xml`、`/feed`、`/rss` URL，并输出 Markdown/JSON 条目列表；Watchlist 的 `Platform: rss`、Collect 的 RSS URL、Reddit RSS fallback 都复用同一实现。

Reddit 配置字段位于 `Platform.Reddit`：

| 字段 | 说明 |
|------|------|
| `APIEnabled` | 是否启用 Reddit API 读取，默认 `true` |
| `ClientID` / `ClientSecret` | Reddit app 凭证，支持 `${ENV}` |
| `RefreshToken` | refresh token 模式使用，支持 `${ENV}` |
| `UserAgent` | Reddit API 要求的明确 User-Agent |
| `OAuthMode` | `client_credentials` 或 `refresh_token` |
| `APIBaseURL` | 默认 `https://oauth.reddit.com` |
| `TokenURL` | 默认 `https://www.reddit.com/api/v1/access_token` |
| `RateLimit.RequestsPerMinute` | 本地保底限速 |
| `Comments.DefaultLimit` / `DefaultDepth` / `DefaultSort` | 单帖评论默认预算 |
| `BrowserFallback.Enabled` | 是否允许 API 失败后兜底浏览器 |
| `BrowserFallback.AllowWithoutOAuth` | 未配置 OAuth 时是否允许读取公开页面，默认 `false` |

配置示例：

```yaml
Platform:
  Reddit:
    APIEnabled: true
    ClientID: "${REDDIT_CLIENT_ID}"
    ClientSecret: "${REDDIT_CLIENT_SECRET}"
    OAuthMode: "client_credentials"
    UserAgent: "digcrawl/1.0 by your-reddit-username"
    Comments:
      DefaultLimit: 50
      DefaultDepth: 3
      DefaultSort: "confidence"
    BrowserFallback:
      Enabled: true
      AllowWithoutOAuth: false
```

### OCR（图片文字提取）

| 字段 | 说明 |
|------|------|
| `Enabled` | 是否启用 OCR，默认 `false` |
| `Provider` | OCR provider，目前支持 `openai_compatible`、`zhipu_ocr`、`stub`；`glm_ocr` / `glm-ocr` 会归一为 `zhipu_ocr` |
| `APIKey` | OCR provider API key |
| `BaseURL` | provider API base URL，`openai_compatible` 默认 `https://api.openai.com/v1`，`zhipu_ocr` 默认 `https://open.bigmodel.cn/api/paas/v4` |
| `Model` | OCR 使用的视觉模型；`zhipu_ocr` 默认 `glm-ocr` |
| `Prompt` | OCR 提示词 |
| `TimeoutSec` | OCR 单次请求超时 |
| `MaxImages` | 每条内容最多处理图片数 |
| `MaxImageBytes` | 本地图片转 data URL 前的大小上限 |
| `MaxTokens` | OCR 返回 token 上限 |
| `AppendToMarkdown` | 是否把 OCR 文本追加到 Markdown，默认 `true` |
| `AppendToText` | 是否把 OCR 文本追加到纯文本，默认 `false` |

OCR 启用后会在平台工具、通用浏览器读取和 Collect 导出中统一生效。结果写入 `platform_metadata.ocr_results`，并记录 `ocr_count`、provider、model、图片来源和错误诊断。直接平台/浏览器调用会把截图作为显式 OCR 候选；Collect 下载媒体时会优先对本地 `.assets/` 图片做 OCR，未下载媒体时会使用远程图片 URL。`zhipu_ocr` 对远程图片 URL 使用智谱 `layout_parsing` / `glm-ocr`，对本地图片和截图使用智谱 `files/ocr` 上传接口。

智谱 GLM-OCR 配置示例：

```yaml
OCR:
  Enabled: true
  Provider: zhipu_ocr
  APIKey: "<zhipu-api-key>"
  BaseURL: "https://open.bigmodel.cn/api/paas/v4"
  Model: "glm-ocr"
  TimeoutSec: 60
  MaxImages: 8
  MaxImageBytes: 8388608
  AppendToMarkdown: true
```

### ASR（视频语音转写）

| 字段 | 说明 |
|------|------|
| `Enabled` | 是否在 Collect 下载到 video/audio 后自动转写，默认 `false`；CLI 和工具调用仍可通过显式请求单独运行 |
| `Provider` | ASR provider，支持 `api`、`local`、`stub` |
| `APIKey` | API provider key |
| `BaseURL` | OpenAI-compatible API base URL，默认 `https://api.openai.com/v1` |
| `Model` | 留空时按 provider 选择默认值：`api=whisper-1`，`local=small` |
| `Language` | 可选语言代码 |
| `Prompt` | 可选转写提示词，API provider 会透传 |
| `ResponseFormat` | API provider response format，支持 `verbose_json`、`json`、`text`、`srt`、`vtt`，默认 `verbose_json` |
| `TimeoutSec` | ffmpeg + ASR 总超时 |
| `FFmpegPath` | ffmpeg 可执行文件路径，默认 `ffmpeg` |
| `TempDir` | 抽取音频的临时目录，空则使用系统临时目录 |
| `AudioSampleRate` | ffmpeg 抽取 wav 的采样率，默认 `16000` |
| `MaxAudioBytes` | API provider 上传音频大小上限 |
| `LocalCommand` | 本地 ASR 命令，默认 `faster-whisper`；命令不存在且未配置 `LocalArgs` 时，会尝试 fallback 到 `python3 scripts/faster_whisper_asr.py` |
| `LocalArgs` | 本地 ASR 参数模板，支持 `{audio_path}`、`{model}`、`{language}`、`{output_dir}` |

配置示例：

```yaml
ASR:
  Provider: api
  APIKey: "<api-key>"
  BaseURL: "https://api.openai.com/v1"
  Model: ""
  ResponseFormat: "verbose_json"
  TimeoutSec: 300
  FFmpegPath: "ffmpeg"
```

本地 `faster-whisper` 示例：

```yaml
ASR:
  Enabled: true
  Provider: local
  Model: small
  Language: zh
  FFmpegPath: "ffmpeg"
  LocalCommand: "/path/to/faster-whisper-venv/bin/python"
  LocalArgs:
    - "scripts/faster_whisper_asr.py"
    - "{audio_path}"
    - "--model"
    - "{model}"
    - "--language"
    - "{language}"
    - "--output_format"
    - "json"
    - "--output_dir"
    - "{output_dir}"
```

### Watchlist / Summary / Notifications

Watchlist 用于按配置定期检查指定来源的新增内容，复用 Collect 抓详情，再通过 Summary provider 生成摘要并发送通知。

`Watchlist` 字段：

| 字段 | 说明 |
|------|------|
| `Enabled` | 是否启用 watchlist，默认 `false` |
| `DryRun` | 是否允许不配置真实通知通道时仅记录运行，默认 `false` |
| `DefaultInterval` | 常驻模式默认检查间隔，默认 `6h` |
| `Scheduler` | `watchlist serve` 使用的内置调度器配置 |
| `NotifyOnFirstRun` | 首次运行是否通知历史内容，默认 `false`，即只建基线 |
| `MaxItemsPerRun` | 单个 watch item 每轮最多处理的新内容数 |
| `LockTTL` | per-watch 锁过期时间，避免 cron / 常驻进程重入 |
| `Persistence.Driver` | watchlist 状态持久化 driver，当前支持 `sqlite`，默认 `sqlite` |
| `Persistence.SQLitePath` | watchlist 状态库路径，默认复用 `Collect.DBPath` |
| `DBPath` | 旧版兼容字段；新配置建议使用 `Persistence.SQLitePath` |
| `Items` | 监控目标列表 |

`Watchlist.Scheduler` 字段：

| 字段 | 说明 |
|------|------|
| `Enabled` | 是否启用内置调度器，默认 `true` |
| `TickInterval` | 到期扫描频率，默认 `5m` |
| `Jitter` | 每轮执行前随机抖动，避免多实例同时触发 |

`Tasks` 是新的通用定时任务描述，推荐新配置优先使用 `Tasks`，旧 `Watchlist.Items` 继续兼容。当前已落地的任务类型：

| Type | 说明 |
|------|------|
| `watchlist.collect` | 通过 `internal/source` 拉取最新内容，复用 Collect、Summary 和 Notifications |
| `feed.digest` | `watchlist.collect` 的兼容别名，适合 RSS/Feed 摘要场景 |

`Tasks[].Schedule.Mode` 当前支持 `interval` 和 `daily`。`interval` 使用 `Every` 的 Go duration，例如 `6h`、`24h`、`30m`；`daily` 使用本机时区的 `At: "HH:MM"`，例如每天上午 9 点写 `At: "09:00"`。调度状态写入通用 `dc_tasks_runtime` / `dc_task_runs` / `dc_task_locks` / `dc_task_events`：任务完成后会按当前 schedule 更新 `next_run_at` 并判断是否到期。

Watchlist 的来源发现由 `internal/source.Service` 统一承载，当前覆盖 RSS/Atom、sitemap、HTML 链接发现、今日头条固定文章/作者页/视频列表页、X/Twitter 单帖/用户页链接发现、抖音作品/用户页链接发现、小红书笔记/用户页链接发现、微信公众号文章/列表页链接发现、Reddit listing/comment 来源，以及 B站、微博、快手、知乎的主页/列表页链接发现。Watchlist 只负责调度、去重、详情采集、摘要和通知，因此后续新增渠道应优先接入 `internal/source`，再由 Watchlist、Tasks 或其他 domain 复用。

推荐维护来源页或列表页 source，例如 `rss`、`sitemap`、`reddit_subreddit`、`reddit_user`、`toutiao_author`、`x_user`、`douyin_user`、`xiaohongshu_user`、`wechat_list`、`bilibili_user`、`weibo_user`、`kuaishou_user`、`zhihu_user`、`web_list`。`toutiao_article`、`toutiao_video`、`x_post`、`douyin_video`、`xiaohongshu_note`、`wechat_article`、`reddit_post_comments`、`bilibili_video`、`weibo_post`、`kuaishou_video`、`zhihu_answer`、`zhihu_article`、`zhihu_zvideo` 属于详情页或评论变更监控，适合单次归档、评论更新或互动状态检查，不适合作为“新增 po 文”主来源。

上线前可以先做 discover-only 验证，只检查来源页能否发现内容，不写入 `dc_watchlist_items`，也不会触发 collect、summary 或 notify：

```bash
digcrawl watchlist source test-discover --config config/config.yaml --id reddit-ai

curl -X POST -H "Authorization: Bearer $DIGCRAWL_GATEWAY_TOKEN" \
  http://127.0.0.1:8787/admin/watchlist/sources/reddit-ai/discover
```

`Watchlist.Items[].Options` 可为不同平台提供扩展参数。Reddit 已支持：

- `reddit_subreddit`：`subreddit`、`sort`、`time_range`、`limit`、`after`
- `reddit_user`：`username`、`where`、`limit`、`after`
- `reddit_post_comments`：`comment_limit`、`comment_depth`、`comment_sort`、`expand_more`、`expand_more_limit`

今日头条已支持：

- `article` / `group` / `toutiao_article`：固定头条文章 URL 监控，例如 `https://www.toutiao.com/w/.../`、`https://www.toutiao.com/group/.../` 或 `https://www.toutiao.com/article/.../`。该模式复用已验证的 `toutiao.article.read` 抓取链路。
- `video` / `toutiao_video`：固定头条视频 URL 监控，例如 `https://www.toutiao.com/video/.../`。该模式复用 `toutiao.video.read`，可在 `ASR.Enabled=true` 时下载视频后写入 `asr_text`。
- `toutiao_author` / `author` / `user`：作者页或视频列表页链接发现；静态 `href`、hydration 中的转义 URL 和相对内容路径都会尝试解析，发现结果使用 `toutiao:<article_id>` 或 `toutiao:video:<video_id>` 去重。头条作者页若完全依赖动态 API/登录态，仍可能需要后续专用 API 或 headless 渲染发现。

X / Twitter 已支持：

- `x_post`：固定单帖 URL 监控，例如 `https://x.com/elonmusk/status/...`。该模式复用已验证的 `x.post.read` 抓取链路。
- `x_user`：用户主页静态 status 链接发现，发现结果使用 `x:<status_id>` 去重；X 主页内容通常依赖前端动态加载，未登录/未渲染 HTML 可能发现不到最新帖子，生产使用建议后续接入 X API/浏览器渲染发现。

抖音已支持：

- `douyin_video` / `video`：固定抖音作品 URL 监控，例如 `https://www.douyin.com/video/...`、`https://www.iesdouyin.com/share/video/...` 或带 `modal_id` 的用户页链接，发现结果使用 `douyin:<work_id>` 去重。
- `douyin_user` / `user`：用户页或列表页作品链接发现，静态链接、hydration URL 和 browser fallback metadata 都会参与候选提取；可配合 `wait_selector`、`include_pattern`、`limit` 收窄范围。

小红书已支持：

- `xiaohongshu_note` / `xhs_note`：固定笔记 URL 监控，例如 `https://www.xiaohongshu.com/explore/...` 或 `/discovery/item/...`，发现结果使用 `xiaohongshu:<note_id>` 去重。
- `xiaohongshu_user` / `xhs_user`：用户页或列表页笔记链接发现，发现结果会规范化为 `/explore/<note_id>` 并使用稳定 note id 去重；登录态平台建议先执行 `digcrawl browser login --platform xiaohongshu`。

B站已支持：

- `bilibili_video` / `bili_video`：固定视频 URL 监控，例如 `https://www.bilibili.com/video/BV...`、`/video/av...` 或 `b23.tv` 短链。该模式复用 `bilibili.video.read`。
- `bilibili_user` / `bili_user`：空间页或视频列表页链接发现，当前从静态链接、hydration URL 和 browser fallback metadata 中抽取 `/video/BV...`、`/video/av...` 内容链接。

微博已支持：

- `weibo_post`：固定微博详情 URL 监控，例如 `https://m.weibo.cn/detail/...`、`/status/...` 或 `https://weibo.com/{uid}/{mid}`。该模式复用 `weibo.post.read`。
- `weibo_user`：用户页或列表页链接发现，只抽取微博详情链接，不会把 `/u/...`、`/profile/...` 用户页当作 po 文。

快手已支持：

- `kuaishou_video` / `ks_video`：固定视频 URL 监控，例如 `https://www.kuaishou.com/short-video/...`、`/photo/...` 或 `v.kuaishou.com` 短链。该模式复用 `kuaishou.video.read`。
- `kuaishou_user` / `ks_user`：主页或列表页链接发现，抽取 `/short-video/...` 和 `/photo/...` 内容链接。

知乎已支持：

- `zhihu_answer`：固定回答 URL 监控，例如 `https://www.zhihu.com/question/.../answer/...`。
- `zhihu_article`：固定专栏文章 URL 监控，例如 `https://zhuanlan.zhihu.com/p/...`。
- `zhihu_zvideo`：固定知乎视频 URL 监控，例如 `https://www.zhihu.com/zvideo/...`。
- `zhihu_user`：用户页或列表页链接发现，抽取回答、专栏文章和知乎视频链接。

微信公众号已支持：

- `wechat_article`：固定公众号文章 URL 监控，例如 `https://mp.weixin.qq.com/s/...`。
- `wechat_list`：公众号文章列表页或普通页面中的 `mp.weixin.qq.com/s...` 链接发现，发现结果使用规范化文章 URL hash 去重；列表页若需要登录或动态加载，可使用 browser fallback 和 `wait_selector`。

普通网页已支持：

- `web_list`：从列表页或首页抽取同域链接，默认用规范化 URL 作为 `content_key`，并去除 `utm_*`、`spm` 等常见追踪参数；可用 `include_pattern`、`exclude_pattern`、`limit`、`wait_selector`、`content_selector` 控制 URL 范围、数量和 browser fallback 提取时机。

`Summary` 字段：

| 字段 | 说明 |
|------|------|
| `Enabled` | 是否启用摘要；关闭时使用规则摘要兜底 |
| `Provider` | `extractive`、`llm_direct`、`digflow_runtime`、`digflow_http` |
| `TimeoutSec` | 摘要请求超时 |
| `MaxInputChars` | 送入摘要 provider 的总输入上限 |
| `FallbackProvider` | 摘要失败时的兜底 provider，默认 `extractive` |
| `InputBudget` | 正文、ASR、OCR、metadata 的分桶预算 |
| `DigFlow` | DigFlow 内嵌 Runtime 或 HTTP 兼容接入配置 |
| `LLM` | OpenAI-compatible chat completion 直连接入配置 |

`Notifications.Feishu` 字段：

| 字段 | 说明 |
|------|------|
| `Enabled` | 是否启用飞书 webhook 通知 |
| `DefaultChannel` | 默认通知通道 ID |
| `Channels` | webhook 通道列表 |
| `WebhookURL` | 飞书机器人 webhook，支持 `${ENV}` |
| `Secret` | 飞书签名 secret，支持 `${ENV}` |
| `MaxMessageChars` | 单条通知文本长度上限，超出会截断 |

配置示例：

```yaml
Watchlist:
  Enabled: true
  DryRun: false
  DefaultInterval: "6h"
  Scheduler:
    Enabled: true
    TickInterval: "5m"
    Jitter: "30s"
  NotifyOnFirstRun: false
  MaxItemsPerRun: 20
  LockTTL: "30m"
  Persistence:
    Driver: sqlite
    SQLitePath: "storage/digcrawl/collect.db"
  Items:
    - ID: rss-example
      Platform: rss
      Type: rss
      URL: https://example.com/feed.xml
      Name: Example Feed
      NotifyTo: feishu-default
      Enabled: true
    - ID: reddit-local-llama
      Platform: reddit
      Type: reddit_subreddit
      URL: https://www.reddit.com/r/LocalLLaMA/new/
      Name: r/LocalLLaMA
      Interval: 6h
      NotifyTo: feishu-default
      Enabled: true
      Options:
        sort: new
        limit: 25
    - ID: reddit-post-comments
      Platform: reddit
      Type: reddit_post_comments
      URL: https://www.reddit.com/r/golang/comments/abc123/title/
      Interval: 6h
      NotifyTo: feishu-default
      Enabled: true
      Options:
        comment_sort: new
        comment_limit: 1

Tasks:
  - ID: reddit-ai-watch
    Name: Reddit AI 社区监控
    Type: watchlist.collect
    Enabled: true
    Schedule:
      Mode: daily
      At: "09:00"
    Source:
      Platform: toutiao
      Kind: toutiao_author
      URL: https://www.toutiao.com/c/user/token/...
      Options:
        limit: 2
        wait_selector: "a[href]"
        notify_no_update: true
    Notify:
      Channel: feishu-default
  - ID: toutiao-article-watch
    Name: 头条指定文章监控
    Type: watchlist.collect
    Enabled: true
    Schedule:
      Mode: interval
      Every: 6h
    Source:
      Platform: toutiao
      Kind: article
      URL: https://www.toutiao.com/w/1870280855070795/
    Notify:
      Channel: feishu-default
  - ID: x-elon-post-watch
    Name: Elon Musk 指定 X 帖子监控
    Type: watchlist.collect
    Enabled: true
    Schedule:
      Mode: interval
      Every: 6h
    Source:
      Platform: x
      Kind: x_post
      URL: https://x.com/elonmusk/status/1234567890
    Notify:
      Channel: feishu-default

Summary:
  Enabled: true
  Provider: digflow_runtime
  FallbackProvider: extractive
  DigFlow:
    RuntimeMode: embedded
    RootDir: "."
    ConfigPath: config/digflow.yml
    GormConfigPath: config/digflow-gorm.yml
    EinoConfigPath: config/eino.yml
    ManifestPath: agents/watchlist/agents.yml
    GlobalToolsPath: config/global_tools.yml
    GlobalSkillsDir: skills
    DisableCheckpoint: true
    Agent: watchlist_summary_agent

Notifications:
  Feishu:
    Enabled: true
    DefaultChannel: feishu-default
    Channels:
      - ID: feishu-default
        WebhookURL: ${FEISHU_WEBHOOK_URL}
        Secret: ${FEISHU_WEBHOOK_SECRET}
        TimeoutSec: 10
        MaxMessageChars: 12000
```

DigFlow HTTP 兼容模式仍可用，适合已有独立 DigFlow 服务的部署：

```yaml
Summary:
  Enabled: true
  Provider: digflow_http
  DigFlow:
    RuntimeMode: http
    BaseURL: http://127.0.0.1:20201
    Endpoint: /api/v1/invoke
    Agent: watchlist_summary_agent
    AuthToken: ${DIGFLOW_API_TOKEN}
```

DigFlow 内嵌 Runtime 需要额外准备：

- `config/digflow-gorm.yml`：DigFlow 平台库连接，表前缀固定 `df_`。
- `config/eino.yml`：DigFlow / DigEino domain 表名、上下文、skills、chat history、execution state 配置。
- `agents/watchlist/agents.yml`：宿主侧 agent manifest。
- `agents/watchlist/summary-agent.yml`：watchlist 摘要 agent。
- `config/runtime-env.yml`：本地运行时变量注入，参考 `config/runtime-env.example.yml`，由 `config/config.yaml` 的 `RuntimeEnv.ConfigPath` 显式启用。

默认构建不编译私有 DigFlow runtime 依赖，`digflow_runtime` provider 会使用 stub 并回退到 extractive 摘要。DigFlow 已固定到 `v1.1.3`；需要真正嵌入 DigFlow runtime 时，先配置 SSH/GOPRIVATE，然后使用 `-tags digflow_embedded` 构建或测试。内嵌 runtime 首次构建会拉取 runtime 依赖树，建议显式设置可写 Go cache，避免本机权限或冷缓存导致长时间无输出。`config/digflow-gorm.yml` / `config/eino.yml` 中的 `${...}` 变量会由 DigCrawl 宿主侧在运行时展开到临时配置文件；缺失变量会直接报错，不会静默展开为空。

```bash
GOPRIVATE=github.com/originaleric/* \
GONOSUMDB=github.com/originaleric/* \
GOPATH=/private/tmp/digcrawl-gopath \
GOMODCACHE=/private/tmp/digcrawl-gomodcache \
GOCACHE=/private/tmp/digcrawl-go-cache \
TMPDIR=/private/tmp \
go test -tags digflow_embedded ./internal/summary
```

数据库 DDL 已对齐 DigFlow `v1.1.3`。该版本主要收口 `pkg/digflowapi` / `pkg/digflowtypes` 公共 DTO 轻量化，`pkg/digflow.NewRuntime`、`Invoke`、`Stream` 兼容既有宿主；digcrawl 当前内嵌路径仍直接使用 `pkg/digflow`，无需迁移 DTO import。本地单机运行推荐使用 SQLite，远端 MySQL 部署使用 MySQL DDL：

- 新库完整初始化 MySQL：`sqls/full/digcrawl_full_sql_ddl.sql`
- 新库完整初始化 SQLite：`sqls/full/digcrawl_full_sqlite_ddl.sql`，包含 Collect、Watchlist/Tasks runtime 和 DigFlow `df_*` embedded runtime 表
- 新库只初始化 DigFlow runtime 表：`sqls/full/digflow_embedded_runtime_full_ddl.sql`
- 根目录兼容全量：`sqls/digcrawl_full_sql_ddl.sql`、`sqls/2026-07-09_DigFlow内嵌Runtime初始化DDL.sql`
- 已执行旧版 DigFlow DDL 的库补跑：`sqls/2026-07-09_DigFlow最新Runtime增量DDL.sql`
- 已执行旧版 DigCrawl DDL 且要启用通用 Tasks 调度的库补跑：`sqls/2026-07-16_通用Task调度RuntimeDDL.sql`

DigCrawl 自身本地 SQLite driver 使用 `github.com/mattn/go-sqlite3`，需要 `CGO_ENABLED=1` 和本机 C 编译环境；macOS 可通过 Xcode Command Line Tools 提供 `cc`。DigFlow 自身 SQLite 配置仍由 DigFlow / GORM 相关配置控制。

最新增量包含：

- LoopRuntime 持久化表：`df_loop_instance`、`df_loop_iteration`、`df_loop_active`
- ResourceAsset 存储键迁移：将 `df_resource_asset.storage_key` 调整为 nullable，并创建 `uidx_resource_asset_storage_key`

如果 DigFlow embedded runtime 也要完全本地化，需要同时调整三个配置入口；只改 `config/digflow-gorm.yml` 不够：

```yaml
# config/digflow-gorm.yml
Gorm:
  UseDbType: "sqlite"
  Mysql:
    IsInitGolobalGormMysql: 0
  Sqlite:
    IsInitGolobalGormSqlite: 1
    Write:
      Path: "./storage/sqlite/digflow.db"

# config/digflow.yml
ChatHistoryStore:
  Enabled: false
  Type: "memory"
sql_lite:
  Path: "./storage/sqlite/digflow-memory.db"

# config/eino.yml
context:
  ChatHistory:
    Enabled: false
    Type: "memory"
    ReuseGlobalMySQL: false
  LongTerm:
    Enabled: false
    Store: "auto"
```

`config/digflow-gorm.yml` 控制 DigFlow 平台表连接；`config/digflow.yml` 的 `ChatHistoryStore` 目前常用持久化实现是 MySQL/PGSQL/sql_mapping，纯本地模式建议禁用或使用 memory；`config/eino.yml` 的 `context.ChatHistory` 也要关闭 `ReuseGlobalMySQL`，否则仍会尝试复用 MySQL。若后续启用长期记忆本地索引，再把 `context.LongTerm.Store` 调整为 `local_sqlite`，并确保 `config/digflow.yml` 的 `sql_lite.Path` 非空。DigCrawl 自身的 Watchlist runtime 通过 `Watchlist.Persistence.SQLitePath` 控制，二者可以共用一个 SQLite 文件，也可以按运行边界拆成两个文件。

表前缀约定：

- DigCrawl 业务表：`dc_*`，MySQL 与 SQLite 保持一致
- DigFlow runtime 表：`df_*`

### Providers（内置 Provider 开关）

| 字段 | 说明 |
|------|------|
| `PlatformEnabled` | 公众号 / 小红书 / 抖音 / X 平台工具 |
| `BrowserEnabled` | `browser.browse` / `snapshot` / `action` |
| `ASREnabled` | `asr.transcribe` 视频/音频转写工具 |
| `ExampleEnabled` | `gateway.ping`、`file.read` |
| `StubEnabled` | 示例第三方 Provider（开发用） |

---

## 3. 命令行总览

```text
digcrawl gateway [flags]       # HTTP 工具网关
digcrawl collector [flags]     # WebSocket 反向连接宿主
digcrawl connect [flags]       # 保存宿主配置并启动 Collector
digcrawl login [flags]         # 仅保存 Token / 宿主地址
digcrawl mcp [flags]           # MCP stdio（Cursor / Claude Desktop）
digcrawl stdio [flags]         # 逐行 JSON 协议
digcrawl dev-host [flags]      # 本地参考宿主（仅开发调试）
digcrawl collect <url>         # 采集单个 URL
digcrawl collect inbox         # 处理收件箱
digcrawl browser login         # 平台扫码/交互登录并保存 Cookie
digcrawl browser check         # 检查平台会话，可选试读
digcrawl asr transcribe        # ffmpeg 抽音频并转写
digcrawl watchlist run         # 执行 watchlist 监控、摘要和通知
digcrawl watchlist serve       # 常驻调度，到期执行不同周期的 Tasks / Watchlist items
scripts/digcrawl-watchlist-service.sh install|rebuild|rebuild-restart|start|stop|restart|status|logs|uninstall
digcollect <url>              # 轻量采集入口，等价于 digcrawl collect <url>
digcrawl help
```

全局习惯：多数子命令支持 `--config <path>` 指定配置文件。

### Watchlist 常驻服务脚本

`scripts/digcrawl-watchlist-service.sh` 用于把 `digcrawl watchlist serve` 托管为用户级服务，方便本机或小型服务器部署。

- macOS：生成 `~/Library/LaunchAgents/com.digcrawl.watchlist.plist` 并通过 `launchctl` 管理。
- Linux：生成 `~/.config/systemd/user/digcrawl-watchlist.service` 并通过 `systemctl --user` 管理。
- 日志：macOS 写入 `DIGCRAWL_LOG_DIR` 下的 `watchlist.out.log` / `watchlist.err.log`；Linux 默认进入 user journal，通过脚本的 `logs` 命令查看。

常用命令：

```bash
DIGCRAWL_CONFIG="$PWD/config/config.yaml" \
./scripts/digcrawl-watchlist-service.sh install

./scripts/digcrawl-watchlist-service.sh start
./scripts/digcrawl-watchlist-service.sh status
./scripts/digcrawl-watchlist-service.sh logs
./scripts/digcrawl-watchlist-service.sh rebuild
./scripts/digcrawl-watchlist-service.sh rebuild-restart
./scripts/digcrawl-watchlist-service.sh restart
./scripts/digcrawl-watchlist-service.sh stop
./scripts/digcrawl-watchlist-service.sh uninstall
```

可选环境变量：

| 变量 | 说明 |
|------|------|
| `DIGCRAWL_BIN` | `digcrawl` 二进制路径，默认优先使用仓库根目录的 `./digcrawl` |
| `DIGCRAWL_CONFIG` | 配置文件路径，默认 `config/config.yaml` |
| `DIGCRAWL_WORKDIR` | 服务工作目录，默认仓库根目录 |
| `DIGCRAWL_LOG_DIR` | 日志目录，默认 `logs/` |
| `DIGCRAWL_SERVICE_NAME` | Linux systemd service 名，默认 `digcrawl-watchlist` |
| `DIGCRAWL_SERVICE_LABEL` | macOS launchd label，默认 `com.digcrawl.watchlist` |
| `DIGCRAWL_AUTO_BUILD` | 是否由脚本自动构建 watchlist 二进制，默认 `1` |
| `DIGCRAWL_BUILD_TAGS` | 自动构建 tag，默认 `digflow_embedded` |
| `DIGCRAWL_BUILD_GOCACHE` | 自动构建 Go build cache，默认 `/private/tmp/digcrawl-gocache` |
| `DIGCRAWL_BUILD_GOMODCACHE` | 自动构建 Go module cache，默认 `.tmp/go-mod-watchlist` |

`install` 会构建默认 binary 并写入用户级服务配置；`run-foreground` 只运行已有 binary。更新代码或 DigFlow 依赖后，使用 `./scripts/digcrawl-watchlist-service.sh rebuild` 单独重建，或使用 `./scripts/digcrawl-watchlist-service.sh rebuild-restart` 重建后重启。脚本会优先读取已安装 launchd plist / systemd unit 里的 `DIGCRAWL_BIN`，重建到服务实际使用的 binary 路径。`build` 保留兼容，也会使用同样的路径解析逻辑。

使用 `digflow_runtime` summary agent 时，服务进程不再要求 launchd/systemd 注入业务密钥。推荐在主配置中设置 `RuntimeEnv.ConfigPath: config/runtime-env.yml`，并在本地 ignored 的 `config/runtime-env.yml` 中开启 `RuntimeEnv.Enabled: true`，填入 `DIGCRAWL_MYSQL_HOST`、`DIGCRAWL_MYSQL_DATABASE`、`DIGCRAWL_MYSQL_USER`、`DIGCRAWL_MYSQL_PASSWORD` 和 `DIGFLOW_SUMMARY_API_KEY`。仓库只提交 `config/runtime-env.example.yml`。

---

## 4. HTTP 网关（gateway）

在本机启动工具网关，默认只监听本机地址。若要暴露给局域网或公网，请显式设置 `AuthToken`、`AllowedTools`、`AllowedDomains` 并确认风险。

```bash
# 使用仓库示例配置
digcrawl gateway --config config/config.yaml

# 覆盖监听地址
digcrawl gateway --config config/config.yaml --addr 127.0.0.1:8787
```

验证：

```bash
curl -s http://127.0.0.1:8787/health
curl -s http://127.0.0.1:8787/manifest | jq .
```

若配置了 `AuthToken`：

```bash
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:8787/manifest
```

### 调用工具示例

```bash
curl -s -X POST http://127.0.0.1:8787/tools/call \
  -H "Content-Type: application/json" \
  -d '{
    "type": "tool_call",
    "id": "demo-1",
    "tool": "gateway.ping",
    "input": {}
	  }'
```

`browser.action`、`file.read` 等清单中标记 `requires_user_approval: true` 的工具默认需要在 `ToolCall` 里传入 `"approved": true`。否则 runtime 会返回 `USER_APPROVAL_REQUIRED`，用于避免宿主或 HTTP 客户端直接执行高风险交互。

---

## 5. 连接云端宿主（collector / connect / login）

典型部署：**用户本机运行 DigCrawl Collector**，通过 WebSocket **反向连接**到你的业务宿主；宿主下发 `tool_call`，在本机执行后回传结果。

### 方式 A：一步连接（推荐）

```bash
digcrawl connect --server https://your-host.example.com --token <collector-token>
```

- 将 `ServerURL`、`Token` 写入 `~/.digcrawl/config.yaml`
- 随即以该配置启动 `collector`

仅保存配置、不启动：

```bash
digcrawl connect --server https://your-host.example.com --token <token> --save-only
```

### 方式 B：分步配置

```bash
# 先保存令牌（可选同时写 server）
digcrawl login --token <collector-token>
digcrawl login --server https://your-host.example.com --token <token>

# 再启动 Collector
digcrawl collector --config ~/.digcrawl/config.yaml
```

### Collector 常用参数

```bash
digcrawl collector \
  --config ~/.digcrawl/config.yaml \
  --server https://your-host.example.com \
  --token <token> \
  --instance-id my-laptop \
  --pull-interval 5
```

| 参数 | 说明 |
|------|------|
| `--server` | 覆盖配置中的 `Collector.ServerURL` |
| `--token` | 覆盖 `Collector.Token` |
| `--instance-id` | 覆盖 `Collector.InstanceID` |
| `--pull-interval` | 拉取间隔（秒）；`-1` 表示使用配置文件 |

### 开发用参考宿主

生产环境请在**你自己的项目**中实现 WebSocket 宿主；仓库仅提供最小参考：

```bash
digcrawl dev-host --addr :8790 --token dev
```

再在另一终端：

```bash
digcrawl collector --server http://127.0.0.1:8790 --token dev
```

---

## 6. IDE 集成（MCP）

在 Cursor、Claude Desktop 等支持 MCP 的客户端中，将 DigCrawl 注册为 stdio 服务：

```json
{
  "mcpServers": {
    "digcrawl": {
      "command": "/path/to/digcrawl",
      "args": ["mcp", "--config", "/path/to/config/config.yaml"]
    }
  }
}
```

启动后，IDE 内的 Agent 可通过 MCP 调用本机已注册工具（与 `gateway` 共用同一套 runtime / Provider）。

---

## 7. stdio JSON 协议

适用于需要管道集成的宿主或脚本：

```bash
digcrawl stdio --config config/config.yaml
```

stdin/stdout 上逐行 JSON，消息类型与 [`protocol/`](protocol/) 包一致（如 manifest 查询、tool_call、tool_result）。适合与自定义编排器对接。

---

## 8. 本地内容采集（collect）

Collect 在**无云端宿主**的情况下，把链接抓取并导出到本地目录，适合知识库、归档、批处理链接。

### 支持的内容来源

| 类型 | 检测方式 | 底层工具 |
|------|----------|----------|
| 微信公众号 | `mp.weixin.qq.com` 等 | `wechat.article.read` |
| 小红书 | `xiaohongshu.com`、`xhslink.com` 等 | `xiaohongshu.note.read` |
| 抖音 | `douyin.com` 等 | `douyin.video.read` |
| 今日头条文章 | `toutiao.com`、`m.toutiao.com` 等 | `toutiao.article.read` |
| 今日头条视频 | `toutiao.com/video/...` | `toutiao.video.read` |
| X / Twitter | `x.com`、`twitter.com` 等 | `x.post.read` |
| B站 | `bilibili.com`、`b23.tv` 等 | `bilibili.video.read` |
| 微博 | `weibo.com`、`m.weibo.cn` 等 | `weibo.post.read` |
| 快手 | `kuaishou.com`、`v.kuaishou.com` 等 | `kuaishou.video.read` |
| 知乎 | `zhihu.com`、`zhuanlan.zhihu.com` 等 | `zhihu.content.read` |
| RSS / Atom | `.rss`、`.atom`、`.xml`、`/feed`、`/rss` | `feed.read` |
| 任意网页 | 其他 `http(s)` URL | `browser.browse`（通用网页） |

小红书、抖音、头条采用 **API 优先 + Browser 兜底** 的 strategy chain：先尝试页面运行时 `fetch(credentials:"include")`，失败时回落到 hydration/meta/DOM 提取。B站、微博详情采用公开 HTTP API 优先、Browser 兜底。X 采用 Tweet lookup API 优先、公开 oEmbed 次优先、Browser 最后兜底。Reddit 采用 OAuth API 优先、RSS 次优先、Browser 最后兜底。输出 JSON 的 `platform_metadata` 会包含 `read_channel`、`extraction_source`、`api_http_status`、`api_error_code` 等诊断字段。`LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`、内容删除/私密会作为结构化错误返回。

### 单条 URL

```bash
digcrawl collect "https://mp.weixin.qq.com/s/xxxx"
digcrawl collect "https://www.toutiao.com/article/xxxx"
digcrawl collect "https://www.toutiao.com/video/xxxx"
digcrawl collect "https://www.reddit.com/r/artificial/.rss" --format md,json
digcrawl collect "https://www.bilibili.com/video/BVxxxx"
digcrawl collect "https://m.weibo.cn/detail/xxxx"
digcrawl collect "https://www.kuaishou.com/short-video/xxxx"
digcrawl collect "https://www.zhihu.com/question/123/answer/456"
digcrawl collect "https://example.com/article" --format md,html,json --out ./my-output
```

### 平台登录与健康检查

部分平台在未登录或 Cookie 过期时会返回 `LOGIN_REQUIRED`。可使用有头浏览器完成登录并保存 Cookie：

```bash
digcrawl browser login --platform xiaohongshu
digcrawl browser login --platform douyin
digcrawl browser login --platform toutiao
```

登录后可检查会话，并可选试读一个 URL：

```bash
digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/xxxx" --trial
```

| 参数 | 说明 |
|------|------|
| `--config` | 配置文件路径 |
| `--out` | 覆盖 `Collect.OutputDir` |
| `--format` | 逗号分隔：`md`、`html`、`json` |
| `--file` | 文本文件，每行一个 URL |

导出的文章文件名会清理非法字符，并按 UTF-8 边界截断长标题；`--format md,json,html` 会同时保留 Markdown、JSON 所需文本字段和 HTML。

### 批量文件

```bash
digcrawl collect --file urls.txt --format md,json --out ./collect-output
```

### 收件箱（inbox）

将待采集链接放入 `collect-inbox/`（默认名，可在配置中修改）：

- 支持 `.url`、`.txt` 等文本文件，每行一个 URL
- 处理成功后移至 `processed/`
- 失败记录移至 `failed/`

```bash
# 处理一次
digcrawl collect inbox

# 持续轮询（Ctrl+C 停止）
digcrawl collect inbox --watch
```

批处理受 `Collect.RateLimitMs` 节流，避免对目标站请求过快。
若配置了 `Collect.PlatformRateLimits`，会按 URL 检测到的平台使用更细粒度的间隔，并加 ±20% jitter。

### 查询与迁移（SQLite）

当 `PersistMode` 为 `sqlite` 或 `both` 时：

```bash
digcrawl collect list
digcrawl collect list --q wechat --limit 50
digcrawl collect show <collect-id>
digcrawl collect show "https://mp.weixin.qq.com/s/..."
digcrawl collect migrate-index   # 将已有 index.json 导入 collect.db
```

### 输出目录结构

```text
collect-output/
  index.json                 # PersistMode=index|both
  2026-05-21/
    文章标题.md
    文章标题.html
    文章标题.json
    文章标题.assets/         # 媒体与截图
      media.json
      001.jpg

storage/digcrawl/
  collect.db                 # PersistMode=sqlite|both

collect-inbox/
  pending.url                # 待处理（示例文件名）
  processed/                 # 已成功
  failed/                    # 失败记录
```

更细的实现说明见 [`collect/README.md`](collect/README.md)。

---

## 9. 内置工具清单

通过 `GET /manifest` 或 MCP 可查看完整 schema；下表为常用工具名。

### 网关示例（example）

| 工具名 | 说明 |
|--------|------|
| `gateway.ping` | 健康检查，返回运行时信息 |
| `file.read` | 读取 `AllowedReadPaths` 下的本地文件 |

### 平台内容（platform）

| 工具名 | 说明 |
|--------|------|
| `wechat.article.read` | 微信公众号文章 |
| `xiaohongshu.note.read` | 小红书笔记 |
| `douyin.video.read` | 抖音视频页 |
| `toutiao.article.read` | 今日头条文章 |
| `toutiao.video.read` | 今日头条视频 |
| `x.post.read` | X 帖子 |
| `bilibili.video.read` | B站视频页 |
| `weibo.post.read` | 微博详情页 |
| `kuaishou.video.read` | 快手视频页 |
| `zhihu.content.read` | 知乎回答 / 专栏文章 / 视频 |

### 浏览器（browser）

| 工具名 | 说明 |
|--------|------|
| `browser.browse` | 打开 URL，提取正文 / 截图 / 摘要 |
| `browser.snapshot` | 无障碍树快照（可交互元素） |
| `browser.action` | 点击、输入等（高风险，建议需用户确认） |

### 语音转写（asr）

| 工具名 | 说明 |
|--------|------|
| `asr.transcribe` | 读取 `AllowedReadPaths` 下的视频/音频文件，用 ffmpeg 抽音频后转写 |

### 开发桩（stub，需 `StubEnabled: true`）

用于验证 Provider 加载流程的占位工具，**不要在生产开启**。

---

## 10. HTTP API 参考

基址：`http://<ListenAddr>`（默认 `http://127.0.0.1:8787`）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `GET` | `/manifest` | 工具清单（ToolManifest） |
| `POST` | `/tools/call` | 执行工具（请求体为 `ToolCall` JSON） |
| `GET` | `/artifacts/{id}` | 下载 Artifact（如截图） |

鉴权（当 `Gateway.AuthToken` 非空时）：

- Header：`Authorization: Bearer <token>`
- 或：`X-DigCrawl-Token: <token>`

---

## 11. 宿主项目接入（Go SDK）

若你的业务服务需要**主动调用**用户机器上的 Gateway（而非 Collector 反向连接），可在宿主侧使用 HTTP 客户端：

```go
import (
    "context"
    "encoding/json"
    "github.com/originaleric/digcrawl/client"
    "github.com/originaleric/digcrawl/protocol"
)

func callPing(ctx context.Context, gatewayURL, token string) error {
    c := client.New(gatewayURL, token)
    manifest, err := c.Manifest(ctx)
    if err != nil {
        return err
    }
    _ = manifest

    result, err := c.Call(ctx, protocol.ToolCall{
        ID:   "host-1",
        Tool: "gateway.ping",
        Input: json.RawMessage(`{}`),
    })
    if err != nil {
        return err
    }
    _ = result
    return nil
}
```

**两种集成模式对比：**

| 模式 | 谁发起连接 | 适用场景 |
|------|------------|----------|
| **Collector** | 本机连宿主 WebSocket | SaaS 统一下发任务、用户无需公网 IP |
| **HTTP Client** | 宿主连用户 Gateway URL | 用户自行暴露端口 / 内网穿透 |

宿主侧如需实现 Collector WebSocket 服务，可直接复用 `transport/host` 包：

```go
srv := host.NewServer("collector-token", "/digcrawl/v1/collector/ws")
http.ListenAndServe(":8790", srv.Handler())
```

业务系统可通过 `srv.Enqueue(instanceID, call)` 支持拉取模式，或用 `srv.Push(instanceID, call)` 立即推送任务。`digcrawl dev-host` 仍保留为本地调试参考。

---

## 12. 扩展自定义 Provider

实现 [`provider.Provider`](provider/provider.go) 接口：

```go
type Provider interface {
    Name() string
    Register(ctx context.Context, reg *registry.Registry) error
}
```

可选实现 `HealthReporter`（注册前健康检查）、`ToolManifester`（仅列出工具描述）。

在自定义启动代码中：

```go
rt, err := runtime.NewRuntime(cfg) // 或自行 NewRegistry 后组装 Runtime
```

默认通过 `runtime.DefaultProviders` 按配置加载内置 Provider；若需追加自定义 Provider，请参考 `runtime/bootstrap.go` 中的 `RegistryOptions.Providers` 字段注入。

---

## 13. 架构与目录

```text
protocol/           协议类型与 JSON 线格式
runtime/            注册表、策略、审计、Artifact、引导装配
transport/          http、collector、mcp、stdio、devhost
client/             宿主侧 HTTP SDK
collect/            检测、执行、导出、索引、收件箱
provider/           platform、browser、example、stub
internal/platform/  各平台页面解析
internal/browser/   go-rod 本地浏览器
```

数据与状态默认落在项目目录下：

- `storage/digcrawl/artifacts` — Artifact 文件
- `storage/digcrawl/browser_cookies` — 浏览器 Cookie
- `collect-output/`、`collect-inbox/` — Collect 产出与收件箱

---

## 14. 常见问题

### 浏览器工具报错「域名不在白名单」

在 `config.yaml` 的 `LocalBrowser.AllowedDomains` 与 `Gateway.AllowedDomains` 中加入目标域名，或放宽策略（注意安全边界）。

### Headless Chrome / Chromium 启动失败

如果报错包含 `Failed to get the debug url`、`can't find a browser binary`、`CreateUniqueTempDir`、`Crashpad` 或 `MachPortRendezvousServer`，优先使用上文的 Playwright `chrome-headless-shell` 方案，并在 `LocalBrowser.ExtraArgs` 中加入：

```yaml
ExtraArgs:
  - "--single-process"
  - "--no-zygote"
  - "--disable-dev-shm-usage"
  - "--disable-crash-reporter"
  - "--disable-crashpad"
  - "--disable-breakpad"
  - "--disable-features=Crashpad"
```

同时建议把 `UserDataDir` 指向 `/private/tmp/...` 这类当前进程可写目录。若需要复用登录态，则使用有头登录路径单独维护平台 Cookie，headless 采集路径使用稳定 profile。

### Collect 通用网页内容不完整

`browser.browse` 对动态站点依赖页面加载与选择器；可在工具 input 中指定 `wait_selector`、`content_selector`（通过 Gateway 直接调用时）。Collect 路径使用默认 full 模式。

### Collector 连不上宿主

检查 `ServerURL` 是否含协议（`https://`）、防火墙、宿主 WebSocket 路径是否与 `WSPath` 一致、Token 是否匹配。

### 与 DigEino 的关系

[DigEino](https://github.com/originaleric/digeino) 是 Eino / DigFlow 生态的工具 Provider 实现；DigCrawl 与之**解耦**，本仓库已内置 Collect 与平台/browser 工具。你可单独把 DigEino 作为 Provider 接到自建 runtime，也可仅使用 DigCrawl 独立发行版。

---

## 15. 相关链接

- 仓库：<https://github.com/originaleric/digcrawl>
- 示例配置：[`config/config.yaml`](config/config.yaml)
- Collect 模块：[`collect/README.md`](collect/README.md)
- 更新记录：[`updates/`](updates/)

---

## 16. 本地抓取测试方案

建议按“基础网页 → 头条 → 登录态平台 → 批量”的顺序测试，方便定位是导出、URL 归一化、登录态、API 通道还是 Browser 兜底的问题。

### 16.1 构建与基础检查

```bash
/Users/dig/.gvm/gos/go1.24.8/bin/go build -o digcrawl ./cmd/digcrawl
./digcrawl --help
```

### 16.2 通用网页 fallback

```bash
./digcrawl collect "https://example.com" --format md,json --out ./collect-output-test
```

预期：生成 md/json，说明 collect、browser 与 export 基础链路可用。

### 16.3 今日头条文章

```bash
./digcrawl collect "https://www.toutiao.com/article/你的文章ID/" \
  --format md,json --out ./collect-output-test
```

检查 JSON：

- `platform` 应为 `toutiao`
- `content_type` 应为 `article`
- `platform_metadata.read_channel` 为 `api` 或 `browser`
- `platform_metadata.extraction_source` 有值
- `title` / `text` 不为空

### 16.4 登录后试读

```bash
./digcrawl browser login --platform xiaohongshu
./digcrawl browser login --platform douyin
./digcrawl browser login --platform toutiao

./digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/你的文章ID/" --trial
```

如果未登录或风控，会看到结构化错误，如 `LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`。

### 16.5 小红书与抖音 URL

小红书优先使用带 `xsec_token` 的 URL：

```bash
./digcrawl collect "https://www.xiaohongshu.com/explore/笔记ID?xsec_token=xxx&xsec_source=pc_search" \
  --format md,json --out ./collect-output-test
```

抖音测试标准视频和 `modal_id`：

```bash
./digcrawl collect "https://www.douyin.com/video/视频ID" \
  --format md,json --out ./collect-output-test

./digcrawl collect "https://www.douyin.com/user/SEC_UID?modal_id=视频ID" \
  --format md,json --out ./collect-output-test
```

预期：`canonical_url` 归一到作品详情 URL，`platform_metadata.risk_flags` 可反映 `modal_id_resolved`、`xsec_token_missing` 等诊断信息。

### 16.6 批量文件

准备 `test-urls.txt`：

```text
https://example.com
https://www.toutiao.com/article/你的文章ID/
https://www.douyin.com/video/视频ID
```

执行：

```bash
./digcrawl collect --file test-urls.txt --format md,json --out ./collect-output-test
```

预期：批量采集按 `Collect.RateLimitMs` / `Collect.PlatformRateLimits` 节流；失败 URL 不影响其他 URL 继续处理。

### 16.7 查看输出与诊断字段

```bash
find ./collect-output-test -maxdepth 3 -type f | sort
```

重点查看每个 JSON 的：

```json
{
  "platform": "...",
  "content_type": "...",
  "title": "...",
  "platform_metadata": {
    "read_channel": "api 或 browser",
    "extraction_source": "...",
    "api_http_status": 200,
    "api_error_code": "...",
    "api_fetch_mode": "browser_context_fetch",
    "api_signature_mode": "page_runtime"
  }
}
```
