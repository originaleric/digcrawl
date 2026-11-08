# DigCrawl Usage Guide

English | [简体中文](USAGE.zh-CN.md)

DigCrawl is a **local tool gateway** for any agent orchestration system. It exposes browser, local file, and private API capabilities on the user's machine to cloud hosts and IDEs through HTTP, WebSocket, MCP, and stdio.

This guide covers installation, configuration, CLI commands, Collect workflows, host integration, and extension development. **See [README.md](README.md) for a quick overview.**

---

## 1. Installation

### Install a release (recommended for users)

Go is not required to use a release. Binaries are available for macOS Apple Silicon / Intel, Linux x86_64 / arm64, and Windows x86_64.

On macOS / Linux, use the repository's installation script:

```bash
curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
```

The script detects the OS and CPU architecture, downloads the matching GitHub Release archive and `checksums.txt`, verifies SHA-256, and installs to `~/.local/bin`. It then creates a missing Skill profile, installs the complete portable Agent Skill to `$XDG_DATA_HOME/digcrawl/skills/digcrawl` (or `~/.local/share/digcrawl/skills/digcrawl` when unset), and prints the generic MCP stdio launch contract. When Claude Code is present, it installs the Claude Skill. It atomically replaces `mcp_servers.digcrawl` in the effective Codex configuration so the command uses the newly installed stable binary rather than a temporary build path; other Codex settings and MCP servers are preserved. MCP and Agent Skills do not define a shared registration file for all hosts, so the installer does not guess or rewrite other hosts' configurations. It does not use `sudo` or modify shell configuration. `--skip-codex` skips only Codex; use `--skip-agent-setup` for a binary-only install. To pin a version or inspect the script first:

```bash
curl -fsSLO https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh
sh install.sh --version vX.Y.Z
sh install.sh --skip-codex
sh install.sh --skip-agent-setup
```

You can also download the archive for your OS and architecture from [GitHub Releases](https://github.com/originaleric/digcrawl/releases) and verify it against the release's `checksums.txt`. Windows users should use the matching `.zip`. Once the Homebrew tap is published, `brew install --cask originaleric/tap/digcrawl` provides a convenient entry point to the same GitHub Release.

After installation, run the following (the installer has already completed profile initialization and validation unless `--skip-agent-setup` was used):

```bash
digcrawl version
digcrawl init --profile skill
digcrawl config validate --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
digcrawl doctor --config "$HOME/.config/digcrawl/digcrawl-skill.yaml"
```

After the installer registers or repairs Codex, restart Codex Desktop and create a new task to reload the MCP tool catalog. Existing tasks do not retroactively load MCP servers that were unavailable when they started.

### Install with npx

With Node.js 20.19+ on macOS, Linux, or Windows:

```bash
npx digcrawl-skill@latest install
npx digcrawl-skill install codex
npx digcrawl-skill install claude-code
npx digcrawl-skill install --all
npx digcrawl-skill install --skip-agent-setup
npx digcrawl-skill status
```

The installer installs the program, complete Skill, and default profile, configures Codex, and installs the Claude Code Skill when detected. npm `digcrawl-skill@1.0.2` pins GitHub Release `v1.0.2`; use `install --version vX.Y.Z` to select another program version. Supported release targets are macOS / Linux amd64 and arm64, and Windows amd64.

The program is installed to `~/.local/bin/digcrawl` (`digcrawl.exe` on Windows). MCP uses absolute paths and does not depend on the npm cache or `PATH`. The installer does not edit shell configuration. Existing profiles and login sessions are preserved; the profile's Bridge is stopped before an update. Restart your agent and reconnect after installation. Custom paths are available through `--install-dir`, `--skip-codex`, `DIGCRAWL_INSTALL_DIR`, `DIGCRAWL_SKILL_DIR`, `CODEX_HOME`, `DIGCRAWL_CODEX_CONFIG`, and `CLAUDE_CONFIG_DIR`. Profile and Skill locations follow the script installation below.


### Browser capabilities (optional)

Background rendering uses only DigCrawl's managed `chrome-headless-shell`; manual sign-in uses separately identified Chrome for Testing. Desktop Chrome, system Chromium, Rod downloads and shared Playwright caches are never fallbacks. Missing or invalid runtime dependencies produce an actionable error.

The installer prepares the pinned runtime. Source and offline installations use:

```bash
digcrawl browser install
# Offline: supply both official ZIPs matching this platform and runtime-lock.json
digcrawl browser install --archive-dir /absolute/path/to/archives
```

The runtime lives in `$XDG_DATA_HOME/digcrawl/browsers`, or `~/.local/share/digcrawl/browsers` when unset. `DIGCRAWL_BROWSER_DIR` selects a separate managed directory. Keep `LocalBrowser.ChromePath: ""`; legacy desktop overrides are rejected. `DIGCRAWL_CHROME_PATH`, `CHROME_PATH` and `CHROME_BIN` no longer choose a background executable. Compatibility arguments are supplied by DigCrawl; ExtraArgs cannot override browser, profile or debugging ownership.

Inspect and explicitly migrate each actual configuration, including embedded host copies:

```bash
digcrawl browser migrate --config /absolute/path/to/profile.yaml
digcrawl browser migrate --config /absolute/path/to/profile.yaml --apply
```

Migration stops only verified old Bridge owners, clears the legacy ChromePath and retains a private original-profile backup. New browser profiles use the `digcrawl-isolated` child of UserDataDir. CookieStoreDir stays unchanged; daily browser data is never copied. Incompatible sessions require signing in again in the dedicated window. Update and restart every caller: installing a new CLI does not change running old binaries. Reinstall legacy CDP services using the updated scripts, with `DIGCRAWL_BIN` pointing to the updated CLI. External CDP must prove it uses the isolated runtime and profile.

Homebrew Cask pre-install steps call the same `browser install --migrate-default` entry point. Runtime readiness precedes standard-profile migration; macOS signing and quarantine protections are not bypassed. Direct installation also honors `DIGCRAWL_BROWSER_ARCHIVE_DIR`. The dedicated login window can show Chrome for Testing’s vendor testing notice; sign in on the target website without enabling browser account synchronization.

### Build from source (developers only)

Requires Go 1.26.1+ (see `go.mod`).

```bash
git clone https://github.com/originaleric/digcrawl.git
cd digcrawl
go build -o digcrawl ./cmd/digcrawl
go build -o digcollect ./cmd/digcollect
```

### Install to PATH (optional)

```bash
go install ./cmd/digcrawl
go install ./cmd/digcollect
# Binaries are usually placed in $GOPATH/bin or $HOME/go/bin
```

### Configuration file locations

| Context | Default path |
|------|----------|
| `mcp` | User Skill profile: `$XDG_CONFIG_HOME/digcrawl/digcrawl-skill.yaml`; `~/.config/digcrawl/digcrawl-skill.yaml` when XDG is unset; `%APPDATA%\DigCrawl\digcrawl-skill.yaml` on Windows |
| `gateway` / `collector` / `collect`, etc. | Command-line `--config`, or `config/config.yaml` when omitted |
| `video` | An explicit `--config` uses only that profile. Otherwise, enabled processors in the project's `config/config.yaml` take priority, with per-processor fallback to the standard user Skill profile. If the project file is absent, the Skill profile is used directly |
| `browser login` / `browser check` | An explicit `--config` uses only that profile. Otherwise, use the project's `config/config.yaml`, or the standard user Skill profile when the project file is absent. To restore an MCP session, pass the same profile path used to start that MCP connection. |
| `connect` / `login` | `$XDG_CONFIG_HOME/digcrawl/config.yaml`, or `~/.config/digcrawl/config.yaml` when XDG is unset (overridable with `--config`) |
| Local GORM database connection | `config/gorm.yml`, copied from `config/gorm.example.yml`; contains passwords and is excluded from Git by default |
| Embedded DigFlow Runtime configuration | `config/digflow.yml`, `config/digflow-gorm.yml`, `config/eino.yml`, `agents/watchlist/agents.yml` |

The first `connect` or `login` creates the user configuration directory and writes the configuration automatically.

---

## 2. Configuration Reference

See [`config/config.yaml`](config/config.yaml) in the repository. The main sections are described below.

### Gateway (HTTP gateway)

| Field | Description |
|------|------|
| `ListenAddr` | Listen address; default `127.0.0.1:8787` |
| `InstanceID` | Instance identifier included in the manifest |
| `AuthToken` | When nonempty, requires Bearer / `X-DigCrawl-Token` authentication |
| `AllowedTools` | Tool name allowlist; empty means unrestricted |
| `AllowedReadPaths` | Local path prefixes accessible to `file.read` |
| `AllowedDomains` | URL domain allowlist merged with policy |
| `ArtifactDir` / `ArtifactTTLMinutes` | Storage directory and TTL for large objects such as screenshots |
| `RequireApproval` | Whether high-risk tools must carry `approved: true` in `ToolCall` |

### Collector (outbound host connection)

| Field | Description |
|------|------|
| `ServerURL` | Host root URL, such as `https://api.example.com` |
| `WSPath` | WebSocket path; default `/digcrawl/v1/collector/ws` |
| `Token` | Authentication token agreed with the host |
| `InstanceID` | Local Collector instance ID; generated automatically when empty |
| `PullIntervalSec` | `>0` enables Collector task polling; `0` uses push mode |
| `MaxConcurrentCalls` | Maximum concurrent tool executions |
| `RateLimitCooldownMs` | Cooldown after a tool call completes for the same `RateLimitKey` |
| `CallQueueSize` | Bounded task queue per Collector session; returns `RATE_LIMITED` when full |
| `CallQueueWaitMs` | Reserved for compatibility; the current worker queue does not block the read loop on enqueue |
| `RetryMaxAttempts` / `RetryDelayMs` | Bounded retries for transient tool failures |

### Collect (local collection)

| Field | Description |
|------|------|
| `OutputDir` | Export root directory; default `collect-output` |
| `InboxDir` | Inbox directory; default `collect-inbox` |
| `DefaultFormats` | Default export formats: `md`, `html`, `json` |
| `DownloadMedia` | Download body media to `.assets/`; independent of Provider `ImageDownload` local retention |
| `PersistMode` | Persistence mode: `files` (files only), `index` (default, adds index.json), `sqlite` (adds collect.db), or `both` |
| `DBPath` | SQLite database path; default `storage/digcrawl/collect.db` |
| `IndexEnabled` | Write `index.json` in `index`/`both` modes; the same URL replaces its previous entry |
| `RateLimitMs` | Interval between URLs during inbox batches, in milliseconds |
| `PlatformRateLimits` | Per-platform batch intervals, such as `xiaohongshu`, `douyin`, `toutiao`, `reddit`, `bilibili`, `weibo`, `kuaishou`, `zhihu` |
| `InboxPollIntervalSec` | Polling interval for `inbox --watch` |

### LocalBrowser (local browser)

| Field | Description |
|------|------|
| `Enabled` | `false` disables the browser and providers that depend on it |
| `AllowedDomains` | Local domain constraints for standalone snapshot/action and page-context API fetch; generic `browser.browse` uses `Gateway.AllowedDomains`, while platform providers also check call, configuration, or platform-default domain policy |
| `ChromePath` | Leave empty for the pinned isolated runtime; desktop executable overrides are rejected |
| `Headless` | Whether to run headlessly |
| `CookieStoreDir` | Per-domain persistent Cookie storage for signed-in collection |
| `CDPMode` / `CDPPort` | Enable a CDP debug port for background browser launches; `browser login` always uses a private dynamic endpoint to avoid taking over another local browser |
| `CDPConnectExisting` | Connect to an existing CDP browser, such as manually launched Chrome; the user manages that process and should start it with `--mute-audio`, or use the service below |
| `UserDataDir` | Persistent browser profile root |
| `ProxyURL` | Browser proxy URL, shared by page fetch/API fallback |
| `ExtraArgs` | Chromium arguments for background collection only; useful for headless compatibility with `--single-process`, `--no-zygote`, or crashpad disabled |
| `InteractiveExtraArgs` | Arguments only for visible Chrome launched by `browser login`; empty by default and does not inherit `ExtraArgs`, so headless options do not affect interactive input or login |
| `TotalTimeoutSec` | Shared budget for browser acquisition, reading, failure retry, and subsequent browser fallback within one tool call; page cleanup has a separate short deadline |
| `NavigateTimeoutSec` | Per-navigation deadline; does not extend the total budget |
| `WaitSelectorTimeoutSec` | Per-selector wait deadline; does not extend the total budget |

### Local MCP Bridge (default)

The first `digcrawl mcp` call starts a private Unix socket Bridge for that profile. Multiple Codex MCP stdio subprocesses only forward protocol messages; the Bridge alone creates the Runtime and Chrome. There is no fixed CDP port or launchd/systemd browser service, and reconnecting MCP does not make independent browsers compete for the profile. Configuration symlinks resolve to the same Bridge, and a persistent `UserDataDir` can be used by only one Bridge at a time. Give separate configurations different `UserDataDir` values to avoid mixing permissions, timeouts, and sessions. The Bridge has no idle exit timer: it runs until a failure or `digcrawl bridge stop --config <profile>`. Use `digcrawl bridge restart --config <profile>` for an immediate restart. Stop/restart cancels in-flight MCP reads immediately and allows the next instance only after the Bridge-managed Chrome has released its resources. The release installer uses a verified temporary new binary to stop the standard Skill profile's old Bridge before replacing the installation target, so the next MCP call does not continue using an old Bridge build.

By default, the Bridge ignores legacy `CDPMode`, `CDPConnectExisting`, and `CDPPort` settings in the profile and manages only the verified isolated headless runtime. An unavailable legacy CDP service, occupied port, or misleading HTTP success no longer determines whether MCP can read pages. `digcrawl doctor --config <profile>` checks browser dependencies, Bridge transport, running build, and actual browser operations separately. The operation check opens and closes a test page through the existing Bridge rather than launching a second browser. Before the first MCP connection, an absent Bridge is explicitly reported as not tested.

The Codex MCP proxy is the host subprocess required by stdio; proxies share the Bridge described above. Initialized proxies follow the host pipe's lifetime by default. Uninitialized pipes are reclaimed after one minute of inactivity unless explicit `--idle-timeout 0` disables that guard. See [connection lifetime and OCR troubleshooting](#mcp-connection-lifetime-and-ocr-troubleshooting) for positive idle deadlines and migration from old settings.

### Legacy fixed CDP service (compatibility only)

Use the dedicated service only when a standalone CLI or custom host explicitly needs `CDPConnectExisting: true`; default MCP does not require it:

```bash
./scripts/digcrawl-cdp-browser-service.sh install
./scripts/digcrawl-cdp-browser-service.sh start
./scripts/digcrawl-cdp-browser-service.sh status
```

This is a manual service for the current user. `install` only saves configuration; it does not enable login startup or restart Chrome after exit. Use `stop` or `uninstall` for cleanup. CDP listens only on loopback, but other local processes can still control the browser; do not reuse the profile on an untrusted multi-user host.

### Platform (platform readers)

| Field | Description |
|------|------|
| `BrowseRetryMaxAttempts` | Platform content-validation fallback attempts, capped at 2; does not control browser connection retries |
| `BrowseRetryDelayMs` | Delay for platform content-validation fallback; browser failures are handled separately by the read layer within the original budget |
| `ReadMetricsEnabled` | Emit structured `[platform-read]` logs |
| `HealthProbes` | Default platform trial-read URLs for `digcrawl browser check --trial` |

Public WeChat articles use static HTTP HTML parsing first, returning `read_channel=http` and `extraction_source=html` in `platform_metadata` on success. Only transient network failures or uncertain parsing enter browser fallback. Captchas, rate limits, deletions, private content, login requirements, and final URLs outside WeChat domains return structured errors directly.

X single-post reading first tries the public Syndication endpoint, without Cookies, an account, or a paid API. On success it parses post text, public images, and the best available direct MP4 reference. Rate limiting, endpoint unavailability, mismatched content IDs, or response changes fall back to the existing Tweet lookup API, public oEmbed, and finally the browser. Login walls, deletions, invisible content, and empty shells return structured errors. These paths do not access private or login-restricted content.

Keep the default `Platform.X` settings unless disabling the public endpoint or replacing it in a controlled environment:

```yaml
Platform:
  X:
    SyndicationEnabled: true
    SyndicationURL: "https://cdn.syndication.twimg.com/tweet-result"
    SyndicationTimeoutSec: 15
```

Reddit reads use the official OAuth API first, supporting `reddit.post.read`, `reddit.subreddit.read`, and `reddit.user.read`. The single-post tool reads selftext, external links, score, upvote ratio, comment count, comment trees, and `more` comment placeholders. `comment_limit`, `comment_depth`, and `comment_sort` bound comment reading. If the API is unavailable, it tries Reddit's public `.rss` feeds before configured browser fallback. Browser fallback is for public pages only; it does not bypass 401/403/429, private communities, login walls, or restricted content.

RSS / Atom use the shared Feed service rather than Watchlist-specific parsing. `feed.read` reads `.rss`, `.atom`, `.xml`, `/feed`, and `/rss` URLs directly and returns Markdown/JSON entries. Watchlist `Platform: rss`, Collect RSS URLs, and Reddit RSS fallback share the same implementation.

Reddit settings live under `Platform.Reddit`:

| Field | Description |
|------|------|
| `APIEnabled` | Enable Reddit API reads; default `true` |
| `ClientID` / `ClientSecret` | Reddit app credentials; support `${ENV}` |
| `RefreshToken` | Used in refresh token mode; supports `${ENV}` |
| `UserAgent` | Explicit User-Agent required by the Reddit API |
| `OAuthMode` | `client_credentials` or `refresh_token` |
| `APIBaseURL` | Default `https://oauth.reddit.com` |
| `TokenURL` | Default `https://www.reddit.com/api/v1/access_token` |
| `RateLimit.RequestsPerMinute` | Local rate limit |
| `Comments.DefaultLimit` / `DefaultDepth` / `DefaultSort` | Default single-post comment budgets |
| `BrowserFallback.Enabled` | Allow browser fallback after API failure |
| `BrowserFallback.AllowWithoutOAuth` | Allow public reads without configured OAuth; default `false` |

Example configuration:

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

### OCR (image text extraction)

| Field | Description |
|------|------|
| `Enabled` | Enable OCR; default `false` |
| `Provider` | OCR provider; supports `openai_compatible`, `stub` |
| `APIKey` | OCR provider API key |
| `BaseURL` | OpenAI-compatible provider API base URL; default `https://api.openai.com/v1` |
| `Model` | Vision model used for OCR |
| `Prompt` | OCR prompt |
| `TimeoutSec` | Per-request OCR timeout |
| `MaxImages` | Maximum images processed per content item |
| `MaxImageBytes` | Local image size limit before data URL conversion |
| `MaxTokens` | Maximum OCR output tokens |
| `AppendToMarkdown` | Append OCR text to Markdown; default `true` |
| `AppendToText` | Append OCR text to plain text; default `false` |

OCR has two configuration scopes. `Skill.OCR` controls direct Agent Skill platform/browser reads. Root `OCR` controls Collect exports and remains a compatibility fallback for standalone Gateway `browser.browse`. `Skill.OCR.Enabled=true` enables default image processing for direct reads, even with `include_media=false`; that parameter only controls returned image URLs. Collect does not inherit `Skill.OCR`, so chat read policies do not cause hidden downloads or recognition. Results go to `platform_metadata.ocr_results`, with `ocr_count`, provider, model, image source, and error diagnostics. Direct platform/browser calls can use screenshots as explicit OCR candidates. Collect prefers local `.assets/` images when media is downloaded, otherwise remote image URLs. The OpenAI-compatible provider converts local images and screenshots to data URLs and calls `/chat/completions`. Valid responses with no readable text remain normal empty results: they do not increase `ocr_count` or append empty `## OCR` sections. HTTP failures, unparseable responses, or missing `choices` are still errors.

Local video-frame OCR can be explicitly enabled for videos with only background music or no usable speech. Collect processes exported videos when `Collect.DownloadMedia=true`. Direct platform reads such as `youtube.video.read` additionally require explicit `include_ocr=true`, `Skill.Media.DownloadMedia`, and `Skill.OCR.VideoFrames.Enabled`. The provider only downloads a bounded local MP4 and passes its path to the existing ASR/frame OCR cores; these take videos and return recognition results without knowledge of platforms, artifacts, or transports. Direct reads delete the MP4 after recognition by default. If the user enabled the platform under `VideoDownload.Providers`, the same read reuses the retained local file for ASR/frame OCR. Collect MP4 export is a separate delivery path, not an ASR/OCR input contract. DigCrawl calls local `ffmpeg` to extract JPEGs at fixed intervals and sends them to the configured OCR provider. Temporary frames are deleted afterward and are not written to `.assets/` or OCR results. This does not replace ASR: `ASR.Enabled=true` processes audio; `OCR.VideoFrames.Enabled=true` processes embedded text. Either can succeed if the other fails. `MaxFrames` and `TimeoutSec` bound frames and ffmpeg time per video. `MaxTotalFrames` reserves a hard total per content item in media order; failed or unextractable videos still consume their reservation so malformed media cannot bypass limits. `OCR.MaxImages` separately limits article images and screenshots. A remote provider receives extracted frames, so this feature is disabled by default.

### Provider video retention

`VideoDownload` retains results from existing platform readers without adding a second downloader. Only platforms explicitly set to `true` under `Providers` are asked to read media and retain resolved single-file videos, each in a private subdirectory. Readers may return multiple qualities, codecs, or playlists; the shared retention path tries only the first downloadable video candidate, retaining at most one final file per request under `VideoDownload.MaxBytes`. YouTube may temporarily use up to three times that size in the same private directory to merge audio/video; temporary files are cleaned on completion or failure, and the retained file still obeys `MaxBytes`. Supported platforms: `bilibili`, `douyin`, `instagram`, `kuaishou`, `threads`, `toutiao`, `weibo`, `xiaohongshu`, `youtube`, `zhihu`. X, Reddit, and WeChat articles are outside this scope. Login restrictions, risk controls, DRM, playlists, or non-video responses never produce a false success.

```yaml
VideoDownload:
  # When empty, the Skill profile uses a media directory beside the profile.
  # Use an absolute path in hand-written configurations that enable a provider.
  Dir: "/absolute/path/to/digcrawl/videos"
  Providers:
    toutiao: true
    douyin: true
    youtube: true
  MaxBytes: 536870912
  TimeoutSec: 300
```

MCP hosts should call `skill.media.configure` only after an explicit user request, for example `{"video_download_providers":{"toutiao":true},"media_dir":"/absolute/path/to/digcrawl/videos","video_download_max_bytes":268435456,"video_download_timeout_sec":90}`. Provider keys merge with the existing policy; `false` disables a platform and `{}` clears the policy. Omit `media_dir` to keep the profile default; an explicit directory must be absolute. `video_download_max_bytes` and `video_download_timeout_sec` write the actual retention settings `VideoDownload.MaxBytes` and `VideoDownload.TimeoutSec`. `platform_metadata.video_download_status` is `saved`, `partial`, `failed`, or `not_available`. Only saved entries return `retained_media_relative_path(s)` relative to `VideoDownload.Dir`, never absolute local paths. Failures include sanitized `video_download_failure_reason` and `video_download_attempted_count`, without signed media URLs or absolute local paths. If Collect parsed the body but provider retention failed, it still exports the body and tries media archiving through `Collect.DownloadMedia`. Duplicate video export is skipped only for actual saved/partial success or confirmed absence of video. Explicit `include_media=true` continues to return provider URLs under the existing media reference contract. Empty policies or disabled platforms preserve existing Collect behavior; image and other media exports are unaffected. Legacy `Skill.Media.RetainDownloadedVideo` / `MaxBytes` remain compatible only with old YouTube configurations lacking `VideoDownload`; they cannot enable retention for new providers.

### Provider image retention

`ImageDownload` controls local original-image retention across single-item reading channels, independently of `VideoDownload`, OCR, and Collect `.assets`. Only providers explicitly set to `true` under `Providers` retain resolved HTTP(S) originals. Generic platform readers, Reddit, and `browser.browse` support it. The browser uses HTTP(S) URLs from rendered `<img>` elements, never screenshots as original-image downloads. Each read creates a private directory under `ImageDownload.Dir`: all images for an item are saved within `MaxBytes`, `MaxItems`, and `TimeoutSec`, or rolled back with no partial set. Success returns paths relative to `ImageDownload.Dir` only; sanitized failures do not expose absolute local paths or signed URLs. Enabling retention does not start OCR. If OCR is explicitly enabled in the same read, it reuses retained local images. Collect's `DownloadMedia` still controls its own `.assets`; duplicate image downloading is skipped only when Provider image retention succeeded and Collect OCR is disabled. Other cases retain existing Collect behavior.

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
  # Example: a Zhipu OpenAI-compatible vision model; another compatible endpoint/model can be used.
  BaseURL: "https://open.bigmodel.cn/api/paas/v4"
  Model: "glm-5v-turbo"
  VideoFrames:
    Enabled: true
    IntervalSec: 5
    MaxFrames: 8
    MaxTotalFrames: 16
    TimeoutSec: 120
    # Optional; when empty, use ffmpeg from PATH.
    FFmpegPath: "ffmpeg"
```

Keep `APIKey` in local private configuration only; do not commit it. `zhipu_ocr` is not a DigCrawl provider name. For Zhipu, keep `Provider: openai_compatible` and choose a vision model supporting Chat Completions image input.

### ASR (video speech transcription)

| Field | Description |
|------|------|
| `Enabled` | Automatically transcribe video/audio downloaded by Collect; default `false`. CLI/tool calls can still run it through an explicit request |
| `Provider` | ASR provider: `api`, `local`, `stub` |
| `APIKey` | API provider key |
| `BaseURL` | OpenAI-compatible API base URL; default `https://api.openai.com/v1` |
| `Model` | Empty selects the provider default: `api=whisper-1`, `local=small` |
| `Language` | Optional language code |
| `Prompt` | Optional transcription prompt, forwarded by the API provider |
| `ResponseFormat` | API provider response format: `verbose_json`, `json`, `text`, `srt`, `vtt`; default `verbose_json` |
| `TimeoutSec` | Minimum shared total timeout for ffprobe, ffmpeg, and ASR. Local ASR uses `max(this value, media duration × 2 + 20 seconds)`. The command's `--timeout` overrides only the current run for longer videos |
| `MaxTimeoutSec` | Hard limit per ASR run; default `1800` seconds. Runtime/MCP reserve execution time accordingly. A duration estimate above this limit fails explicitly instead of being silently cancelled by an outer deadline |
| `FFmpegPath` | ffmpeg executable; default `ffmpeg` |
| `TempDir` | Temporary audio extraction directory; empty uses the system temporary directory |
| `ModelCacheDir` | Single persistent cache directory for local ASR models. By default, `digcrawl asr setup` downloads and verifies the selected local model offline here; `digcrawl asr check` uses the same configuration scope. For legacy Skill configurations without this field, first startup reuses the old `HF_HOME` (or the old working directory's `.cache/huggingface`) in place and records compatibility state in the new directory. Models are not moved, future working directories do not matter, and no redownload is needed. An explicit directory is entirely user-managed |
| `AudioSampleRate` | Sample rate for ffmpeg WAV extraction; default `16000` |
| `MaxAudioBytes` | Maximum audio upload size for the API provider |
| `LocalCommand` | Local ASR command; default `faster-whisper`. If absent and `LocalArgs` is unset, use the bundled runner and search for `python3`, `python`, then `py` |
| `LocalArgs` | Local ASR argument template: `{audio_path}`, `{model}`, `{language}`, `{output_dir}`. Commands containing paths and the first script argument in a Skill profile resolve relative to the profile directory |

Release users do not need Go. The default local runner still requires `ffmpeg` and Python 3.9–3.13. `asr setup`, `asr check`, and `asr transcribe` always select one explicit ASR scope. Without `--config`, they inspect the project's `config/config.yaml` and user Skill profile, choosing the first file that actually declares an ASR scope; a project configuration with neither root `ASR` nor `Skill` is skipped. Project root `ASR` and Skill profile `Skill.ASR` have separate model caches. A file declaring both requires explicit `--scope root` or `--scope skill`; an explicit scope must exist in that file, with no guessing or fallback. If neither configuration exists, the command asks for `digcrawl init --profile skill` rather than writing a temporary runtime into the current directory. `digcrawl asr setup --yes` installs faster-whisper in the selected scope's isolated venv, downloads the chosen local model, and verifies it offline. `digcrawl asr check` then checks that managed runtime, ffmpeg, and model offline in the same scope. Scripts, runtimes, and models for custom runners or unmanaged local commands are externally managed; `asr check` only verifies that the command is executable and explicitly reports "unverified", not ready. If the current provider is not `local`, setup prepares the default local model `small` without rewriting configuration; transcribe with `--provider local --model small`, or update the scope's Provider / Model. Likewise, `--model base` prepares only that model for this invocation; use it explicitly later or update configuration. With custom `LocalCommand` / `LocalArgs`, setup requires `--skip-model` and then exits without creating a venv, installing dependencies, or accessing the network. `digcrawl doctor` only checks the MCP Skill profile; do not use it to verify project root `ASR`.

Example configuration:

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

Local `faster-whisper` example:

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

Watchlist periodically checks configured sources for new content, reuses Collect for detail reads, generates summaries through a Summary provider, and sends notifications.

`Watchlist` fields:

| Field | Description |
|------|------|
| `Enabled` | Enable Watchlist; default `false` |
| `DryRun` | Allow logging-only runs without a real notification channel; default `false` |
| `DefaultInterval` | Default check interval in daemon mode; default `6h` |
| `Scheduler` | Built-in scheduler configuration for `watchlist serve` |
| `NotifyOnFirstRun` | Notify about historical content on the first run; default `false`, establishing a baseline only |
| `MaxItemsPerRun` | Maximum new items processed per watch item per run |
| `LockTTL` | Per-watch lock expiry to avoid cron / daemon reentry |
| `Persistence.Driver` | Watchlist state persistence driver; currently `sqlite`, default `sqlite` |
| `Persistence.SQLitePath` | Watchlist state database path; defaults to `Collect.DBPath` |
| `DBPath` | Legacy compatibility field; prefer `Persistence.SQLitePath` in new configurations |
| `Items` | List of monitoring targets |

`Watchlist.Scheduler` fields:

| Field | Description |
|------|------|
| `Enabled` | Enable the built-in scheduler; default `true` |
| `TickInterval` | Due-task scan interval; default `5m` |
| `Jitter` | Random delay before a run to avoid simultaneous triggers across instances |
| `MaxConcurrentRuns` | Concurrent Watchlist source tasks; default `1` is serial, values above `1` explicitly enable parallelism |

`Watchlist.Collect` fields:

| Field | Description |
|------|------|
| `MaxConcurrentItems` | Concurrent detail collection, summarization, and delivery within one source; default `1` is serial. Collect preserves unique output paths, serializes index.json updates, and applies shared per-platform rate limits |

`Tasks` is the general scheduled-task format recommended for new configurations; legacy `Watchlist.Items` remains supported. Implemented task types:

| Type | Description |
|------|------|
| `watchlist.collect` | Fetch new content through `internal/source`, reusing Collect, Summary, and Notifications |
| `feed.digest` | Compatibility alias for `watchlist.collect`, suitable for RSS/Feed digests |

`Tasks[].Schedule.Mode` supports `interval` and `daily`. `interval` uses a Go duration in `Every`, such as `6h`, `24h`, or `30m`. `daily` uses `At: "HH:MM"` in the local timezone, such as `At: "09:00"` for 9 a.m. Scheduler state is stored in `dc_tasks_runtime` / `dc_task_runs` / `dc_task_locks` / `dc_task_events`. After a task completes, its current schedule updates `next_run_at` for subsequent due checks.

Watchlist source discovery is handled by `internal/source.Service`. It supports RSS/Atom, sitemaps, HTML links, Toutiao articles/author pages/video lists, X/Twitter posts/user pages, Douyin works/user pages, Xiaohongshu notes/user pages, WeChat articles/list pages, Reddit listings/comments, and home/list pages for Bilibili, Weibo, Kuaishou, Zhihu, and YouTube. Watchlist handles scheduling, deduplication, detail collection, summaries, and notifications. Add future source channels to `internal/source` first so Watchlist, Tasks, and other domains can reuse them.

Prefer source or list pages such as `rss`, `sitemap`, `reddit_subreddit`, `reddit_user`, `toutiao_author`, `x_user`, `douyin_user`, `xiaohongshu_user`, `wechat_list`, `bilibili_user`, `weibo_user`, `kuaishou_user`, `zhihu_user`, `youtube_channel`, or `web_list`. Detail-page/comment-change sources—`toutiao_article`, `toutiao_video`, `x_post`, `douyin_video`, `xiaohongshu_note`, `wechat_article`, `reddit_post_comments`, `bilibili_video`, `weibo_post`, `kuaishou_video`, `zhihu_answer`, `zhihu_article`, `zhihu_zvideo`, `youtube_video`—suit one-time archiving, comment updates, or interaction checks rather than discovery of newly published posts.

Before deployment, use discover-only validation to check source discovery without writing `dc_watchlist_items` or triggering collect, summary, or notify:

```bash
digcrawl watchlist source test-discover --config config/config.yaml --id reddit-ai

curl -X POST -H "Authorization: Bearer $DIGCRAWL_GATEWAY_TOKEN" \
  http://127.0.0.1:8787/admin/watchlist/sources/reddit-ai/discover
```

`Watchlist.Items[].Options` supplies platform-specific parameters. Reddit supports:

- `reddit_subreddit`：`subreddit`、`sort`、`time_range`、`limit`、`after`
- `reddit_user`：`username`、`where`、`limit`、`after`
- `reddit_post_comments`：`comment_limit`、`comment_depth`、`comment_sort`、`expand_more`、`expand_more_limit`

Toutiao supports:

- `article` / `group` / `toutiao_article`: monitor a fixed article URL such as `https://www.toutiao.com/w/.../`, `https://www.toutiao.com/group/.../`, or `https://www.toutiao.com/article/.../`, reusing the verified `toutiao.article.read` path.
- `video` / `toutiao_video`: monitor a fixed video URL such as `https://www.toutiao.com/video/.../`, reusing `toutiao.video.read`. With `ASR.Enabled=true`, the downloaded video can produce `asr_text`.
- `toutiao_author` / `author` / `user`: discover links on author pages or video lists. Static `href`, escaped hydration URLs, and relative content paths are parsed; results deduplicate by `toutiao:<article_id>` or `toutiao:video:<video_id>`. Author pages fully dependent on dynamic APIs/login may still need a dedicated API or headless discovery implementation.

X / Twitter supports:

- `x_post`: monitor a fixed post URL such as `https://x.com/elonmusk/status/...`, reusing the verified `x.post.read` path.
- `x_user`: discover static status links on user profiles, deduplicating by `x:<status_id>`. X profiles typically load dynamically; unauthenticated/unrendered HTML may not expose recent posts. Production use may require future X API or browser-rendered discovery support.

Threads supports:

- `threads_post` / `post` / `thread`: monitor a fixed Threads post such as `https://www.threads.com/@zuck/post/...` or legacy `threads.net` links. Reuses `threads.post.read` and normalizes to current `threads.com` canonical URLs.
- `threads_user` / `user` / `profile`: discover `/@user/post/...` links on profile lists, deduplicating by `threads:<post_id>`. Default `auth_mode=public` does not read local Cookies. Session access requires both global `Social.Threads.AuthMode=local_session` and source option `auth_mode=local_session`.

YouTube supports:

- `youtube_video` / `video`: monitor a fixed public video or Shorts URL such as `https://www.youtube.com/watch?v=...`, `https://youtu.be/...`, or `https://www.youtube.com/shorts/...`, normalized to `https://www.youtube.com/watch?v=<video_id>`.
- `youtube_channel` / `channel`: discover public video and Shorts links on channel/list pages, deduplicating by `youtube:<video_id>`.
- `youtube.video.read` uses the local browser to read title, description, channel, publication time, cover, and publicly available subtitle tracks. Explicit ASR or video-frame OCR requires local `yt-dlp` and `ffmpeg` to download and merge adaptive streams into a bounded local MP4, then call the shared ASR/frame OCR cores. The MP4 is deleted afterward by default; `VideoDownload.Providers.youtube=true` retains it in `VideoDownload.Dir`, referenced by `platform_metadata.retained_media_relative_path`. `include_media=true` returns metadata such as public covers, without promising large MP4 delivery over stdio MCP. Expiring playback-stream URLs are not exported. When public subtitles are absent, page metadata and description are still returned.

YouTube downloads select at most 360p, prioritizing audio, download latency, and frame extraction efficiency. Direct Skill understanding without provider retention obeys `Skill.Media.MaxBytes`; normal Collect exports obey `Collect.MaxMediaBytes`. With `VideoDownload.Providers.youtube=true`, the retained file and same-call reuse obey `VideoDownload.MaxBytes` and `VideoDownload.TimeoutSec`. The private merge directory may use up to three times `MaxBytes`, cleaned on completion or failure. DigCrawl ignores `yt-dlp` user/system configuration and disables its proxy to avoid inheriting external network or command settings. Install `yt-dlp` on `PATH` first, for example with `brew install yt-dlp` on macOS.

Instagram supports:

- `instagram_post` / `post`: monitor a fixed image/text post such as `https://www.instagram.com/p/.../`, reusing `instagram.content.read`.
- `instagram_reel` / `reel`: monitor a fixed Reels URL such as `https://www.instagram.com/reel/.../`; content type is normalized to `video`.
- `instagram_user` / `user` / `profile`: discover `/p/...` and `/reel/...` links on profile lists, deduplicating by `instagram:<shortcode>`. Default `auth_mode=public` does not read local Cookies. Session access requires both global `Social.Instagram.AuthMode=local_session` and source option `auth_mode=local_session`.

Douyin supports:

- `douyin_video` / `video`: monitor a fixed work URL such as `https://www.douyin.com/video/...`, `https://www.iesdouyin.com/share/video/...`, or a profile link with `modal_id`, deduplicating by `douyin:<work_id>`.
- `douyin_user` / `user`: discover work links on profile/list pages using static links, hydration URLs, and browser fallback metadata. Narrow the scope with `wait_selector`, `include_pattern`, and `limit`.

Xiaohongshu supports:

- `xiaohongshu_note` / `xhs_note`: monitor a fixed note URL such as `https://www.xiaohongshu.com/explore/...` or `/discovery/item/...`, deduplicating by `xiaohongshu:<note_id>`.
- `xiaohongshu_user` / `xhs_user`: discover note links on profile/list pages, normalizing to `/explore/<note_id>` and deduplicating by stable note ID. For signed-in use, run `digcrawl browser login --platform xiaohongshu` first.

Bilibili supports:

- `bilibili_video` / `bili_video`: monitor a fixed video URL such as `https://www.bilibili.com/video/BV...`, `/video/av...`, or a `b23.tv` short link, reusing `bilibili.video.read`.
- `bilibili_user` / `bili_user`: discover video links on space/list pages, currently extracting `/video/BV...` and `/video/av...` from static links, hydration URLs, and browser fallback metadata.

Weibo supports:

- `weibo_post`: monitor a fixed post URL such as `https://m.weibo.cn/detail/...`, `/status/...`, or `https://weibo.com/{uid}/{mid}`, reusing `weibo.post.read`.
- `weibo_user`: discover post-detail links on profile/list pages; `/u/...` and `/profile/...` user pages are not treated as posts.

Kuaishou supports:

- `kuaishou_video` / `ks_video`: monitor a fixed video URL such as `https://www.kuaishou.com/short-video/...`, `/photo/...`, or a `v.kuaishou.com` short link. Reuses `kuaishou.video.read`, preferring Kuaishou Web GraphQL `visionVideoDetail`. Risk controls still triggered after local Cookies are loaded return `CAPTCHA_REQUIRED`.
- `kuaishou_user` / `ks_user`: discover `/short-video/...` and `/photo/...` links on profile/list pages, preferring Kuaishou Web GraphQL `visionProfilePhotoList`, then HTML/browser link discovery.

Zhihu supports:

- `zhihu_answer`: monitor a fixed answer URL such as `https://www.zhihu.com/question/.../answer/...`.
- `zhihu_article`: monitor a fixed column article such as `https://zhuanlan.zhihu.com/p/...`.
- `zhihu_zvideo`: monitor a fixed video URL such as `https://www.zhihu.com/zvideo/...`.
- `zhihu_user`: discover answer, column article, and video links on profile/list pages.

WeChat Official Accounts support:

- `wechat_article`: monitor a fixed article URL such as `https://mp.weixin.qq.com/s/...`.
- `wechat_list`: discover `mp.weixin.qq.com/s...` links on article lists or ordinary pages, deduplicating by normalized article URL hash. Lists requiring login or dynamic loading can use browser fallback and `wait_selector`.

Ordinary web pages support:

- `web_list`: extract same-domain links from list/home pages, using normalized URLs as `content_key` and removing common tracking parameters such as `utm_*` and `spm`. `include_pattern`, `exclude_pattern`, `limit`, `wait_selector`, and `content_selector` control URL scope, count, and browser fallback extraction timing.

`Summary` fields:

| Field | Description |
|------|------|
| `Enabled` | Enable summarization; when disabled, fall back to rule-based summaries |
| `Provider` | `extractive`、`llm_direct`、`digflow_runtime`、`digflow_http` |
| `TimeoutSec` | Summary request timeout |
| `MaxInputChars` | Total input limit sent to the summary provider |
| `FallbackProvider` | Fallback on summary failure; default `extractive` |
| `InputBudget` | Separate budgets for body, ASR, OCR, and metadata |
| `DigFlow` | Embedded DigFlow Runtime or HTTP-compatible integration settings |
| `LLM` | Direct OpenAI-compatible chat completion settings |

`Notifications.Feishu` fields:

| Field | Description |
|------|------|
| `Enabled` | Enable Feishu webhook notifications |
| `DefaultChannel` | Default notification channel ID |
| `Channels` | Webhook channel list |
| `WebhookURL` | Feishu bot webhook; supports `${ENV}` |
| `Secret` | Feishu signing secret; supports `${ENV}` |
| `MaxMessageChars` | Maximum notification text length; longer messages are truncated |

Example configuration:

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
    Name: Reddit AI community monitoring
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
    Name: Fixed Toutiao article monitoring
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
    Name: Fixed Elon Musk X post monitoring
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

DigFlow HTTP compatibility mode remains available for deployments with a separate DigFlow service:

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

Embedded DigFlow Runtime additionally requires:

- `config/digflow-gorm.yml`: DigFlow platform database connection; fixed `df_` table prefix.
- `config/eino.yml`: DigFlow / DigEino domain table names, context, skills, chat history, and execution state.
- `agents/watchlist/agents.yml`: host-side agent manifest.
- `agents/watchlist/summary-agent.yml`: Watchlist summary agent.
- `config/runtime-env.yml`: local runtime variable injection; see `config/runtime-env.example.yml`. Enabled explicitly by `RuntimeEnv.ConfigPath` in `config/config.yaml`.

The default build excludes private DigFlow runtime dependencies; `digflow_runtime` uses a stub and falls back to extractive summaries. DigFlow is at `v1.1.4`. For actual embedding, configure SSH/GOPRIVATE and build or test with `-tags digflow_embedded`. The first embedded build fetches the runtime dependency tree; set a writable Go cache explicitly to avoid long silent waits caused by local permissions or a cold cache. The DigCrawl host expands `${...}` variables from `config/digflow-gorm.yml` / `config/eino.yml` into temporary configuration files at runtime. Missing variables fail explicitly instead of becoming empty strings.

```bash
GOPRIVATE=github.com/originaleric/* \
GONOSUMDB=github.com/originaleric/* \
GOPATH=/private/tmp/digcrawl-gopath \
GOMODCACHE=/private/tmp/digcrawl-gomodcache \
GOCACHE=/private/tmp/digcrawl-go-cache \
TMPDIR=/private/tmp \
go test -tags digflow_embedded ./internal/summary
```

Database DDL is aligned with DigFlow `v1.1.4`. `pkg/digflow.NewRuntime`, `Invoke`, and `Stream` remain this repository's embedded Runtime interfaces; DigCrawl still uses `pkg/digflow` directly and requires no DTO import migration. SQLite is recommended for local single-machine use; remote MySQL deployments use MySQL DDL:

- Full initialization of a new MySQL database: `sqls/full/digcrawl_full_sql_ddl.sql`
- Full initialization of a new SQLite database: `sqls/full/digcrawl_full_sqlite_ddl.sql`, including Collect, Watchlist/Tasks runtime, and embedded DigFlow `df_*` tables
- Initialize only DigFlow runtime tables in a new database: `sqls/full/digflow_embedded_runtime_full_ddl.sql`
- Root-level compatibility full DDL: `sqls/digcrawl_full_sql_ddl.sql`, `sqls/2026-07-09_DigFlow内嵌Runtime初始化DDL.sql`
- Additional migration for databases using older DigFlow DDL: `sqls/2026-07-09_DigFlow最新Runtime增量DDL.sql`
- Additional migration for databases using older DigCrawl DDL to enable general Tasks scheduling: `sqls/2026-07-16_通用Task调度RuntimeDDL.sql`

DigCrawl's local SQLite driver uses `github.com/mattn/go-sqlite3`, requiring `CGO_ENABLED=1` and a local C compiler; macOS can provide `cc` through Xcode Command Line Tools. DigFlow's SQLite connection remains controlled by its own DigFlow / GORM settings.

The latest incremental migrations include:

- LoopRuntime persistence tables: `df_loop_instance`, `df_loop_iteration`, `df_loop_active`
- ResourceAsset storage-key migration: make `df_resource_asset.storage_key` nullable and create `uidx_resource_asset_storage_key`

A fully local embedded DigFlow runtime requires changes at all three entry points; changing only `config/digflow-gorm.yml` is insufficient:

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

`config/digflow-gorm.yml` controls DigFlow platform tables. Common persistent `ChatHistoryStore` implementations in `config/digflow.yml` are MySQL/PGSQL/sql_mapping; disable it or use memory for local-only mode. Disable `ReuseGlobalMySQL` in `config/eino.yml`'s `context.ChatHistory` too, or it still tries to reuse MySQL. If local long-term memory indexing is later enabled, set `context.LongTerm.Store` to `local_sqlite` and ensure `sql_lite.Path` in `config/digflow.yml` is nonempty. DigCrawl's own Watchlist runtime uses `Watchlist.Persistence.SQLitePath`; the two can share a SQLite file or use separate files according to runtime boundaries.

Table prefix conventions:

- DigCrawl business tables: `dc_*`, identical for MySQL and SQLite
- DigFlow runtime tables: `df_*`

### Providers (built-in provider switches)

| Field | Description |
|------|------|
| `PlatformEnabled` | WeChat / Xiaohongshu / Douyin / X platform tools |
| `BrowserEnabled` | `browser.browse` / `snapshot` / `action` |
| `ASREnabled` | `asr.transcribe` video/audio transcription tool |
| `ExampleEnabled` | `gateway.ping`、`file.read` |
| `StubEnabled` | Example third-party provider for development |

---

## 3. CLI Overview

```text
digcrawl gateway [flags]       # HTTP tool gateway
digcrawl collector [flags]     # Outbound WebSocket connection to a host
digcrawl connect [flags]       # Save host configuration and start Collector
digcrawl login [flags]         # Save Token / host URL only
digcrawl mcp [flags]           # MCP stdio（Cursor / Claude Desktop）
digcrawl init --profile skill  # Create a user Skill profile (--force required to replace an existing file)
digcrawl config validate --config <profile> # Validate profile YAML, paths, enabled capabilities, and boundaries
digcrawl doctor --config <profile> # Check profile, artifacts, browser, ffmpeg, and MCP initialization
digcrawl doctor --diagnostic-id <ID> # Query the safe local summary for a returned diagnostic ID
digcrawl stdio [flags]         # Line-delimited JSON protocol
digcrawl dev-host [flags]      # Local reference host (development only)
digcrawl collect <url>         # Collect a single URL
digcrawl collect inbox         # Process the inbox
digcrawl browser login         # Interactive/platform QR login and Cookie storage
digcrawl browser check         # Check platform sessions, optionally with trial reads
digcrawl asr setup --yes        # Select project/user Skill configuration, install isolated faster-whisper, and verify the selected local model
digcrawl asr check              # Check local ASR offline in the same scope as setup/transcribe
digcrawl asr transcribe <file> --timeout 1200 # Extract audio with ffmpeg and transcribe any local video/audio
digcrawl video <local-file-or-direct-url> [--mode auto|asr|ocr|both] # Analyze a local video or directly downloadable video URL
digcrawl watchlist run         # Run Watchlist monitoring, summarization, and notifications
digcrawl watchlist serve       # Schedule due Tasks / Watchlist items with their respective intervals
scripts/digcrawl-watchlist-service.sh install|rebuild|rebuild-restart|start|stop|restart|status|logs|uninstall  # macOS/Linux
.\\scripts\\digcrawl-watchlist-service.ps1 install|start|stop|restart|status|logs|uninstall                    # Windows
digcollect <url>              # Lightweight entry point, equivalent to digcrawl collect <url>
digcrawl help
```

Most subcommands accept `--config <path>` to select a configuration file.

### Direct video analysis

`digcrawl video` accepts an explicitly provided local video path or directly downloadable `http`/`https` media URL. Read platform page URLs through their platform tools. `--mode auto` runs only enabled ASR and video-frame OCR processors; choose `asr`, `ocr`, or `both` explicitly if needed. Output defaults to JSON; change it with `--format text`, `--format md`, and `--out <file>`.

```bash
digcrawl video ./meeting.mp4
digcrawl video "https://cdn.example.com/meeting.mp4" --mode both --format md --out meeting.md
digcrawl video "https://cdn.example.com/meeting.mp4" --keep ./meeting.mp4
```

Direct downloads use a private temporary directory, bounded by `--max-bytes` (default 512 MiB) and `--download-timeout` (default 300 seconds), and are deleted after processing. Use `--keep <path>` only when the user explicitly requests retaining this direct download. It does not read or change `Skill.Media.RetainDownloadedVideo`. Without `--config`, enabled project root ASR/OCR processors take priority; disabled processors can be supplied by standard user Skill profile `Skill.ASR` / `Skill.OCR`. Explicit `--config` always uses only the selected profile.

### Watchlist service scripts

`scripts/digcrawl-watchlist-service.sh` manages `digcrawl watchlist serve` as a user service for local or small-server deployments. Use the corresponding PowerShell script on Windows.

- macOS: generates `~/Library/LaunchAgents/com.digcrawl.watchlist.plist`, copies the startup script privately to `~/Library/Application Support/DigCrawl/bin/`, and manages it with `launchctl`. `start` / `restart` migrate old plists and refresh the copy so launchd does not reject project scripts in protected directories such as `Documents`.
- Linux: generates `~/.config/systemd/user/digcrawl-watchlist.service` and manages it with `systemctl --user`.
- Windows: creates the current user's `DigCrawl Watchlist` Task Scheduler task and manages it through PowerShell.
- Logs: macOS writes `watchlist.out.log` / `watchlist.err.log` under `DIGCRAWL_LOG_DIR`; Linux defaults to the user journal, accessible through the script's `logs` command.
- On macOS/Linux, `install`, `install-cli`, `build`, `rebuild`, and `rebuild-restart` create a `${DIGCRAWL_CLI_BIN_DIR:-~/.local/bin}/digcrawl` symlink to the service binary and add the directory to the login shell's startup configuration. Open a new terminal or source that configuration before using `digcrawl` directly. On Windows, set `DIGCRAWL_BIN` to the release `digcrawl.exe` path, or place it in the project root.
- On a Watchlist session-expiry notification, run `<actual-service-executable> browser login --config <actual-service-config> --platform <platform>` as provided. The command uses the service's current executable and loaded configuration to avoid writing Cookies to another profile. When configuration was not loaded from a file, `--config` is omitted to preserve default resolution.

Common commands:

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

Windows (PowerShell):

```powershell
$env:DIGCRAWL_CONFIG = "$PWD\config\config.yaml"
.\scripts\digcrawl-watchlist-service.ps1 install
.\scripts\digcrawl-watchlist-service.ps1 status
```

Optional environment variables:

| Variable | Description |
|------|------|
| `DIGCRAWL_BIN` | `digcrawl` binary path; prefers repository-root `./digcrawl` by default |
| `DIGCRAWL_CONFIG` | Configuration path; default `config/config.yaml` |
| `DIGCRAWL_WORKDIR` | Service working directory; default repository root |
| `DIGCRAWL_LOG_DIR` | Log directory; default `logs/` |
| `DIGCRAWL_SERVICE_NAME` | Linux systemd service name; default `digcrawl-watchlist` |
| `DIGCRAWL_SERVICE_LABEL` | macOS launchd label; default `com.digcrawl.watchlist` |
| `DIGCRAWL_AUTO_BUILD` | Let the script build the Watchlist binary automatically; default `1` |
| `DIGCRAWL_BUILD_TAGS` | Automatic build tags; default `digflow_embedded` |
| `DIGCRAWL_BUILD_GOCACHE` | Automatic Go build cache; default `/private/tmp/digcrawl-gocache` |
| `DIGCRAWL_BUILD_GOMODCACHE` | Automatic Go module cache; default `.tmp/go-mod-watchlist` |

`install` succeeds only after the service is registered, started, and passes its running-state check; no extra `start` is needed. `run-foreground` runs an existing binary only. After code or DigFlow dependency updates, use `./scripts/digcrawl-watchlist-service.sh rebuild` to rebuild, or `./scripts/digcrawl-watchlist-service.sh rebuild-restart` to rebuild and restart. The script first reads `DIGCRAWL_BIN` from the installed launchd plist / systemd unit and rebuilds at the binary path actually used by the service. The compatibility `build` command uses the same path resolution.

With the `digflow_runtime` summary agent, the service no longer requires launchd/systemd to inject business secrets. Set `RuntimeEnv.ConfigPath: config/runtime-env.yml` in the main configuration, then enable `RuntimeEnv.Enabled: true` in the locally ignored `config/runtime-env.yml` and supply `DIGCRAWL_MYSQL_HOST`, `DIGCRAWL_MYSQL_DATABASE`, `DIGCRAWL_MYSQL_USER`, `DIGCRAWL_MYSQL_PASSWORD`, and `DIGFLOW_SUMMARY_API_KEY`. Only `config/runtime-env.example.yml` is committed.

---

## 4. HTTP Gateway (gateway)

Start a local tool gateway; it listens on localhost by default. To expose it to a LAN or the internet, explicitly configure `AuthToken`, `AllowedTools`, and `AllowedDomains`, and assess the exposure.

```bash
# Use the repository's example configuration
digcrawl gateway --config config/config.yaml

# Override the listen address
digcrawl gateway --config config/config.yaml --addr 127.0.0.1:8787
```

Verify:

```bash
curl -s http://127.0.0.1:8787/health
curl -s http://127.0.0.1:8787/manifest | jq .
```

With `AuthToken` configured:

```bash
curl -s -H "Authorization: Bearer <token>" http://127.0.0.1:8787/manifest
```

### Tool call example

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

Tools marked `requires_user_approval: true` in the manifest, such as `browser.action` and `file.read`, require `"approved": true` in `ToolCall` by default. Otherwise, the runtime returns `USER_APPROVAL_REQUIRED` to prevent hosts or HTTP clients from directly executing high-risk interactions.

---

## 5. Connect to a Cloud Host (collector / connect / login)

Typical deployment: **DigCrawl Collector runs on the user's machine** and makes an **outbound WebSocket connection** to your application host. The host sends `tool_call` messages; the Collector executes locally and returns results.

### Option A: connect in one step (recommended)

```bash
digcrawl connect --server https://your-host.example.com --token <collector-token>
```

- Writes `ServerURL` and `Token` to `~/.digcrawl/config.yaml`
- Starts `collector` with that configuration immediately

Save configuration without starting:

```bash
digcrawl connect --server https://your-host.example.com --token <token> --save-only
```

### Option B: configure in separate steps

```bash
# Save the token first (optionally with the server URL)
digcrawl login --token <collector-token>
digcrawl login --server https://your-host.example.com --token <token>

# Then start Collector
digcrawl collector --config ~/.digcrawl/config.yaml
```

### Common Collector options

```bash
digcrawl collector \
  --config ~/.digcrawl/config.yaml \
  --server https://your-host.example.com \
  --token <token> \
  --instance-id my-laptop \
  --pull-interval 5
```

| Option | Description |
|------|------|
| `--server` | Override `Collector.ServerURL` |
| `--token` | Override `Collector.Token` |
| `--instance-id` | Override `Collector.InstanceID` |
| `--pull-interval` | Polling interval in seconds; `-1` uses configuration |

### Development reference host

Implement the production WebSocket host in **your own project**. This repository provides only a minimal reference:

```bash
digcrawl dev-host --addr :8790 --token dev
```

Then, in another terminal:

```bash
digcrawl collector --server http://127.0.0.1:8790 --token dev
```

---

## 6. IDE Integration (MCP)

Register DigCrawl as a stdio service in Codex, Claude Code, Cursor, WorkBuddy, or another MCP client. The release installer creates a missing Skill profile, retains the complete portable Skill bundle, and repairs the effective Codex `mcp_servers.digcrawl` configuration. When Claude Code is detected, its Skill is installed too. For manually extracted releases or `--skip-agent-setup`, first run `digcrawl init --profile skill`, then check the local profile with `digcrawl config validate --config <profile>` and `digcrawl doctor --config <profile>`. On a new installation, `digcrawl mcp` without `--config` discovers the user `digcrawl-skill.yaml`; explicitly passing its absolute path in host configuration is still recommended:

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

Once started, the IDE agent calls registered local tools through MCP. The MCP entry point connects on demand to the same profile's Bridge, which exclusively owns the Runtime and browser. HTTP `gateway` remains a separately started service.

Except for Codex, the installer does not guess the host's MCP configuration file. Copy or link the complete `skills/digcrawl/` directory to the host's Skill location and register the `command` and `args` above under the [generic stdio launch contract](./.agents/skills/digcrawl/mcp/README.md). Do not copy only `SKILL.md`. Codex registration explicitly sets `--idle-timeout 0`. Other hosts can use the default configuration above and choose idle reclamation according to their connection-management capabilities.

`digcrawl mcp` manages the local Bridge separately from host stdio proxies. At most one Bridge exists per profile and runs until stopped or failed. Initialized proxies follow the host pipe by default instead of exiting after 15 minutes without tool calls. Uninitialized pipes default to one-minute idle reclamation; explicit `--idle-timeout 0` disables both initialization and session idle reclamation. A positive value sets tool inactivity timeouts for initialized sessions while retaining the initialization guard. Protocol ping does not extend tool inactivity deadlines; in-flight calls are protected. An explicit legacy `15m` still applies. See [connection lifetime and OCR troubleshooting](#mcp-connection-lifetime-and-ocr-troubleshooting) for migration, upgrade reconnection, and OCR diagnostics.

### Minimal Agent Skill profile

For the repository's `digcrawl` Skill, create a minimal user profile with `digcrawl init --profile skill`, or maintain a copy of `config/digcrawl-skill.example.yaml`. `init` generates OS-native absolute `Skill.Media.Dir`, `VideoDownload.Dir`, and `ImageDownload.Dir` paths beside the profile. Host configuration stores only absolute binary and profile paths. The host agent should interpret image text and page visuals from MCP screenshot attachments; `media` URLs are source references. The host also summarizes Markdown, image observations, and `asr_text`. Video transcription uses local `Skill.ASR`; enable `Skill.Media.DownloadMedia`, a media directory, and ASR dependencies only when explicitly requested. Downloaded MP4s exist only for the current interpretation by default. For an explicit request to save supported platform videos, the host agent calls `skill.media.configure` with `video_download_providers`; named entries merge with policy, `false` disables one platform, `{}` clears policy, and `media_dir` accepts only absolute paths. Image retention uses the same tool's `image_download_providers`, `image_download_dir`, `image_download_max_bytes`, `image_download_max_items`, and `image_download_timeout_sec`; it also requires an explicit request and does not enable OCR. Do not edit YAML or use legacy `retain_downloaded_video` / `max_bytes` to enable new platforms; those only support old YouTube profiles without `VideoDownload`. Retention status is in `platform_metadata.video_download_status`; saved paths are relative to `VideoDownload.Dir`, and the host must not request or display absolute paths. Configuration updates use in-process admission plus a cross-process profile lock. Retry later on retryable `SKILL_PROFILE_BUSY` while another update is running. `skill.asr.enable` checks ffmpeg and the current provider before writing its two opt-in switches; missing dependencies leave configuration unchanged. The default Skill profile has no OCR or LLM API Key and does not use DigFlow.

---

## 7. stdio JSON Protocol

For hosts or scripts integrating over pipes:

```bash
digcrawl stdio --config config/config.yaml
```

stdin/stdout use line-delimited JSON with message types matching [`protocol/`](protocol/), such as manifest queries, tool_call, and tool_result. This supports custom orchestrators.

---

## 8. Local Content Collection (collect)

Collect retrieves links and exports them locally **without a cloud host**, for knowledge bases, archiving, and batch processing.

### Supported content sources

| Type | Detection | Underlying tool |
|------|----------|----------|
| WeChat Official Accounts | `mp.weixin.qq.com`, etc. | `wechat.article.read` |
| Xiaohongshu | `xiaohongshu.com`, `xhslink.com`, etc. | `xiaohongshu.note.read` |
| Douyin | `douyin.com`, etc. | `douyin.video.read` |
| Toutiao articles | `toutiao.com`, `m.toutiao.com`, etc. | `toutiao.article.read` |
| Toutiao videos | `toutiao.com/video/...` | `toutiao.video.read` |
| X / Twitter | `x.com`, `twitter.com`, etc. | `x.post.read` |
| Bilibili | `bilibili.com`, `b23.tv`, etc. | `bilibili.video.read` |
| Weibo | `weibo.com`, `m.weibo.cn`, etc. | `weibo.post.read` |
| Kuaishou | `kuaishou.com`, `v.kuaishou.com`, etc. | `kuaishou.video.read` |
| Zhihu | `zhihu.com`, `zhuanlan.zhihu.com`, etc. | `zhihu.content.read` |
| YouTube videos / Shorts | `youtube.com/watch`, `youtube.com/shorts`, `youtu.be` | `youtube.video.read` |
| RSS / Atom | `.rss`、`.atom`、`.xml`、`/feed`、`/rss` | `feed.read` |
| Any web page | Other `http(s)` URLs | `browser.browse` (generic web page) |

Xiaohongshu, Douyin, and Toutiao use an **API-first, browser-fallback** strategy: try page-runtime `fetch(credentials:"include")`, then hydration/meta/DOM extraction. Bilibili and Weibo details prefer public HTTP APIs with browser fallback. X uses Tweet lookup API, then public oEmbed, then browser fallback. Reddit uses OAuth API, then RSS, then browser fallback. JSON `platform_metadata` includes diagnostics such as `read_channel`, `extraction_source`, `api_http_status`, and `api_error_code`. `LOGIN_REQUIRED`, `CAPTCHA_REQUIRED`, `RATE_LIMITED`, deleted content, and private content return structured errors.

### Single URL

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

### Platform sign-in and health checks

Some platforms return `LOGIN_REQUIRED` when signed out or Cookies expire. Use DigCrawl's dedicated visible browser to sign in and save Cookies; it does not read your everyday browser profile. Explicit `--config` selects the profile for `browser login` and `browser check`. Without it, the current project's `config/config.yaml` takes priority, falling back to the standard user Skill profile only when the project file is absent. To restore an MCP session, pass the same `--config <profile>` used by MCP to avoid writing Cookies to another profile. Run `browser login` directly in an interactive terminal: it waits for Enter to save Cookies after sign-in. `nohup`, launchd, or redirected stdin are rejected before browser startup. Only one DigCrawl login per platform may run at a time. A live local process owning SingletonLock causes the command to direct you to the original terminal, never take over or close that window. Stale Chromium locks are removed only after the owner is confirmed exited. After saving or failure, DigCrawl closes the Chrome launched by this command and reports failure if the profile was not released. The command prints the login URL and Cookie directory:

```bash
digcrawl browser login --platform xiaohongshu
digcrawl browser login --platform douyin
digcrawl browser login --platform toutiao
digcrawl browser login --platform threads
digcrawl browser login --platform instagram
digcrawl browser login --platform youtube
```

YouTube sign-in uses a dedicated profile in Chrome for Testing without CDP or script injection during login. Click "Sign in", complete Google authentication, return to YouTube, then confirm saving. DigCrawl closes its Chrome and waits for profile release before starting background session synchronization. Windows users must close every window of that dedicated Chrome before confirming. `youtube.video.read` reuses the YouTube session in that configuration directory by default. It does not copy your everyday browser session or write Google-domain Cookies to YouTube Cookie storage. Login does not guarantee removal of platform checks; preserve `LOGIN_REQUIRED` or `CAPTCHA_REQUIRED` if still returned. This session is for browser reads; `yt-dlp` downloads do not inherit it automatically.

After signing in, check the session and optionally trial-read a URL:

`browser check` uses a temporary isolated browser profile and the same saved Cookie store. It can run while the MCP Bridge owns its persistent profile; it verifies browser release before removing the temporary profile.

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

| Option | Description |
|------|------|
| `--config` | Configuration file path |
| `--out` | Override `Collect.OutputDir` |
| `--format` | Comma-separated `md`, `html`, `json` |
| `--file` | Text file containing one URL per line |

Exported article filenames remove invalid characters and truncate long titles on UTF-8 boundaries. `--format md,json,html` retains Markdown, text fields required by JSON, and HTML together.

### Batch file

```bash
digcrawl collect --file urls.txt --format md,json --out ./collect-output
```

### Inbox

Place links in `collect-inbox/` (the configurable default):

- Accepts text files such as `.url` and `.txt`, one URL per line
- Successful files move to `processed/`
- Failed records move to `failed/`

```bash
# Process once
digcrawl collect inbox

# Poll continuously (Ctrl+C to stop)
digcrawl collect inbox --watch
```

Batches are throttled by `Collect.RateLimitMs` to avoid excessive requests.
`Collect.PlatformRateLimits` applies finer intervals based on URL-detected platforms, with ±20% jitter.

### Queries and migration (SQLite)

With `PersistMode` set to `sqlite` or `both`:

```bash
digcrawl collect list
digcrawl collect list --q wechat --limit 50
digcrawl collect show <collect-id>
digcrawl collect show "https://mp.weixin.qq.com/s/..."
digcrawl collect migrate-index   # Import existing index.json into collect.db
```

### Output directory structure

```text
collect-output/
  index.json                 # PersistMode=index|both
  2026-05-21/
    article-title.md
    article-title.html
    article-title.json
    article-title.assets/       # Media and screenshots
      media.json
      001.jpg

storage/digcrawl/
  collect.db                 # PersistMode=sqlite|both

collect-inbox/
  pending.url                # Pending (example filename)
  processed/                 # Successful files
  failed/                    # Failed records
```

See [`collect/README.md`](collect/README.md) for implementation details.

---

## 9. Built-in Tools

Inspect complete schemas through `GET /manifest` or MCP. Common tool names are listed below.

### Gateway examples (example)

| Tool | Description |
|--------|------|
| `gateway.ping` | Health check returning runtime information |
| `file.read` | Read local files under `AllowedReadPaths` |

### Platform content (platform)

| Tool | Description |
|--------|------|
| `wechat.article.read` | WeChat Official Account article |
| `xiaohongshu.note.read` | Xiaohongshu note |
| `douyin.video.read` | Douyin video page |
| `toutiao.article.read` | Toutiao article |
| `toutiao.video.read` | Toutiao video |
| `x.post.read` | X post |
| `bilibili.video.read` | Bilibili video page |
| `weibo.post.read` | Weibo detail page |
| `kuaishou.video.read` | Kuaishou video page |
| `zhihu.content.read` | Zhihu answer / column article / video |
| `youtube.video.read` | YouTube video / Shorts and public subtitles |

### Browser (browser)

| Tool | Description |
|--------|------|
| `browser.browse` | Open a URL and extract body / screenshot / summary |
| `browser.snapshot` | Accessibility tree snapshot with interactive elements |
| `browser.action` | Click, type, etc.; high risk, user confirmation recommended |

### Speech transcription (asr)

| Tool | Description |
|--------|------|
| `asr.transcribe` | Read video/audio under `AllowedReadPaths`, extract audio with ffmpeg, and transcribe |

### Development stub (requires `StubEnabled: true`)

Placeholder tools for verifying provider loading. **Do not enable in production.**

---

## 10. HTTP API Reference

Base URL: `http://<ListenAddr>` (default `http://127.0.0.1:8787`)

| Method | Path | Description |
|------|------|------|
| `GET` | `/health` | Health check |
| `GET` | `/manifest` | Tool manifest (ToolManifest) |
| `POST` | `/tools/call` | Execute a tool; body is `ToolCall` JSON |
| `GET` | `/artifacts/{id}` | Download an artifact such as a screenshot |

Authentication when `Gateway.AuthToken` is nonempty:

- Header：`Authorization: Bearer <token>`
- Or: `X-DigCrawl-Token: <token>`

---

## 11. Host Integration (Go SDK)

If your application service must **initiate calls** to the user's Gateway instead of receiving an outbound Collector connection, use the host-side HTTP client:

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

**Integration modes:**

| Mode | Connection initiator | Use case |
|------|------------|----------|
| **Collector** | Local machine connects to host WebSocket | SaaS task dispatch without requiring a public user IP |
| **HTTP Client** | Host connects to the user's Gateway URL | User-exposed port or tunnel |

To implement a Collector WebSocket host, reuse `transport/host`:

```go
srv := host.NewServer("collector-token", "/digcrawl/v1/collector/ws")
http.ListenAndServe(":8790", srv.Handler())
```

Applications can use `srv.Enqueue(instanceID, call)` for pull mode or `srv.Push(instanceID, call)` for immediate task push. `digcrawl dev-host` remains a local debugging reference.

---

## 12. Custom Providers

Implement [`provider.Provider`](provider/provider.go):

```go
type Provider interface {
    Name() string
    Register(ctx context.Context, reg *registry.Registry) error
}
```

Optionally implement `HealthReporter` for pre-registration health checks or `ToolManifester` to list tool descriptors only.

In custom startup code:

```go
rt, err := runtime.NewRuntime(cfg) // Or assemble a Runtime after creating your own registry
```

Built-in providers load by configuration through `runtime.DefaultProviders`. To add custom providers, inject them through `RegistryOptions.Providers` in `runtime/bootstrap.go`.

---

## 13. Architecture and Directories

```text
protocol/           Protocol types and JSON wire format
runtime/            Registry, policy, audit, artifacts, bootstrap
transport/          http、collector、mcp、stdio、devhost
client/             Host-side HTTP SDK
collect/            Detection, execution, export, index, inbox
provider/           platform、browser、example、stub
internal/platform/  Platform-specific page parsing
internal/browser/   go-rod local browser
```

Data and state default to project-local directories:

- `storage/digcrawl/artifacts` — Artifact files
- `storage/digcrawl/browser_cookies` — Browser Cookies
- `collect-output/`, `collect-inbox/` — Collect output and inbox

---

## 14. FAQ

### Browser tool reports a domain outside the allowlist

Check the tool and actual redirected URL first. Generic `browser.browse` obeys call policy and underlying `Gateway.AllowedDomains`; standalone `browser.snapshot` also retains `LocalBrowser.AllowedDomains`. Platform providers validate input and confirmed final sources using call policy, configuration, then platform-default domain policy. Browser paths also obey underlying browser constraints. Feed redirects, Reddit RSS redirects, and Bilibili browser-media merging likewise check final sources. Adjust the relevant configuration only when the target domain is needed. Canonical links, outbound body links, and media CDNs are distinct from page sources; domain policy is not network isolation.

### Browser timeouts or repeated failures

MCP `error_details` for `BROWSER_*` errors include `stage`, `operation`, `reason`, `handled`, `retry_count`, `recovery_status`, `next_actions`, and `diagnostic_id` as applicable. A tool call performs at most one browser-failure retry; platform fallback shares the original budget. Collectors and agents should not automatically replay the entire tool after `handled: true`.

- `send_queue`: wait for the current read to finish, then make a new request. This queue failure does not close a healthy connection.
- `cancelled` / `budget_exhausted`: the call has ended; start again only after confirming the read is still needed.
- `page_changed`: navigation or reload occurred during reading and mixed results were discarded. Wait for the page to stabilize before reading again.
- Profile occupied: finish or end the session using that dedicated profile; do not close unrelated Chrome instances.
- `LOCAL_BRIDGE_UNAVAILABLE` or build mismatch: close the old DigCrawl MCP connection, restart the actual profile's Bridge with the newly installed binary, then reconnect the host. Interrupted tools may have executed; do not immediately replay operations with side effects.
- External CDP page cleanup unconfirmed: clean up leftover pages. If that cannot be confirmed, restart the external browser, then DigCrawl. DigCrawl does not terminate external browsers for the user.

Troubleshoot with the actual MCP profile's absolute path. Diagnostic ID summaries contain safe classifications only, without Cookies, raw browser exceptions, or page content:

```bash
digcrawl doctor --config /absolute/path/to/digcrawl-skill.yaml
digcrawl doctor --diagnostic-id <returned-ID>
# After an upgrade, or when next_actions requires restart, close the old MCP connection first.
digcrawl bridge restart --config /absolute/path/to/digcrawl-skill.yaml
```

MCP progress notifications before/after operations require the host to supply a progress token with the request. Available tools are determined by current `tools/list`. The default Skill obtains snapshots through `browser.browse` with `mode: "snapshot"`; standalone `browser.snapshot` is not automatically exposed.

### Headless Chrome / Chromium startup failure

Run `digcrawl browser runtime` to check the pinned dependency and integrity, then `digcrawl browser install` for missing dependencies. For a damaged version, first migrate/stop its owners, run `digcrawl browser install --repair` to preserve and replace the damaged directory. Never substitute daily Chrome or delete its locks/data. Use the migration command above for legacy executable overrides.

### Incomplete generic web content in Collect

Dynamic sites require adequate page loading and selectors for `browser.browse`. Direct Gateway calls can specify `wait_selector` and `content_selector` in tool input. Collect uses default full mode.

### Collector cannot connect to the host

Check that `ServerURL` includes a scheme (`https://`), firewall access, the host WebSocket path matches `WSPath`, and the Token matches.

### Relationship to DigEino

[DigEino](https://github.com/originaleric/digeino) implements tool providers for the Eino / DigFlow ecosystem. DigCrawl is **decoupled** from it and includes Collect and platform/browser tools. You can integrate DigEino as a provider in a custom runtime or use the standalone DigCrawl distribution.

---

## 15. Related Links

- Repository: <https://github.com/originaleric/digcrawl>
- Example configuration: [`config/config.yaml`](config/config.yaml)
- Collect module: [`collect/README.md`](collect/README.md)

---

## 16. Local Retrieval Test Plan

Test in this order: basic web pages, Toutiao, signed-in platforms, then batches. This helps distinguish export, URL normalization, session, API-channel, and browser-fallback failures.

### 16.1 Build and basic checks

```bash
GOROOT=/Users/dig/.gvm/gos/go1.26.1 /Users/dig/.gvm/gos/go1.26.1/bin/go build -o digcrawl ./cmd/digcrawl
./digcrawl --help
```

### 16.2 Generic web fallback

```bash
./digcrawl collect "https://example.com" --format md,json --out ./collect-output-test
```

Expected: md/json files are generated, confirming the basic collect, browser, and export path.

### 16.3 Toutiao articles

```bash
./digcrawl collect "https://www.toutiao.com/article/YOUR_ARTICLE_ID/" \
  --format md,json --out ./collect-output-test
```

Check the JSON:

- `platform` is `toutiao`
- `content_type` is `article`
- `platform_metadata.read_channel` is `api` or `browser`
- `platform_metadata.extraction_source` is populated
- `title` / `text` are nonempty

### 16.4 Trial reads after sign-in

These commands use current project configuration. To verify an MCP session, pass the same `--config <profile>` used by MCP to both login and check.

```bash
./digcrawl browser login --platform xiaohongshu
./digcrawl browser login --platform douyin
./digcrawl browser login --platform toutiao
./digcrawl browser login --platform threads
./digcrawl browser login --platform instagram
./digcrawl browser login --platform youtube

./digcrawl browser check --platform toutiao \
  --url "https://www.toutiao.com/article/YOUR_ARTICLE_ID/" --trial
./digcrawl browser check --platform threads \
  --url "https://www.threads.com/@zuck/post/YOUR_POST_ID" --trial
./digcrawl browser check --platform instagram \
  --url "https://www.instagram.com/reel/YOUR_SHORTCODE/" --trial
```

Signed-out sessions or risk controls return structured errors such as `LOGIN_REQUIRED`, `CAPTCHA_REQUIRED`, and `RATE_LIMITED`.

### 16.5 Xiaohongshu and Douyin URLs

Prefer Xiaohongshu URLs with `xsec_token`:

```bash
./digcrawl collect "https://www.xiaohongshu.com/explore/NOTE_ID?xsec_token=xxx&xsec_source=pc_search" \
  --format md,json --out ./collect-output-test
```

Test both standard Douyin video URLs and `modal_id`:

```bash
./digcrawl collect "https://www.douyin.com/video/VIDEO_ID" \
  --format md,json --out ./collect-output-test

./digcrawl collect "https://www.douyin.com/user/SEC_UID?modal_id=VIDEO_ID" \
  --format md,json --out ./collect-output-test
```

Expected: `canonical_url` normalizes to the work detail URL. `platform_metadata.risk_flags` can report diagnostics such as `modal_id_resolved` and `xsec_token_missing`.

### 16.6 Batch file

Prepare `test-urls.txt`:

```text
https://example.com
https://www.toutiao.com/article/YOUR_ARTICLE_ID/
https://www.douyin.com/video/VIDEO_ID
```

Run:

```bash
./digcrawl collect --file test-urls.txt --format md,json --out ./collect-output-test
```

Expected: batch collection obeys `Collect.RateLimitMs` / `Collect.PlatformRateLimits`; failed URLs do not stop other URLs from being processed.

### 16.7 Inspect output and diagnostics

```bash
find ./collect-output-test -maxdepth 3 -type f | sort
```

Inspect these fields in each JSON:

```json
{
  "platform": "...",
  "content_type": "...",
  "title": "...",
  "platform_metadata": {
    "read_channel": "api or browser",
    "extraction_source": "...",
    "api_http_status": 200,
    "api_error_code": "...",
    "api_fetch_mode": "browser_context_fetch",
    "api_signature_mode": "page_runtime"
  }
}
```

## MCP Connection Lifetime and OCR Troubleshooting

### Default lifetime and legacy configuration migration

`digcrawl mcp --config <profile>` keeps a bounded pre-initialization wait by default, then follows the host's stdin pipe after initialization. It no longer disconnects after 15 minutes without tool calls. Host EOF, SIGINT, or SIGTERM ends the proxy. Explicit `--idle-timeout 0` disables both initialization and session idle reclamation. Positive idle timeouts are recommended only for hosts that explicitly support reconnecting. The installer rewrites its managed DigCrawl Codex registration to explicit `0`; other hosts and unrelated configuration retain their existing installation boundaries.

New defaults do not override explicit host registration arguments. Replace legacy `--idle-timeout 15m` with `--idle-timeout 0`, or remove both arguments to use the default lifecycle. Reestablish the MCP connection for changes to take effect. Periodic ping is unnecessary to keep the new default session alive.

### Transport closed and upgrade reconnection

`Transport closed` means MCP transport has closed; it does not establish that a web link is invalid. A closed stdio pipe requires the host to restart and initialize MCP. Restarting the Bridge cannot repair the old pipe. DigCrawl does not promise automatic reconnection on behalf of host products.

After upgrading or replacing the local binary:

1. End the old DigCrawl MCP connection and install the new binary with its complete matching Skill.
2. Use the newly installed binary to validate and explicitly restart the profile actually used by MCP:

```bash
digcrawl config validate --config /absolute/path/to/digcrawl-skill.yaml
digcrawl bridge restart --config /absolute/path/to/digcrawl-skill.yaml
digcrawl doctor --config /absolute/path/to/digcrawl-skill.yaml
```

3. Reconnect and initialize MCP in the host. If there is no separate reconnect action, restart the host application. For example, restart Codex before reading again when a chat retains the old connection. Opening a new chat alone does not guarantee that the old connection has refreshed.

An interrupted request may already have executed, especially media retention or configuration updates. Check its outcome before deciding whether to issue it again.

### On-demand Bridge startup and recovery budget

Before dispatch, the proxy probes and starts the same profile's Bridge as needed. Automatic fault recovery allows at most 3 attempts with a 2-second backoff. Initial startup, normal startup after a verified controlled stop, and explicit manual restart do not consume attempts or clear prior failures. Recovery records update atomically under the existing lifecycle lock and survive proxy reconnections. Ten minutes of continuous successful RPC observations restore the budget; gaps above one minute break continuity. Idleness and system sleep do not consume attempts themselves. This observes the Bridge service path, not readiness of the browser, target site, or OCR; doctor reports these separately.

After exhaustion, fix dependencies first, then explicitly run `digcrawl bridge restart --config <profile>`. An explicit restart permits one manual startup without clearing the automatic budget. Build mismatches still require closing old MCP, explicitly restarting with the matching installed binary, and reconnecting. Clients do not automatically upgrade or downgrade one another's Bridge.

Startup is rejected during `bridge stop`; later new tool requests can start the Bridge on demand after stopping completes. `.stop-*` intent records left by an abnormally terminated stop controller block automatic takeover. Check doctor and confirm the controller and old Bridge/Chrome have exited before an operator handles the specific leftover record in its private cache directory. Do not bulk-delete caches, locks, or everyday Chrome processes. This conservative boundary avoids concurrent recreation when ownership is unknown.

A connection break after dispatch is marked `execution_state=unknown` and is not generically replayed. Even reads may retain media or refresh sessions according to the profile; tool names alone do not prove absence of side effects. Browser internals already have bounded fault retries. On `handled=true`, perform the prerequisites in `next_actions` instead of stacking retries across layers.

### Session image OCR and HTTP errors

The default Skill profile does not enable model OCR or contain a model API Key. To use it, the operator configures `Skill.OCR.Enabled: true` with a usable provider, BaseURL, Model, and credentials in the actual MCP profile. Collect's root `OCR` and Skill-side settings are maintained separately. Image download/retention switches do not enable OCR.

With `Skill.OCR` enabled, normal `browser.browse` obtains budgeted body image bytes in the active page session, including same-origin blob images, and passes them inline to the configured model. Cookies, authentication headers, and image bytes do not enter ordinary MCP output; this does not enable long-term image retention. Same-origin images use the existing session. Cross-origin images try anonymous fetching first so public CDNs with wildcard CORS do not reject credentialed requests. Only authorization or fetch failure permits one additional session attempt within the same timeout. Cross-origin/CORS, login, and permission restrictions still apply. Unavailable images produce per-image failures while preserving the body; inaccessible URLs are not passed to the model for blind retries.

OCR results retain per-image errors, HTTP status, controlled classifications, and success/failure/no-text counts. Partial failures or budget truncation are `degraded`. `no_images` means this call had no processable candidates, not that the entire article has no illustrations. HTTP 400 means the provider rejected the input; it alone does not prove OCR was unconfigured. Causes may involve model input format, image support, or image retrieval. Use per-image classifications:

| Status or classification | Troubleshooting |
| --- | --- |
| `invalid_input` (400/422) | Check provider endpoint, model image support, input format, and size limits; do not repeat the same request blindly |
| `image_unavailable` (400/422) | The provider reports image retrieval failure; check access in the actual browser session and use session byte inputs |
| `authentication_failed` (401/403) | Check OCR service credentials and model access; investigate separately from the web page's sign-in permissions |
| `rate_limited` (429) | Reduce concurrency and retry later according to the service's limits |
| `provider_unavailable` (5xx) | Check OCR service availability and retry after recovery |
| `access_denied` / `fetch_failed` | Browser-side image acquisition failed; check page login, permissions, or CORS. Restarting MCP does not resolve image authorization |
| `too_large` / `unsupported_format` / `budget_exhausted` | Check resource limits and actual inputs; the body can remain available while image recognition degrades |

HTTP 400 is not automatically retried repeatedly. Raw provider responses are not echoed directly. Validate body readability, document coverage, and image OCR success separately.

### Feishu body coverage and image scope

Full reads of Feishu wiki/docx pages traverse rendered blocks within the current page, bounded by 48 windows, 12 seconds of traversal, and approximately 2 MiB HTML, then restore the original scroll position. `content_coverage` separately reports reaching the rendered end, block counts, and truncation. It does not imply that hidden/collapsed content or all original attachments were obtained. `ocr_image_scope=current_rendered_images` identifies the actually rendered image scope separately from body traversal. OCR completed does not mean every attachment in the document was recognized.

### Recovery record migration and downgrade

Recovery record v2 uses a controlled-exit flag to distinguish normal startup from fault recovery. A worker confirms successful normal SIGINT/SIGTERM cleanup. When `bridge stop` directly terminates a process group, only the controller that actually stopped the owned process confirms cleanup, after acquiring the original lifecycle lock and verifying release of the group and browser resources. Running stop for an already absent crashed process does not clear its fault state; failed cleanup cannot confirm normal exit either. Migration from v1 to v2 preserves the old count unchanged as a conservative upper bound. Legacy records may contain only real failures accumulated after a stable reset, so migration does not infer and subtract an "initial startup" or clear the budget for an upgrade. Older versions do not understand v2; direct downgrade compatibility is not guaranteed. Plan a history-preserving record migration before rollback rather than deleting records to bypass the budget.
