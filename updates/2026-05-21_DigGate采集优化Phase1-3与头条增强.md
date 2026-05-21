# 2026-05-21 · DigGate 采集优化 Phase 1–3 与头条增强

> 依据 [plans/2026-05-21-DigGate采集优化方案.md](../plans/2026-05-21-DigGate采集优化方案.md) 对照 MediaCrawler 混合架构，分三阶段落地 **Browser + API 双通道**、会话持久化、Collector/Collect 限流与韧性运维。方案待办已全部勾选完成。

## 概述

在既有平台读取能力（URL 归一化、hydration/meta/DOM 提取、结构化错误码）之上，将 DigGate 采集栈升级为 **API 优先 + Browser 兜底** 的 strategy chain，并补齐头条平台、CDP 登录、Collector worker queue、统一重试与观测日志。签名策略采用 warm 页面内 `fetch(credentials:'include')`，未引入 MediaCrawler JS 源码；metadata 会记录 `api_fetch_mode=browser_context_fetch` 与 `api_signature_mode=page_runtime`，便于观测真实命中率。

---

## 一、Phase 1 — 基础修复与会话（P0 / P1）

### 1.1 URL 与短链

| 项 | 内容 |
|----|------|
| `NormalizedURL` 扩展 | 新增 `QueryParams`、`BrowseURL`、`EffectiveBrowseURL()`（`internal/platform/normalize_url.go`） |
| 小红书 | 保留 `xsec_token` / `xsec_source`；缺失时 `risk_flags: xsec_token_missing`（`xiaohongshu/url.go`、`note.go`） |
| 抖音 | 支持 query `modal_id`、纯数字 ID；`BrowseURL` 保留 query（`douyin/url.go`、`video.go`） |
| 短链 | `ResolveWithBrowserFallback()`：HTTP follow 失败时 rod 导航取最终 URL（`internal/platform/resolve_browser.go`） |

### 1.2 Collector / Collect 限流

| 项 | 内容 |
|----|------|
| Collector 队列 | 会话内有界 channel + worker pool；read loop 只负责快速入队，队列满才 `RATE_LIMITED`；可配置 `RateLimitCooldownMs`（`transport/collector/client.go`、`options.go`） |
| Collect 平台节流 | `PlatformRateLimits` + ±20% jitter（`collect/service.go`、`collect/inbox.go`） |

### 1.3 浏览器会话与 CLI

| 项 | 内容 |
|----|------|
| `browser_pool.go` | CDP attach、`UserDataDir` 持久 profile、`ProxyURL` |
| CLI | `diggate browser login --platform xiaohongshu\|douyin\|toutiao`（`cmd/diggate/browser_cmd.go`、`internal/browser/login.go`） |
| Cookie | 登录后逐域导航并同步 Cookie 写回 `CookieStoreDir`，避免把首页域 Cookie 误写到其他域 |

### 1.4 头条首版（Browser）

| 项 | 内容 |
|----|------|
| 平台包 | `internal/platform/toutiao/*`（url / article / selectors / normalize / metadata） |
| 工具 | `toutiao.article.read` 注册至 provider / collect / config |
| URL | `article/{id}`、`group/{id}`、移动端短链 resolve |

---

## 二、Phase 2 — API 混合通道（P2）

### 2.1 公共能力

| 项 | 内容 |
|----|------|
| `internal/browser/page_fetch.go` | 浏览器上下文 `fetch`（带 Cookie） |
| `internal/platform/session/fetch.go` | session 封装 |
| `internal/platform/strategy.go`、`api_fallback.go` | API 错误分类与 fallback 决策；登录/验证码/限流直接终止，网络/空结构 fallback |
| `internal/platform/retry.go` | Browser 层 `WithBrowseRetry` |
| 配置 | `edith.xiaohongshu.com` 等 API 域加入 allowed domains |

### 2.2 小红书 API

| 项 | 内容 |
|----|------|
| `xiaohongshu/api_client.go` | `POST /api/sns/web/v1/feed`（页面内 fetch） |
| `xiaohongshu/api_mapper.go` | feed JSON → `platform.Content` |
| `note.go` | API 优先 → Browser 兜底；`extraction_source: api\|hydration\|dom` |

### 2.3 抖音 API

| 项 | 内容 |
|----|------|
| `douyin/api_client.go` | aweme detail API（页面内 fetch） |
| `douyin/api_mapper.go` | 响应映射 title / author / stats / media |
| `video.go` | 同 strategy chain |

### 2.4 测试

| 项 | 内容 |
|----|------|
| `internal/platform/strategy_test.go` | strategy chain 单测 |
| `xiaohongshu/api_mapper_test.go`、`douyin/api_mapper_test.go` | fixture 映射单测 |

---

## 三、Phase 3 — 韧性与运维（P3）

### 3.1 统一 RetryPolicy

| 项 | 内容 |
|----|------|
| `internal/platform/retry_config.go` | `BrowseRetryPolicy()` 读取 `Platform.BrowseRetryMaxAttempts` / `BrowseRetryDelayMs` |
| xhs / dy / 头条 Read | 接入 `BrowseRetryPolicy()` + retry 计数写入 metrics |
| `transport/collector/retry.go` | 工具执行 transient 失败有限重试（`Collector.RetryMaxAttempts` / `RetryDelayMs`） |

### 3.2 健康检查

| 项 | 内容 |
|----|------|
| `internal/platform/healthcheck/check.go` | pong（平台首页）+ 可选试读 |
| CLI | `diggate browser check --platform ... [--url URL] [--trial]` |
| 配置 | `Platform.HealthProbes` 可预设各平台 probe URL |

### 3.3 观测与指标

| 项 | 内容 |
|----|------|
| `internal/platform/observability.go` | `ReadMetrics`、`MergeContentMetrics`、`[platform-read]` 结构化日志 |
| metadata | `read_channel`、`api_latency_ms`、`browser_latency_ms`、`retry_count` |
| API attempt | `api_fetch_mode`、`api_signature_mode`、`api_http_status`、`api_error_code`、`api_error_message` |
| 配置 | `Platform.ReadMetricsEnabled`（默认 true） |

### 3.4 Proxy

沿用 `LocalBrowser.ProxyURL`，经 browser pool / page fetch 路径生效；API 签名 fetch 与 Browser 共用同一会话。

---

## 四、头条增强（Phase 2 观测 + Phase 3 同期）

| 项 | 内容 |
|----|------|
| URL | 新增 `/w/{id}` 分享形态（`toutiao/url.go`） |
| API | `toutiao/api_client.go` 尝试 `/article/v2/index/`、`/group/article_info/` |
| Mapper | `toutiao/api_mapper.go` + `testdata/api_ok.json` |
| Hydration | `metadata.go` 强化 `deepFindArticle`、script JSON 解析 |
| Strategy | `article.go`：API 优先 → Browser 兜底 → metrics |
| 单测 | `api_mapper_test.go`、`url_test.go` |

> 头条 API 端点依赖真实 Cookie / 页面环境，线上可能仍频繁 fallback；可通过 `extraction_source` 与 `[platform-read]` 日志观测成功率。

---

## 五、配置变更摘要

`config/config.yaml` / `config/config.go` 新增或扩展：

```yaml
Collector:
  RateLimitCooldownMs: 1000
  CallQueueSize: 32
  CallQueueWaitMs: 30000
  RetryMaxAttempts: 2
  RetryDelayMs: 1500

Collect:
  PlatformRateLimits:
    xiaohongshu: 2000
    douyin: 2500
    toutiao: 2000

Platform:
  BrowseRetryMaxAttempts: 2
  BrowseRetryDelayMs: 800
  ReadMetricsEnabled: true
  HealthProbes: {}

LocalBrowser:
  CDPMode: false
  UserDataDir: ""
  ProxyURL: ""
```

---

## 六、Strategy Chain 行为摘要

| 错误/状态 | 行为 |
|----------|------|
| 网络超时、临时 5xx | API fallback Browser；Browser 按 RetryPolicy 重试 |
| API 签名失败、空结构 | fallback Browser；metadata 记录 `api_error_code` |
| `LOGIN_REQUIRED` / `CAPTCHA_REQUIRED` / `RATE_LIMITED` | 直接返回结构化错误，提示 `browser login` 或降低频率 |
| `CONTENT_DELETED` / `CONTENT_PRIVATE` | 终止，不重试、不 fallback |

---

## 七、使用示例

```bash
# 扫码登录（持久 profile + Cookie）
diggate browser login --platform xiaohongshu

# 健康检查：pong + 试读
diggate browser check --platform toutiao \
  --url "https://www.toutiao.com/article/..." --trial

# 采集（API 优先，失败自动 Browser 兜底）
diggate collect "https://www.xiaohongshu.com/explore/...?xsec_token=..."
diggate collect "https://www.douyin.com/video/..."
diggate collect "https://www.toutiao.com/article/..."
```

---

## 八、验证

- `/Users/dig/.gvm/gos/go1.24.8/bin/go test ./...` 全量通过
- `/Users/dig/.gvm/gos/go1.24.8/bin/go build ./cmd/diggate/...` 通过

---

## 九、后续建议

- 各平台真实 URL live smoke（有 / 无 Cookie 各一轮），固化样例 URL
- 头条 API 端点按线上观测调整；成功率低时以 Browser/hydration 为主
- xhshow / a_bogus 若需脱离页面 fetch，评估自研 Go 签名；当前页面运行时 fetch 不复制 MediaCrawler JS 源码，但真实 API 成功率仍需 live smoke 观测

---

## 相关文档

- 方案：[plans/2026-05-21-DigGate采集优化方案.md](../plans/2026-05-21-DigGate采集优化方案.md)
- 前序更新：[2026-05-21_抖音小红书X平台读取完善.md](./2026-05-21_抖音小红书X平台读取完善.md)
- 使用：[USAGE.md](../USAGE.md)
