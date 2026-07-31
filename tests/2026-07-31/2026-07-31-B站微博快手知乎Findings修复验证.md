# 2026-07-31 B站/微博/快手/知乎 Findings 修复验证

## 修复范围

根据 `2026-07-31-B站微博快手知乎真实URL生产级测试报告.md` 的 Findings，本轮修复覆盖：

- 新增国内渠道进入生产配置准入边界。
- 微博访客系统/登录壳页不再误判为成功内容。
- 快手浏览器采集超时不再拖满 90 秒，并返回结构化平台错误。
- B站 API 视频在简介过薄时仍提供可供 summary 使用的基础上下文。
- 平台结构化错误在 runtime/audit 中不再显示为 success。

## 验证命令

```bash
GOCACHE=/private/tmp/digcrawl-gocache go test ./config ./internal/platform/weibo ./internal/platform/kuaishou ./internal/platform/bilibili ./provider/platform ./collect
```

结果：通过。

```bash
GOCACHE=/private/tmp/digcrawl-gocache go test ./runtime ./provider/platform ./collect ./internal/platform/weibo ./internal/platform/kuaishou ./internal/platform/bilibili ./config
```

结果：通过。

```bash
GOCACHE=/private/tmp/digcrawl-gocache go test ./...
```

结果：通过。

```bash
git diff --check
```

结果：通过。

## 真实 URL smoke

### B站

```bash
/private/tmp/digcrawl-prod-20260731-domestic-fix/digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-domestic-fix/bilibili --format json https://www.bilibili.com/video/BV1xx411c7mD
```

结果：成功。正式配置不再出现 `DOMAIN_NOT_ALLOWED`。

落盘内容要点：

- `platform=bilibili`
- `content_type=video`
- `title=字幕君交流场所`
- `text_len=75`
- `text=标题: 字幕君交流场所 ... UP主: 碧诗 ...`

### 微博

```bash
/private/tmp/digcrawl-prod-20260731-domestic-fix/digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-domestic-fix/weibo-after-runtime --format json https://m.weibo.cn/detail/502233445566
```

结果：失败符合预期，不再落盘 `Sina Visitor System` 成功内容。

关键日志：

```text
[gateway-audit] tool=weibo.post.read ... status=error err_code=LOGIN_REQUIRED
collect: 微博页面进入访客系统，需要登录态或安全验证
```

### 快手

```bash
/private/tmp/digcrawl-prod-20260731-domestic-fix/digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-domestic-fix/kuaishou-after-runtime --format json https://www.kuaishou.com/short-video/3xqxmjkthzpckus
```

结果：失败符合预期，35 秒返回结构化错误，不再拖满旧的 90 秒。

关键日志：

```text
[gateway-audit] tool=kuaishou.video.read ... status=error err_code=PARTIAL_CONTENT duration_ms=35002
collect: 快手页面浏览器采集超时
```

### 知乎

```bash
/private/tmp/digcrawl-prod-20260731-domestic-fix/digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-domestic-fix/zhihu --format json https://www.zhihu.com/question/344682829/answer/1100147389
```

结果：成功。正式配置不再出现 `DOMAIN_NOT_ALLOWED`。

## Fault Model Coverage

- 配置准入：tested。四个真实 URL 均已不再被正式配置域名拦截。
- 平台错误语义：tested。微博和快手返回 `status=error`，audit 有平台错误码。
- 内容库污染：tested。微博壳页不再以成功内容落盘。
- 长耗时阻塞：tested。快手从约 90 秒降为 35 秒结构化失败。
- Summary 输入质量：tested。B站 API 输出包含标题、简介、UP 主和互动数据，避免只有 `www`。
- 回归风险：tested。`go test ./...` 和 `git diff --check` 均通过。

## 结论

本轮报告中的 P1/P2 均已完成代码侧修复并通过回归。快手真实 URL 仍可能因为站点限制无法成功采集，但当前行为已经从长时间不可诊断失败收敛为可观测的结构化平台错误。
