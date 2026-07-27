# 2026-07-27 Watchlist 来源页生产级测试报告

## 测试目标

使用 `dig-production-test` 口径验证 Watchlist 来源页发现与采集链路：

- 头条作者页来源：`https://www.toutiao.com/c/user/token/<redacted>/?source=profile`
- X 用户页来源：`https://x.com/elonmusk`

要求覆盖来源页发现、去重 key、单次调度、采集、总结、通知链路的生产级可运行性。Feishu 通知本轮使用 dry-run/noop，不真实发送消息。

## 环境与前置

- 项目目录：`/Users/dig/projects/digcrawl`
- 测试二进制：`/private/tmp/digcrawl-prod/digcrawl`
- 输出目录：`/private/tmp/digcrawl-prod/bothrun2/output`
- Go 缓存隔离：
  - `GOCACHE=/private/tmp/digcrawl-gocache`
  - `GOPATH=/private/tmp/digcrawl-gopath`
- 浏览器能力：本机 Chromium/Chrome headless 可调用。
- 登录态：本机存在头条 cookie 存储文件，但该来源页在静态接口与 headless 读取中仍返回登录态不足。

## 执行命令

```bash
GOCACHE=/private/tmp/digcrawl-gocache GOPATH=/private/tmp/digcrawl-gopath go test ./internal/source ./internal/watchlist/service ./transport/http ./cmd/digcrawl
GOCACHE=/private/tmp/digcrawl-gocache GOPATH=/private/tmp/digcrawl-gopath go build -o /private/tmp/digcrawl-prod/digcrawl ./cmd/digcrawl

/private/tmp/digcrawl-prod/digcrawl watchlist source test-discover \
  --config /private/tmp/digcrawl-prod/watchlist-discover.yaml \
  --id x-list

/private/tmp/digcrawl-prod/digcrawl watchlist source test-discover \
  --config /private/tmp/digcrawl-prod/watchlist-discover.yaml \
  --id toutiao-list

/private/tmp/digcrawl-prod/digcrawl watchlist run \
  --config /private/tmp/digcrawl-prod/watchlist-both.yaml \
  --once

GOCACHE=/private/tmp/digcrawl-gocache GOPATH=/private/tmp/digcrawl-gopath go test ./...
git diff --check
```

## 结果

整体结论：`PARTIAL`。X 来源页链路跑通；头条作者页链路被平台登录要求阻塞，当前代码已能明确返回 `LOGIN_REQUIRED`，不再误判为空列表或静默成功。

### X 来源页

状态：`PASSED`

- `watchlist source test-discover` 成功发现 5 条帖子。
- 发现内容使用稳定 `ContentKey`，格式为 `x:<status_id>`。
- `watchlist run --once` dry-run 中成功完成发现、采集、总结和 noop 通知。
- 采集路径走 X oEmbed API，输出了 JSON/Markdown 结果。

本轮样例输出：

- `x:2081141965651898709`
- `x:2081616755823186385`
- `x:2081473387583484395`
- `x:2081614718414831810`
- `x:2080975062841336176`

产物样例：

- `/private/tmp/digcrawl-prod/bothrun2/output/2026-07-27/Peter is not correct about me in this case. I have always known that dangers fro.json`
- `/private/tmp/digcrawl-prod/bothrun2/output/2026-07-27/Peter is not correct about me in this case. I have always known that dangers fro.md`
- `/private/tmp/digcrawl-prod/bothrun2/output/2026-07-27/.assets/media.json`

### 头条作者页

状态：`BLOCKED`

- 静态 HTML 抓取只返回头条 JS shell，未暴露文章链接。
- Headless 浏览器 fallback 能打开页面，但页面正文与链接未返回有效作者内容。
- 头条 PC 用户信息接口对该 token 返回登录要求，代码识别为：

```text
LOGIN_REQUIRED: toutiao author page requires login
```

这不是当前 Watchlist 调度逻辑或来源抽象的失败，而是该头条作者页在当前机器的有效登录态不足。需要重新建立有效头条登录态后重跑。

## 已修复项

### P1: 头条来源页失败原因不可区分

修复状态：`FIXED`

之前头条来源页在静态 HTML 无链接时会表现为普通 `CONTENT_NOT_FOUND`，不利于判断是代码解析失败还是平台登录态不足。现在来源服务会：

- 先执行静态 HTML 链接发现。
- 无结果时使用本机 headless 浏览器 fallback。
- 对头条作者页额外探测用户信息接口。
- 明确返回 `LOGIN_REQUIRED`，避免把登录态问题误报为内容不存在。

### P2: `watchlist run --once` 遇到来源失败仍返回 0

修复状态：`FIXED`

之前 `--once` 会记录错误日志但进程退出码仍可能为 0，生产调度无法感知失败。现在 `watchlist run --once` 在任一来源返回错误时会以非 0 退出，便于 cron、systemd、CI 或外层编排识别失败。

## 验证

```text
go test ./internal/source ./internal/watchlist/service ./transport/http ./cmd/digcrawl 通过
go build ./cmd/digcrawl 通过
go test ./... 通过
git diff --check 通过
```

## 残余风险

- 头条作者页需要有效登录态或可用的官方/半官方来源接口；当前机器的已有 cookie 对本次 URL 不足。
- 头条 PC 端接口和页面结构变化频繁，即使登录态恢复，也需要保留 browser fallback 与明确错误分类。
- X 当前可通过公开页面发现与 oEmbed 采集跑通，但未来页面结构变化仍可能影响来源页发现。

## 后续建议

- 重新执行头条网页登录并刷新本机 cookie 后，单独重跑 `toutiao-list`。
- 如果头条作者页后续仍返回登录要求，需要补充可持续的账号登录态维护流程，或者切换到可授权的接口来源。
