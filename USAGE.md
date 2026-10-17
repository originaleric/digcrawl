# DigCrawl 使用指南

DigCrawl 是面向任意 Agent 编排系统的**本地工具网关**：在用户机器上安全暴露浏览器、本地文件、私有 API 等能力，供云端宿主或 IDE 通过 HTTP、WebSocket、MCP、stdio 调用。

本文档覆盖安装、配置、全部 CLI 命令、Collect 采集、宿主接入与扩展开发。**快速概览见 [README.md](README.md)。**

---

## 1. 安装

### 使用 npx 安装

> 发布准备中：仓库公开、`digcrawl-skill` npm 包发布后可用。

已安装 Node.js 20.19+ 的用户可在 macOS、Linux 或 Windows 运行：

```bash
npx digcrawl-skill install
```

安装程序、完整 Skill 和默认 profile，自动配置 Codex；检测到 Claude Code 配置目录时安装其 Skill。也可指定宿主或仅安装程序：

```bash
npx digcrawl-skill install codex
npx digcrawl-skill install claude-code
npx digcrawl-skill install --all
npx digcrawl-skill install --skip-agent-setup
npx digcrawl-skill status
```

npm 安装器版本固定对应一个 GitHub Release（首版为 `v1.0.0`），不会在运行时自动切换到未知的新版本。升级安装器用 `npx digcrawl-skill@latest install`；指定程序版本用 `install --version vX.Y.Z`。仅支持 Release 已提供的 macOS / Linux amd64、arm64 和 Windows amd64。

程序使用 `~/.local/bin/digcrawl`（Windows 为 `digcrawl.exe`），MCP 注册绝对路径，不依赖 npm 缓存或 `PATH`。安装器不修改 shell 文件；手动运行 `digcrawl` 时可将该目录加入 `PATH`。Profile 和 Skill 路径与下方脚本一致；Windows profile 默认为 `%APPDATA%\\DigCrawl\\digcrawl-skill.yaml`，也遵循 `XDG_CONFIG_HOME`。

可用 `--install-dir`、`--skip-codex`，以及 `DIGCRAWL_INSTALL_DIR`、`DIGCRAWL_SKILL_DIR`、`CODEX_HOME`、`DIGCRAWL_CODEX_CONFIG`、`CLAUDE_CONFIG_DIR` 自定义路径。安装会保留已有 profile 和登录会话，更新前先停止该 profile 的 Bridge；完成后重启 Agent 并打开新对话。其他宿主使用安装器输出的 [MCP stdio 配置](./.agents/skills/digcrawl/mcp/README.md)。

### 从 Release 安装（推荐给开源用户）

常规用户不需要安装 Go。Release 提供 macOS Apple Silicon / Intel、Linux x86_64 / arm64 和 Windows x86_64 二进制。

macOS / Linux 可使用仓库内的安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

脚本自动识别系统与 CPU 架构，从 GitHub Release 下载对应压缩包与 `checksums.txt`，完成 SHA-256 校验后安装到 `~/.local/bin`。随后它会创建缺失的 Skill profile，把完整可移植 Agent Skill 安装到 `$XDG_DATA_HOME/digcrawl/skills/digcrawl`（未设置时为 `~/.local/share/digcrawl/skills/digcrawl`），并输出通用 MCP stdio 启动契约。它会在 Claude Code 已存在时安装 Claude Skill，并以原子方式替换 Codex 实际生效配置中的 `mcp_servers.digcrawl`，确保命令使用刚安装的稳定二进制而非临时构建路径；其它 Codex 设置与 MCP server 保留不变。MCP 和 Agent Skills 并不规定所有宿主共用的注册文件，因此安装器不会猜测或改写其它宿主配置。它不会使用 `sudo` 或修改 shell 配置。`--skip-codex` 只跳过 Codex；仅需二进制时传 `--skip-agent-setup`。要固定版本或先审查脚本：

```bash
curl -fsSLO https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh
sh install.sh --version vX.Y.Z
sh install.sh --skip-codex
sh install.sh --skip-agent-setup
```

也可以从 [GitHub Releases](https://github.com/originaleric/digcrawl/releases) 手动下载当前系统与 CPU 架构对应的压缩包，并用 Release 中的 `checksums.txt` 校验。Windows 用户请使用对应的 `.zip`。Homebrew tap 发布后可使用 `brew install --cask originaleric/tap/digcrawl`；它只是同一 GitHub Release 的便捷安装入口。

安装后执行（若未传 `--skip-agent-setup`，前两步已经由安装器完成）：

```bash
digcrawl version
digcrawl init --profile skill
digcrawl config validate --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

安装器注册或修复 Codex 后，请重启 Codex Desktop 并新建 task，使其重新加载 MCP 工具目录；旧 task 不会追溯性地加载启动时不可用的 MCP server。

### 浏览器能力（按需）

使用浏览器类工具时需本机 Chrome/Chromium。`ChromePath` 留空时 DigCrawl 会优先发现系统浏览器，再发现当前可用的 Playwright `chrome-headless-shell`；不要把 `chromium_headless_shell-<版本>` 这类会随 Playwright 更新而变化的路径写死到配置中。在 macOS sandbox 或 CI-like headless 环境中，可使用 Playwright `chrome-headless-shell` 作为兜底，并按需要配置启动参数。

### 本机 Headless Chromium（推荐兜底）

当系统 Chrome、Chrome for Testing 或 Rod 自动下载的 Chromium 无法 headless 启动时，可安装 Playwright 的 `chrome-headless-shell`：

```bash
mkdir -p "$HOME/.cache/digcrawl-playwright"
cd "$HOME/.cache/digcrawl-playwright"

npm_config_cache=/private/tmp/npm-cache \
PLAYWRIGHT_BROWSERS_PATH="$HOME/.cache/ms-playwright" \
npx -y playwright install chromium

export DIGCRAWL_PLAYWRIGHT_BROWSERS_PATH="$HOME/.cache/ms-playwright"
```

安装后保持 `LocalBrowser.ChromePath: ""`，DigCrawl 会动态选择当前可用的 shell。macOS sandbox 下通常还需要 `--single-process` / `--no-zygote` 以及禁用 crashpad：

```yaml
LocalBrowser:
  ChromePath: ""
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

### 从源码运行（仅开发者）

需要 Go 1.26.1+（以 `go.mod` 为准）。

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
| `mcp` | 用户 Skill profile：`$XDG_CONFIG_HOME/digcrawl/digcrawl-skill.yaml`；未设置 XDG 时为 `~/.config/digcrawl/digcrawl-skill.yaml`；Windows 为 `%APPDATA%\DigCrawl\digcrawl-skill.yaml` |
| `gateway` / `collector` / `collect` 等 | 命令行 `--config`，未指定时为 `config/config.yaml` |
| `video` | 显式 `--config` 时只使用该 profile；未指定时先使用项目 `config/config.yaml` 中已启用的处理器，再按处理器回退到标准用户 Skill profile，项目文件不存在时直接使用该 Skill profile |
| `browser login` / `browser check` | 显式 `--config` 时只使用该 profile；未指定时先使用项目 `config/config.yaml`，项目文件不存在时使用标准用户 Skill profile。恢复 MCP 会话时应传入启动该 MCP 的同一 profile 路径。 |
| `connect` / `login` | `$XDG_CONFIG_HOME/digcrawl/config.yaml`；未设置 XDG 时为 `~/.config/digcrawl/config.yaml`（可用 `--config` 覆盖） |
| 本地 GORM 数据库连接 | `config/gorm.yml`，由 `config/gorm.example.yml` 复制生成；包含密码，默认不入 Git |
| DigFlow 内嵌 Runtime 配置 | `config/digflow.yml`、`config/digflow-gorm.yml`、`config/eino.yml`、`agents/watchlist/agents.yml` |

首次 `connect` 或 `login` 会自动创建用户配置目录并写入配置。

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
| `DownloadMedia` | 是否下载正文中的媒体到 `.assets/`；这与 Provider `ImageDownload` 本地保存独立 |
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
| `AllowedDomains` | 独立 snapshot/action 与页面上下文 API fetch 的本地域名约束；通用 `browser.browse` 使用 `Gateway.AllowedDomains`，平台 provider 另外检查调用、配置或平台默认域名策略 |
| `ChromePath` | 指定 Chrome/Chromium/headless shell 可执行文件路径；为空时自动发现系统浏览器或当前可用的 Playwright 缓存，不应写死版本化缓存路径 |
| `Headless` | 是否无头模式 |
| `CookieStoreDir` | 按域名持久化 Cookie，便于登录态采集 |
| `CDPMode` / `CDPPort` | 后台抓取启动浏览器时开启 CDP 调试端口；`browser login` 始终使用私有动态端点，避免接管其他本机浏览器 |
| `CDPConnectExisting` | 连接已有 CDP 浏览器（如手动启动的 Chrome）；该进程由用户管理，需自行以 `--mute-audio` 启动，或使用下方专用服务 |
| `UserDataDir` | 持久化浏览器 profile 根目录 |
| `ProxyURL` | 浏览器代理地址，页面 fetch/API fallback 共用 |
| `ExtraArgs` | 仅后台抓取使用的 Chromium 启动参数；适合配置 `--single-process`、`--no-zygote`、禁用 crashpad 等 headless 兼容参数 |
| `InteractiveExtraArgs` | 仅 `browser login` 的可视 Chrome 使用；默认留空，不继承 `ExtraArgs`，避免 headless 进程参数影响人工输入与登录 |
| `TotalTimeoutSec` | 同一次工具调用中浏览器获取、读取、故障重试与后续浏览器 fallback 共用的总预算；页面清理另有短时限 |
| `NavigateTimeoutSec` | 单次导航时限，不延长总预算 |
| `WaitSelectorTimeoutSec` | 单次选择器等待时限，不延长总预算 |

### 本机 MCP Bridge（默认）

`digcrawl mcp` 首次被调用时，会为该 profile 启动一个私有 Unix socket Bridge。多个 Codex MCP stdio 子进程只转发协议，Bridge 才是唯一创建 Runtime 和 Chrome 的进程；因此没有固定 CDP 端口、没有 launchd/systemd 常驻浏览器，也不会因 MCP 重连重复争抢浏览器。配置文件的符号链接会归并到同一个 Bridge；而同一个持久化 `UserDataDir` 在任意时刻只能由一个 Bridge 使用。若确实需要两套独立配置，请给它们配置不同的 `UserDataDir`，避免权限、超时和登录态混用。Bridge 不使用空闲退出计时器：它会持续服务，直到异常退出或执行 `digcrawl bridge stop --config <profile>`；需要立即重启则用 `digcrawl bridge restart --config <profile>`。停止/重启不是平滑交接：它会立即取消在途 MCP 读取，待 Bridge 自己管理的 Chrome 已释放后才允许下一实例启动。Release 安装器使用已验证的临时新二进制，在替换安装目标前先停掉标准 Skill profile 的旧 Bridge，因此下一次 MCP 调用不会继续执行旧二进制。

Bridge 默认忽略 profile 中遗留的 `CDPMode`、`CDPConnectExisting` 与 `CDPPort`，直接管理 `ChromePath`（未配置时优先发现系统 Chrome）。这样旧的 CDP 服务短暂不可用、端口被占用或返回伪 HTTP 成功，都不再决定 MCP 是否可读页面。`digcrawl doctor --config <profile>` 分别检查浏览器依赖、Bridge 传输、运行构建与实际浏览器操作；实际操作通过已有 Bridge 创建并关闭测试页面，不启动第二个浏览器。首次 MCP 连接前若 Bridge 尚未运行，这项检查会明确报告未执行。

Codex 的 MCP proxy 仍是 stdio 协议所必需的宿主子进程，但不会再各自拥有 Runtime 或浏览器。它在无初始化时 1 分钟回收；任意已初始化宿主都会在 15 分钟无实际调用后回收。只有 `initialize` 和 `tools/call` 会刷新计时，MCP `ping` 不会制造永不退出的残留进程。

### 旧版固定 CDP 服务（仅兼容场景）

只有独立 CLI 或自定义宿主明确需要 `CDPConnectExisting: true` 时，才使用专用服务；MCP 默认不需要它：

```bash
./scripts/digcrawl-cdp-browser-service.sh install
./scripts/digcrawl-cdp-browser-service.sh start
./scripts/digcrawl-cdp-browser-service.sh status
```

该服务是当前用户的手动服务：`install` 只保存配置，既不会登录自启，也不会在 Chrome 退出后自动拉起；用 `stop` 或 `uninstall` 清理。CDP 仅绑定回环地址，但仍允许同机进程控制该浏览器；不要在不受信任的多用户主机复用 profile。

### Platform（平台读取）

| 字段 | 说明 |
|------|------|
| `BrowseRetryMaxAttempts` | 平台内容验证 fallback 的尝试次数，上限为 2；不控制浏览器连接故障重试 |
| `BrowseRetryDelayMs` | 平台内容验证 fallback 的等待间隔；浏览器故障由读取层在原预算内独立处理 |
| `ReadMetricsEnabled` | 是否输出 `[platform-read]` 结构化日志 |
| `HealthProbes` | `digcrawl browser check --trial` 的平台默认试读 URL |

微信公众号公开文章默认优先使用 HTTP HTML 静态解析，成功时会在 `platform_metadata` 中标记 `read_channel=http`、`extraction_source=html`。只有网络 transient 或解析状态不确定时才进入 Browser 兜底；验证码、频控、删除、私密、登录要求和最终 URL 跳出微信域名会直接返回结构化错误。

X 单帖读取默认先尝试公开 Syndication 分发端点，无需 Cookie、账号或付费 API；成功时会解析正文、公开图片以及可用的最佳直链 MP4 引用。该端点限流、不可用、返回内容 ID 不匹配或结构变化时，继续既有 Tweet lookup API、公开 oEmbed 和 Browser 兜底。Browser 仅作为最后兜底，登录墙、删除、不可见和空壳页会返回结构化错误；这些链路都不会读取私密或登录受限内容。

`Platform.X` 可保留默认值；仅在需要关闭或为受控环境替换公开端点时配置：

```yaml
Platform:
  X:
    SyndicationEnabled: true
    SyndicationURL: "https://cdn.syndication.twimg.com/tweet-result"
    SyndicationTimeoutSec: 15
```

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
| `Provider` | OCR provider，目前支持 `openai_compatible`、`stub` |
| `APIKey` | OCR provider API key |
| `BaseURL` | OpenAI-compatible provider API base URL，默认 `https://api.openai.com/v1` |
| `Model` | OCR 使用的视觉模型 |
| `Prompt` | OCR 提示词 |
| `TimeoutSec` | OCR 单次请求超时 |
| `MaxImages` | 每条内容最多处理图片数 |
| `MaxImageBytes` | 本地图片转 data URL 前的大小上限 |
| `MaxTokens` | OCR 返回 token 上限 |
| `AppendToMarkdown` | 是否把 OCR 文本追加到 Markdown，默认 `true` |
| `AppendToText` | 是否把 OCR 文本追加到纯文本，默认 `false` |

OCR 有明确的两层边界：`Skill.OCR` 只控制 Agent Skill 的直接平台/浏览器读取；`OCR` 控制 Collect 导出，并保留为独立 Gateway `browser.browse` 的兼容回退。`Skill.OCR.Enabled=true` 是直接读取的默认处理开关：文章图片会作为内部输入进入 OCR，即使调用方保持 `include_media=false`；该参数只控制是否返回图片 URL，不会关闭 OCR。Collect 不会继承 `Skill.OCR`，因此不会因为聊天读取策略而产生隐藏的下载或识别。结果写入 `platform_metadata.ocr_results`，并记录 `ocr_count`、provider、model、图片来源和错误诊断。直接平台/浏览器调用会把截图作为显式 OCR 候选；Collect 下载媒体时会优先对本地 `.assets/` 图片做 OCR，未下载媒体时会使用远程图片 URL。OpenAI-compatible Provider 对本地图片和截图会转换为 data URL，再调用 `/chat/completions`。合法响应但没有可读文字的图片或视频帧会保留为正常空结果：不计入 `ocr_count`、不追加空的 `## OCR` 段；HTTP 失败、响应无法解析或响应缺少 `choices` 仍会记录为错误。

对于只有 BGM 或没有可用语音的视频，可以显式开启本地视频抽帧 OCR。Collect 在 `Collect.DownloadMedia=true` 时会处理其导出的视频；直接平台读取（如 `youtube.video.read`）则需同时显式请求 `include_ocr=true`、启用 `Skill.Media.DownloadMedia` 和 `Skill.OCR.VideoFrames.Enabled`。Provider 仅负责下载受限本地 MP4，并把文件路径传给既有 ASR/抽帧 OCR core；这些 core 只输入视频、输出识别结果，不感知平台、artifact 或 transport。直接读取默认在识别后删除 MP4。若用户已显式在 `VideoDownload.Providers` 启用该平台，同一次读取会复用保存后的本地文件供 ASR/抽帧 OCR 使用。Collect 的 MP4 导出是独立交付路径，不参与 ASR/OCR 的输入契约。DigCrawl 以固定间隔调用本机 `ffmpeg` 抽取 JPEG，再交给已配置的 OCR provider；临时帧在识别后删除，不会写入 `.assets/` 或 OCR 结果。它不替代 ASR：`ASR.Enabled=true` 处理音频，`OCR.VideoFrames.Enabled=true` 处理内嵌文字；两者均开启时各自执行，任一失败不会阻止另一项。`MaxFrames` 与 `TimeoutSec` 分别是每个视频的抽帧数量和 ffmpeg 运行上限；`MaxTotalFrames` 则是单条内容的硬总量预留上限，按媒体出现顺序分配，失败或没有可抽帧的视频也会占用其预留份额，以防异常媒体绕过限制。`OCR.MaxImages` 只限制文章图片与截图，两类预算互不抢占。若 provider 是远程服务，抽出的帧会发送给它，因此默认关闭。

### Provider 视频持久保存

`VideoDownload` 是已有平台读取结果的本地保存开关，不增加第二套平台下载器。仅当 `Providers` 中的平台显式为 `true` 时，DigCrawl 才会要求该平台读取媒体并保存已解析到的单文件视频；每次保存创建私有子目录。平台读取可能返回同一内容的多个清晰度、编码或播放清单，通用保存路径只尝试首个可下载视频候选，因此一次请求至多保留一个最终文件，`VideoDownload.MaxBytes` 限制该文件大小。YouTube 为合并音视频流会在同一私有目录使用至多三倍该值的临时空间，完成或失败后清理，最终保留文件仍受 `MaxBytes` 限制。当前支持 `bilibili`、`douyin`、`instagram`、`kuaishou`、`threads`、`toutiao`、`weibo`、`xiaohongshu`、`youtube`、`zhihu`。X、Reddit 和微信公众号不在本次范围。受登录、风控、DRM、播放列表或非视频响应限制而无法取得单文件时，不会伪造保存成功。

```yaml
VideoDownload:
  # 空值时，Skill profile 使用 profile 同级的 media 目录；启用 provider
  # 的手写配置请使用绝对路径。
  Dir: "/absolute/path/to/digcrawl/videos"
  Providers:
    toutiao: true
    douyin: true
    youtube: true
  MaxBytes: 536870912
  TimeoutSec: 300
```

MCP 宿主应仅在用户明确要求后调用 `skill.media.configure`，例如 `{"video_download_providers":{"toutiao":true},"media_dir":"/absolute/path/to/digcrawl/videos","video_download_max_bytes":268435456,"video_download_timeout_sec":90}`。调用中的 provider 键会与已有策略合并，`false` 关闭指定平台，空对象 `{}` 清空整个策略。不传 `media_dir` 时保留 profile 的默认目录；传入时必须为绝对路径。`video_download_max_bytes` 与 `video_download_timeout_sec` 分别写入新保存路径实际使用的 `VideoDownload.MaxBytes` 和 `VideoDownload.TimeoutSec`。结果的 `platform_metadata.video_download_status` 为 `saved`、`partial`、`failed` 或 `not_available`；只有成功保存的条目会返回相对于 `VideoDownload.Dir` 的 `retained_media_relative_path(s)`，不会返回本机绝对路径。失败时会附带脱敏的 `video_download_failure_reason` 与 `video_download_attempted_count`；这些诊断和返回错误不会包含签名媒体 URL 或本机绝对路径。若 Collect 已成功解析正文但 provider 保存失败，Collect 仍会导出正文，并按原有 `Collect.DownloadMedia` 路径尝试媒体归档；只有实际保存成功、部分成功或确认无视频时才跳过重复视频导出。调用者若显式请求 `include_media=true`，仍按既有媒体引用契约取得 provider 返回的 URL。空 provider 策略或未启用的平台保持原有 `Collect.DownloadMedia` 行为，图片和其他媒体导出也不受影响。旧 `Skill.Media.RetainDownloadedVideo` / `MaxBytes` 仅保留给未声明 `VideoDownload` 的旧 YouTube 配置兼容，不能用来开启新的 provider 保存。

### Provider 图片持久保存

`ImageDownload` 是全部单内容读取渠道共用的本地原图保存开关，与 `VideoDownload`、OCR 和 Collect `.assets` 相互独立。仅当 `Providers` 中的 provider 显式为 `true` 时，DigCrawl 才会保存读取结果中已解析的 HTTP(S) 原图。通用平台、Reddit 和 `browser.browse` 已接入；浏览器仅使用已渲染 `<img>` 的 HTTP(S) 地址，不把截图当作下载原图。每次读取在 `ImageDownload.Dir` 下创建私有目录：一条内容的图片要么全部在 `MaxBytes`、`MaxItems` 与 `TimeoutSec` 约束内保存，要么回滚，不留下半套结果。成功路径只以相对 `ImageDownload.Dir` 的路径返回，失败原因会脱敏，不泄露本机绝对路径或签名 URL。启用它本身不会启动 OCR；若同次读取显式启用 OCR，则复用已保存的本地图片。Collect 的 `DownloadMedia` 仍控制自己的 `.assets`；Provider 图片保存成功且 Collect OCR 关闭时才会跳过重复图片下载，其他情形维持既有 Collect 行为。

```yaml
ImageDownload:
  Dir: "/absolute/path/to/digcrawl/images"
  Providers:
    x: true
    reddit: true
    wechat: true
    web: true
  MaxBytes: 10485760
  MaxItems: 8
  TimeoutSec: 60
```

```yaml
OCR:
  Enabled: true
  Provider: openai_compatible
  APIKey: "<vision-api-key>"
  # 示例：智谱 OpenAI-compatible 视觉模型；也可替换为其他兼容端点和模型。
  BaseURL: "https://open.bigmodel.cn/api/paas/v4"
  Model: "glm-5v-turbo"
  VideoFrames:
    Enabled: true
    IntervalSec: 5
    MaxFrames: 8
    MaxTotalFrames: 16
    TimeoutSec: 120
    # 可选；为空时从 PATH 使用 ffmpeg。
    FFmpegPath: "ffmpeg"
```

请将 `APIKey` 仅写入本地私有配置，不要提交到仓库。`zhipu_ocr` 不是 DigCrawl 的 provider 名称；如使用智谱，请保持 `Provider: openai_compatible`，并选择支持 Chat Completions 图像输入的视觉模型。

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
| `TimeoutSec` | ffprobe、ffmpeg 与 ASR 共享的总超时下限；本地 ASR 会取 `max(该值, 媒体时长×2+20 秒)`，长视频可用命令的 `--timeout` 仅覆盖本次运行 |
| `MaxTimeoutSec` | 单次 ASR 的硬上限，默认 `1800` 秒；Runtime/MCP 会据此预留任务时间。若时长公式超过该值，会明确失败而非被外层 deadline 静默取消。 |
| `FFmpegPath` | ffmpeg 可执行文件路径，默认 `ffmpeg` |
| `TempDir` | 抽取音频的临时目录，空则使用系统临时目录 |
| `ModelCacheDir` | 本地 ASR 模型的唯一持久缓存目录；默认 `digcrawl asr setup` 会将本次选择的 local 模型下载至此并离线验证，`digcrawl asr check` 使用相同配置域检查可用性。旧版未配置该字段的 Skill 会在首次启动时原地复用旧 `HF_HOME`（或旧 cwd 下的 `.cache/huggingface`），并在新目录记录兼容状态，不移动模型、不依赖后续 cwd，也无需重新下载。显式配置该字段时该目录完全由用户负责。 |
| `AudioSampleRate` | ffmpeg 抽取 wav 的采样率，默认 `16000` |
| `MaxAudioBytes` | API provider 上传音频大小上限 |
| `LocalCommand` | 本地 ASR 命令，默认 `faster-whisper`；命令不存在且未配置 `LocalArgs` 时，会使用内置 runner，并依次查找 `python3`、`python`、`py` |
| `LocalArgs` | 本地 ASR 参数模板，支持 `{audio_path}`、`{model}`、`{language}`、`{output_dir}`；Skill profile 内带路径的命令和首个脚本参数相对 profile 目录解析 |

使用 DigCrawl 发布版不需要 Go。本地默认 runner 仍需要可用的 `ffmpeg` 与 Python 3.9–3.13。`asr setup`、`asr check` 与 `asr transcribe` 总是先选择一个明确的 ASR 配置域：不传 `--config` 时，会检查当前项目 `config/config.yaml` 与用户 Skill profile，并选择第一个实际声明 ASR 域的文件；不含根 `ASR` 或 `Skill` 的项目配置会被跳过。项目根 `ASR` 与 Skill profile 的 `Skill.ASR` 分别拥有独立模型缓存。一个配置文件同时声明两者时，必须显式传 `--scope root` 或 `--scope skill`；显式 scope 也必须在该文件中实际声明，命令不会猜测或回退。找不到任一配置会明确要求先执行 `digcrawl init --profile skill`，因此发布版不会向当前工作目录写入临时运行时。`digcrawl asr setup --yes` 会在所选域的隔离 venv 内安装 faster-whisper、下载并离线验证所选 local 模型；随后 `digcrawl asr check` 会以相同配置域离线校验该 DigCrawl 管理的运行时、ffmpeg 与模型。自定义 runner 或未由 DigCrawl 管理的本地命令，其脚本、运行时和模型均由外部管理；`asr check` 只确认入口命令可执行，并会明确输出“未验证”，不会报告 ready。如当前 provider 不是 `local`，setup 会预备 local 默认模型 `small`，但不会改写现有配置；请在转写时显式传入 `--provider local --model small`，或将所选域的 Provider / Model 改为目标值。`--model base` 同样仅为本次预备模型，后续使用时需传入相同模型或更新配置。配置了自定义 `LocalCommand` / `LocalArgs` 时，setup 需显式 `--skip-model`，随后会直接退出，不会创建 venv、安装依赖或访问网络。`digcrawl doctor` 仍只检查 MCP Skill profile；不要用它确认项目根 `ASR`。

配置示例：

```yaml
ASR:
  Provider: api
  APIKey: "<api-key>"
  BaseURL: "https://api.openai.com/v1"
  Model: ""
  ResponseFormat: "verbose_json"
  TimeoutSec: 300
  MaxTimeoutSec: 1800
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
| `MaxConcurrentRuns` | 同时执行的 Watchlist 来源任务数；默认 `1` 保持串行，设置大于 `1` 时显式启用并行 |

`Watchlist.Collect` 字段：

| 字段 | 说明 |
|------|------|
| `MaxConcurrentItems` | 单个来源内同时进行详情采集、摘要与投递的条目数；默认 `1` 保持串行。Collect 会保留唯一输出路径、串行更新 index.json，并按平台统一限流。 |

`Tasks` 是新的通用定时任务描述，推荐新配置优先使用 `Tasks`，旧 `Watchlist.Items` 继续兼容。当前已落地的任务类型：

| Type | 说明 |
|------|------|
| `watchlist.collect` | 通过 `internal/source` 拉取最新内容，复用 Collect、Summary 和 Notifications |
| `feed.digest` | `watchlist.collect` 的兼容别名，适合 RSS/Feed 摘要场景 |

`Tasks[].Schedule.Mode` 当前支持 `interval` 和 `daily`。`interval` 使用 `Every` 的 Go duration，例如 `6h`、`24h`、`30m`；`daily` 使用本机时区的 `At: "HH:MM"`，例如每天上午 9 点写 `At: "09:00"`。调度状态写入通用 `dc_tasks_runtime` / `dc_task_runs` / `dc_task_locks` / `dc_task_events`：任务完成后会按当前 schedule 更新 `next_run_at` 并判断是否到期。

Watchlist 的来源发现由 `internal/source.Service` 统一承载，当前覆盖 RSS/Atom、sitemap、HTML 链接发现、今日头条固定文章/作者页/视频列表页、X/Twitter 单帖/用户页链接发现、抖音作品/用户页链接发现、小红书笔记/用户页链接发现、微信公众号文章/列表页链接发现、Reddit listing/comment 来源，以及 B站、微博、快手、知乎、YouTube 的主页/列表页链接发现。Watchlist 只负责调度、去重、详情采集、摘要和通知，因此后续新增渠道应优先接入 `internal/source`，再由 Watchlist、Tasks 或其他 domain 复用。

推荐维护来源页或列表页 source，例如 `rss`、`sitemap`、`reddit_subreddit`、`reddit_user`、`toutiao_author`、`x_user`、`douyin_user`、`xiaohongshu_user`、`wechat_list`、`bilibili_user`、`weibo_user`、`kuaishou_user`、`zhihu_user`、`youtube_channel`、`web_list`。`toutiao_article`、`toutiao_video`、`x_post`、`douyin_video`、`xiaohongshu_note`、`wechat_article`、`reddit_post_comments`、`bilibili_video`、`weibo_post`、`kuaishou_video`、`zhihu_answer`、`zhihu_article`、`zhihu_zvideo`、`youtube_video` 属于详情页或评论变更监控，适合单次归档、评论更新或互动状态检查，不适合作为“新增 po 文”主来源。

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

Threads 已支持：

- `threads_post` / `post` / `thread`：固定 Threads 帖子 URL 监控，例如 `https://www.threads.com/@zuck/post/...` 或旧 `threads.net` 链接。该模式复用 `threads.post.read`，并统一归一到当前 `threads.com` canonical URL。
- `threads_user` / `user` / `profile`：用户主页列表发现，抽取 `/@user/post/...` 链接，发现结果使用 `threads:<post_id>` 去重。默认 `auth_mode=public`，不读取本机 Cookie；需要登录态时必须同时设置全局 `Social.Threads.AuthMode=local_session` 和 source options `auth_mode=local_session`。

YouTube 已支持：

- `youtube_video` / `video`：固定公开视频或 Shorts URL 监控，例如 `https://www.youtube.com/watch?v=...`、`https://youtu.be/...`、`https://www.youtube.com/shorts/...`；统一归一为 `https://www.youtube.com/watch?v=<video_id>`。
- `youtube_channel` / `channel`：频道页或列表页链接发现，抽取公开视频与 Shorts 的链接，并按 `youtube:<video_id>` 去重。
- `youtube.video.read` 通过本地浏览器读取标题、简介、频道、发布时间、封面和可公开取得的字幕轨。显式 ASR 或视频帧 OCR 请求时，YouTube provider 需要本机 `yt-dlp` 与 `ffmpeg`，下载并合并自适应流为受限本地 MP4，再直接调用通用 ASR 与视频抽帧 OCR。默认完成后删除 MP4；在 `VideoDownload.Providers.youtube=true` 时保留到 `VideoDownload.Dir`，并从返回的 `platform_metadata.retained_media_relative_path` 定位该文件。`include_media=true` 仅返回公开视频封面等媒体元数据，不承诺通过 stdio MCP 交付大 MP4。不会导出会过期的播放流 URL；没有公开字幕时仍返回页面元数据与简介。

YouTube 下载最高选择 360p，优先保证音轨、下载时延与抽帧效率；未启用 provider 保存的直接 Skill 理解受 `Skill.Media.MaxBytes` 限制，常规 Collect 导出受 `Collect.MaxMediaBytes` 限制；`VideoDownload.Providers.youtube=true` 时，最终保存文件和同次复用受 `VideoDownload.MaxBytes` 与 `VideoDownload.TimeoutSec` 限制，音视频合并的私有临时目录最多使用三倍 `MaxBytes`，完成或失败后清理。DigCrawl 调用 `yt-dlp` 时会忽略其用户/系统配置并禁用代理，避免继承下载器的外部网络或命令设置；`yt-dlp` 必须预先安装并位于 `PATH`，例如 macOS 可使用 `brew install yt-dlp`。

Instagram 已支持：

- `instagram_post` / `post`：固定 Instagram 图文 URL 监控，例如 `https://www.instagram.com/p/.../`。该模式复用 `instagram.content.read`。
- `instagram_reel` / `reel`：固定 Instagram Reels URL 监控，例如 `https://www.instagram.com/reel/.../`，内容类型归一为 `video`。
- `instagram_user` / `user` / `profile`：用户主页列表发现，抽取 `/p/...` 和 `/reel/...` 链接，发现结果使用 `instagram:<shortcode>` 去重。默认 `auth_mode=public`，不读取本机 Cookie；需要登录态时必须同时设置全局 `Social.Instagram.AuthMode=local_session` 和 source options `auth_mode=local_session`。

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

- `kuaishou_video` / `ks_video`：固定视频 URL 监控，例如 `https://www.kuaishou.com/short-video/...`、`/photo/...` 或 `v.kuaishou.com` 短链。该模式复用 `kuaishou.video.read`，优先走快手 Web GraphQL `visionVideoDetail`，读取本机 Cookie 后仍触发风控时返回 `CAPTCHA_REQUIRED`。
- `kuaishou_user` / `ks_user`：主页或列表页链接发现，优先走快手 Web GraphQL `visionProfilePhotoList`，失败后回落到 HTML/browser 链接发现，抽取 `/short-video/...` 和 `/photo/...` 内容链接。

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
    MaxConcurrentRuns: 3
  Collect:
    MaxConcurrentItems: 5
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

默认构建不编译私有 DigFlow runtime 依赖，`digflow_runtime` provider 会使用 stub 并回退到 extractive 摘要。DigFlow 已升级到 `v1.1.4`；需要真正嵌入 DigFlow runtime 时，先配置 SSH/GOPRIVATE，然后使用 `-tags digflow_embedded` 构建或测试。内嵌 runtime 首次构建会拉取 runtime 依赖树，建议显式设置可写 Go cache，避免本机权限或冷缓存导致长时间无输出。`config/digflow-gorm.yml` / `config/eino.yml` 中的 `${...}` 变量会由 DigCrawl 宿主侧在运行时展开到临时配置文件；缺失变量会直接报错，不会静默展开为空。

```bash
GOPRIVATE=github.com/originaleric/* \
GONOSUMDB=github.com/originaleric/* \
GOPATH=/private/tmp/digcrawl-gopath \
GOMODCACHE=/private/tmp/digcrawl-gomodcache \
GOCACHE=/private/tmp/digcrawl-go-cache \
TMPDIR=/private/tmp \
go test -tags digflow_embedded ./internal/summary
```

数据库 DDL 已对齐 DigFlow `v1.1.4`。`pkg/digflow.NewRuntime`、`Invoke`、`Stream` 仍是本仓库使用的内嵌 Runtime 接口；digcrawl 当前内嵌路径仍直接使用 `pkg/digflow`，无需迁移 DTO import。本地单机运行推荐使用 SQLite，远端 MySQL 部署使用 MySQL DDL：

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
digcrawl init --profile skill  # 创建用户级 Skill profile（已有文件须 --force）
digcrawl config validate --config <profile> # 校验 profile YAML、路径、启用能力与边界
digcrawl doctor --config <profile> # 检查 profile、artifact、浏览器、ffmpeg 与 MCP 初始化
digcrawl doctor --diagnostic-id <ID> # 查询返回诊断编号对应的安全本地摘要
digcrawl stdio [flags]         # 逐行 JSON 协议
digcrawl dev-host [flags]      # 本地参考宿主（仅开发调试）
digcrawl collect <url>         # 采集单个 URL
digcrawl collect inbox         # 处理收件箱
digcrawl browser login         # 平台扫码/交互登录并保存 Cookie
digcrawl browser check         # 检查平台会话，可选试读
digcrawl asr setup --yes        # 使用项目配置或用户 Skill profile，安装隔离 faster-whisper 并验证所选 local 模型
digcrawl asr check              # 以与 setup/transcribe 相同的配置域离线检查本地 ASR
digcrawl asr transcribe <file> --timeout 1200 # ffmpeg 抽音频并转写任意本地视频/音频
digcrawl video <local-file-or-direct-url> [--mode auto|asr|ocr|both] # 解析本地视频或可直接下载的视频 URL
digcrawl watchlist run         # 执行 watchlist 监控、摘要和通知
digcrawl watchlist serve       # 常驻调度，到期执行不同周期的 Tasks / Watchlist items
scripts/digcrawl-watchlist-service.sh install|rebuild|rebuild-restart|start|stop|restart|status|logs|uninstall  # macOS/Linux
.\\scripts\\digcrawl-watchlist-service.ps1 install|start|stop|restart|status|logs|uninstall                    # Windows
digcollect <url>              # 轻量采集入口，等价于 digcrawl collect <url>
digcrawl help
```

全局习惯：多数子命令支持 `--config <path>` 指定配置文件。

### 直接视频解析

`digcrawl video` 面向明确提供的本地视频路径或可直接下载的 `http`/`https` 媒体 URL；平台页面 URL 仍通过对应平台工具读取。`--mode auto` 只运行已启用的 ASR 与视频帧 OCR，也可显式传入 `asr`、`ocr` 或 `both`。结果默认是 JSON；使用 `--format text`、`--format md` 和 `--out <file>` 调整输出。

```bash
digcrawl video ./meeting.mp4
digcrawl video "https://cdn.example.com/meeting.mp4" --mode both --format md --out meeting.md
digcrawl video "https://cdn.example.com/meeting.mp4" --keep ./meeting.mp4
```

直链下载默认使用私有临时目录，并受 `--max-bytes`（默认 512 MiB）与 `--download-timeout`（默认 300 秒）限制，处理结束后删除。仅在用户明确要求保留该次直链下载时使用 `--keep <path>`；它不会读取或修改 `Skill.Media.RetainDownloadedVideo`。未传 `--config` 时，项目根级 ASR/OCR 中已启用的处理器优先，未启用的处理器可从标准用户 Skill profile 的 `Skill.ASR` / `Skill.OCR` 补齐；显式 `--config` 始终只使用指定 profile。

### Watchlist 常驻服务脚本

`scripts/digcrawl-watchlist-service.sh` 用于把 `digcrawl watchlist serve` 托管为用户级服务，方便本机或小型服务器部署。Windows 请使用对应的 PowerShell 脚本。

- macOS：生成 `~/Library/LaunchAgents/com.digcrawl.watchlist.plist`，并将启动脚本私有复制到 `~/Library/Application Support/DigCrawl/bin/`，再通过 `launchctl` 管理；`start` / `restart` 会自动迁移旧 plist 并刷新该副本，避免 launchd 被系统拒绝执行 `Documents` 等受保护目录中的项目脚本。
- Linux：生成 `~/.config/systemd/user/digcrawl-watchlist.service` 并通过 `systemctl --user` 管理。
- Windows：创建当前用户的 Task Scheduler 任务 `DigCrawl Watchlist` 并通过 PowerShell 管理。
- 日志：macOS 写入 `DIGCRAWL_LOG_DIR` 下的 `watchlist.out.log` / `watchlist.err.log`；Linux 默认进入 user journal，通过脚本的 `logs` 命令查看。
- macOS/Linux 的 `install`、`install-cli`、`build`、`rebuild` 和 `rebuild-restart` 会创建 `${DIGCRAWL_CLI_BIN_DIR:-~/.local/bin}/digcrawl` 到服务 binary 的符号链接，并把该目录写入登录 shell 的启动配置。新开终端或 source 对应配置后，才可直接使用 `digcrawl`；Windows 请把发布版 `digcrawl.exe` 路径作为 `DIGCRAWL_BIN`（或放在项目根目录）。
- 收到 Watchlist 登录失效通知时，运行通知中给出的 `<服务实际 executable> browser login --config <服务实际配置路径> --platform <platform>`；命令由服务的当前 executable 与已加载配置生成，避免把 Cookie 写入另一套 profile。未从文件加载配置时会省略 `--config`，保留原有默认解析。

常用命令：

```bash
DIGCRAWL_CONFIG="$PWD/config/config.yaml" \
./scripts/digcrawl-watchlist-service.sh install

./scripts/digcrawl-watchlist-service.sh status
./scripts/digcrawl-watchlist-service.sh logs
./scripts/digcrawl-watchlist-service.sh rebuild
./scripts/digcrawl-watchlist-service.sh rebuild-restart
./scripts/digcrawl-watchlist-service.sh restart
./scripts/digcrawl-watchlist-service.sh stop
./scripts/digcrawl-watchlist-service.sh uninstall
```

Windows（在 PowerShell 中）：

```powershell
$env:DIGCRAWL_CONFIG = "$PWD\config\config.yaml"
.\scripts\digcrawl-watchlist-service.ps1 install
.\scripts\digcrawl-watchlist-service.ps1 status
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

`install` 只有在服务已注册、已启动并通过运行状态检查后才会成功返回；不需要再补一次 `start`。`run-foreground` 只运行已有 binary。更新代码或 DigFlow 依赖后，使用 `./scripts/digcrawl-watchlist-service.sh rebuild` 单独重建，或使用 `./scripts/digcrawl-watchlist-service.sh rebuild-restart` 重建后重启。脚本会优先读取已安装 launchd plist / systemd unit 里的 `DIGCRAWL_BIN`，重建到服务实际使用的 binary 路径。`build` 保留兼容，也会使用同样的路径解析逻辑。

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

在 Codex、Claude Code、Cursor、WorkBuddy 等支持 MCP 的客户端中，将 DigCrawl 注册为 stdio 服务。Release 安装器会创建缺失的 Skill profile、保留完整的可移植 Skill bundle，并自动修复 Codex 的有效 `mcp_servers.digcrawl` 配置；检测到 Claude Code 时会安装其 Skill。手动解压 Release 或使用 `--skip-agent-setup` 时，先运行 `digcrawl init --profile skill`，再用 `digcrawl config validate --config <profile>` 和 `digcrawl doctor --config <profile>` 检查本机 profile。新安装时 `digcrawl mcp` 不传 `--config` 会发现用户级 `digcrawl-skill.yaml`；在宿主配置中仍建议显式传入 profile 的绝对路径：

```json
{
  "mcpServers": {
    "digcrawl": {
      "command": "/path/to/digcrawl",
      "args": ["mcp", "--config", "/absolute/path/to/digcrawl-skill.yaml"]
    }
  }
}
```

启动后，IDE 内的 Agent 通过 MCP 调用本机已注册工具。MCP 入口会按需连接同 profile 的本机 Bridge；Bridge 独占 Runtime 与浏览器，HTTP `gateway` 仍是独立的显式服务。

除 Codex 外，安装器不会猜测宿主的 MCP 配置文件。请将完整 `skills/digcrawl/` 目录复制或链接到宿主规定的 Skill 目录，并按[通用 stdio 启动契约](./.agents/skills/digcrawl/mcp/README.md)登记上面的 `command` 与 `args`；不要只复制 `SKILL.md`。Codex 的适配器额外设置了空闲回收参数，通用 MCP 配置不应照抄该参数。

`digcrawl mcp` 把稳定的本机 Bridge 与可回收的 stdio 子进程分开管理：Bridge 每个 profile 至多一个，只在显式停止或异常退出时关闭，避免在预检与 `tools/call` 之间消失；stdio 子进程才承担回收。无论是否显式传入正值 `--idle-timeout`，未发送 MCP `initialize` 且 stdin 连续空闲 1 分钟的管道都会回收；已初始化客户端则在连续 15 分钟没有实际工具调用后回收，避免宿主 clientInfo 名称变化绕过回收。计时器只在 `initialize` 或实际 `tools/call` 时刷新，MCP `ping` 等传输保活包不会让闲置子进程永远存活。正值 `--idle-timeout` 仅覆盖已初始化 session 的 15 分钟间隔；仅在确实需要关闭回收时传 `--idle-timeout=0`。

### Agent Skill 最小 profile

使用仓库级 `digcrawl` Skill 时，优先通过 `digcrawl init --profile skill` 创建用户目录中的最小 profile；也可复制 `config/digcrawl-skill.example.yaml` 自行维护。`init` 会生成 profile 同级、跨平台的绝对 `Skill.Media.Dir`、`VideoDownload.Dir` 与 `ImageDownload.Dir`，宿主配置只保存二进制和 profile 的绝对路径。图片文字和页面视觉理解优先由宿主 Agent 读取 MCP 返回的截图附件；`media` URL 只作来源引用。宿主也负责基于 Markdown、图片观察和 `asr_text` 完成总结。仅视频转写使用本地 `Skill.ASR`，用户明确请求时才需启用 `Skill.Media.DownloadMedia`、配置媒体目录和 ASR 依赖；下载的 MP4 默认只在本次理解期间存在。用户明确要求保存支持平台的视频时，host agent 应调用 `skill.media.configure`，以 `video_download_providers` 指定平台；命名项与已有策略合并，`false` 关闭一个平台，`{}` 清空策略，`media_dir` 仅接受绝对路径。图片保存使用同一工具的 `image_download_providers`、`image_download_dir`、`image_download_max_bytes`、`image_download_max_items` 和 `image_download_timeout_sec`；它同样只应在用户明确要求后启用，且不等同于开启 OCR。不要编辑 YAML，也不要用旧的 `retain_downloaded_video` / `max_bytes` 开启新平台保存；它们只兼容未声明 `VideoDownload` 的旧 YouTube profile。保存状态见 `platform_metadata.video_download_status`，成功文件只以相对于 `VideoDownload.Dir` 的路径返回，宿主不得要求或显示绝对路径。配置写入会同时受同进程准入和跨进程 profile 锁保护；若另一项本地配置更新尚未完成，请根据可重试的 `SKILL_PROFILE_BUSY` 稍后重试。`skill.asr.enable` 会在写入两个基础 opt-in 开关前检查 ffmpeg 与当前 provider，缺失依赖时不会写入配置。默认 Skill profile 不包含 OCR 或 LLM API Key，也不使用 DigFlow。

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
| YouTube 视频 / Shorts | `youtube.com/watch`、`youtube.com/shorts`、`youtu.be` | `youtube.video.read` |
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
digcrawl collect "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
digcrawl collect "https://example.com/article" --format md,html,json --out ./my-output
```

### 平台登录与健康检查

部分平台在未登录或 Cookie 过期时会返回 `LOGIN_REQUIRED`。可使用 DigCrawl 的独立有头浏览器完成登录并保存 Cookie；它不会读取日常浏览器 profile。`browser login` 与 `browser check` 显式传入 `--config` 时使用该 profile；未传时先使用当前项目 `config/config.yaml`，项目文件不存在时才使用标准用户 Skill profile。恢复 MCP 会话时，必须传入启动该 MCP 的同一个 `--config <profile>`，避免把 Cookie 写入另一套 profile。`browser login` 必须在可交互终端直接运行：登录完成后该终端会等待一次 Enter 来保存 Cookie，因此不能用 `nohup`、launchd 或重定向标准输入启动；程序会在启动浏览器前拒绝这类调用。每个平台同一时间只允许一个 DigCrawl 登录：发现 SingletonLock 的本机进程仍存活时，命令会指向原终端，绝不接管或关闭该窗口；仅当该进程已确认退出时，才自动清理上次异常退出遗留的 Chromium 锁。保存完成或登录流程失败后，DigCrawl 会关闭由本次命令启动的专用 Chrome，并把 profile 未释放作为命令失败报告。命令会输出打开的登录 URL 和 Cookie 保存目录：

```bash
digcrawl browser login --platform xiaohongshu
digcrawl browser login --platform douyin
digcrawl browser login --platform toutiao
digcrawl browser login --platform threads
digcrawl browser login --platform instagram
digcrawl browser login --platform youtube
```

YouTube 登录使用普通 Chrome 的独立 profile：登录阶段不连接 CDP、不注入脚本。用户点击“登录”，完成 Google 登录并返回 YouTube，再确认保存；DigCrawl 正常关闭该专用 Chrome、等待 profile 释放后，才启动后台会话同步。Windows 用户需先关闭该专用 Chrome 的全部窗口，再确认。`youtube.video.read` 默认复用该配置目录中的 YouTube 会话；不会复制日常浏览器会话，也不会将 Google 域的 Cookie 写入 YouTube Cookie 存储。登录不保证消除平台的验证要求；若仍返回 `LOGIN_REQUIRED` 或 `CAPTCHA_REQUIRED`，按原错误处理。该会话接入用于浏览器读取，`yt-dlp` 媒体下载不自动继承它。

登录后可检查会话，并可选试读一个 URL：

```bash
digcrawl browser check --platform youtube \
  --url "https://www.youtube.com/watch?v=LP5OCa20Zpg" --trial
digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/xxxx" --trial
digcrawl browser check --platform threads \
  --url "https://www.threads.com/@zuck/post/xxxx" --trial
digcrawl browser check --platform instagram \
  --url "https://www.instagram.com/reel/xxxx/" --trial
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
| `youtube.video.read` | YouTube 视频 / Shorts 与公开字幕 |

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

先核对所用工具与实际跳转地址。通用 `browser.browse` 受调用策略与底层 `Gateway.AllowedDomains` 约束；独立 `browser.snapshot` 还保留 `LocalBrowser.AllowedDomains` 约束。平台 provider 按调用策略、配置、平台默认域名的优先顺序校验输入与已确认的最终来源，使用浏览器的路径同时受底层浏览器约束。Feed、Reddit RSS 跳转以及 B站合并的浏览器媒体同样受最终来源检查。只有确实需要访问目标域名时才调整对应配置；canonical 链接、内容中的外链和媒体 CDN 不等同于页面来源，域名策略也不是网络隔离。

### 浏览器超时或反复失败

`BROWSER_*` 错误会通过 MCP 的 `error_details` 返回 `stage`、`operation`、`reason`、`handled`、`retry_count`、`recovery_status`、`next_actions` 与 `diagnostic_id`（按实际故障提供）。同一次工具调用最多进行一次浏览器故障重试，平台 fallback 共用原时间预算；Collector 与 Agent 不应在 `handled: true` 后再自动重放整项工具。

- `send_queue`：等待当前读取结束，再发起新的读取；这类发送排队失败不会关闭健康连接。
- `cancelled` / `budget_exhausted`：本次调用已结束，确认仍需读取后重新操作。
- `page_changed`：读取期间导航或文档重载，混合结果已丢弃；等页面稳定后重新读取。
- profile 被占用：先完成或结束占用该专用 profile 的会话，不要关闭无关 Chrome。
- `LOCAL_BRIDGE_UNAVAILABLE` 或构建不匹配：关闭旧 DigCrawl MCP 连接，使用已安装的新二进制重启实际 profile 的 Bridge，然后重新连接宿主。中断的工具可能已执行，不要直接重放有副作用的操作。
- 外部 CDP 页面清理未确认：清理遗留页面；无法确认时重启该外部浏览器，再重启 DigCrawl。DigCrawl 不会替用户终止外部浏览器。

使用实际 MCP profile 的绝对路径排障；诊断编号摘要只包含安全分类，不输出 Cookie、原始浏览器异常或页面内容：

```bash
digcrawl doctor --config /absolute/path/to/digcrawl-skill.yaml
digcrawl doctor --diagnostic-id <returned-ID>
# 升级后或 next_actions 指明需重启时，先关闭旧 MCP 连接。
digcrawl bridge restart --config /absolute/path/to/digcrawl-skill.yaml
```

MCP 返回操作前后的进度通知需要宿主随请求提供 progress token。可用工具以当前 `tools/list` 为准；默认 Skill 通过 `browser.browse` 的 `mode: "snapshot"` 读取快照，不会自动开放独立 `browser.snapshot`。

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

---

## 16. 本地抓取测试方案

建议按“基础网页 → 头条 → 登录态平台 → 批量”的顺序测试，方便定位是导出、URL 归一化、登录态、API 通道还是 Browser 兜底的问题。

### 16.1 构建与基础检查

```bash
GOROOT=/Users/dig/.gvm/gos/go1.26.1 /Users/dig/.gvm/gos/go1.26.1/bin/go build -o digcrawl ./cmd/digcrawl
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

下面的命令适用于当前项目配置；如要验证 MCP 会话，请为 login 和 check 都传入启动该 MCP 的同一 `--config <profile>`。

```bash
./digcrawl browser login --platform xiaohongshu
./digcrawl browser login --platform douyin
./digcrawl browser login --platform toutiao
./digcrawl browser login --platform threads
./digcrawl browser login --platform instagram
./digcrawl browser login --platform youtube

./digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/你的文章ID/" --trial
./digcrawl browser check --platform threads \
  --url "https://www.threads.com/@zuck/post/你的帖子ID" --trial
./digcrawl browser check --platform instagram \
  --url "https://www.instagram.com/reel/你的短码/" --trial
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
