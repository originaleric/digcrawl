# 2026-08-26 头条 Watchlist LOGIN_REQUIRED 复现与修复测试报告

## 目标

使用最新本地源码、不重启常驻服务、不替换当前运行 bundle，复现今早头条视频/微头条 watchlist 返回 `LOGIN_REQUIRED` 的问题，定位根因并验证修复。

## 环境

- 仓库：`/Users/dig/projects/digcrawl`
- 分支：`main`
- 最新远端提交：`65b5c02`
- 测试方式：`go run -tags digflow_embedded ./cmd/digcrawl ...`
- 缓存隔离：
  - `GOCACHE=/private/tmp/digcrawl-gocache`
  - `GOMODCACHE=/Users/dig/projects/digcrawl/.tmp/go-mod-watchlist`
- 常驻服务：未重启

## 复现结果

最新源码修复前，三个真实 watchlist 源均可复现 `LOGIN_REQUIRED`：

```text
watch=toutiao-video-list-ca213b1aca67a8 -> LOGIN_REQUIRED
watch=toutiao-wtt-list-ciag3h9k8zxccj3x -> LOGIN_REQUIRED
watch=toutiao-zsaf-video-list -> LOGIN_REQUIRED
```

进一步用临时 browser 调试入口绕过 watchlist 封装后发现：

- 默认浏览器选择会优先使用本机 Google Chrome，本地一次性测试中失败在 browser acquire：`无法启动或连接本地浏览器`。
- 强制使用 Playwright `chrome-headless-shell` 后，头条主页可以打开，且页面能看到作者信息和已登录状态，不是登录页。
- 初始 DOM 只看到作者信息和“加载失败，请重新刷新”，没有 `/video/` 或 `/w/` 内容链接。
- 增加滚动和等待后，动态列表能正常注入，能抓到真实内容链接。

## 根因

根因不是 cookie store 整体损坏，而是三段问题叠加：

1. Headless 服务场景默认优先选择本机 Google Chrome，当前环境中该路径启动/连接不稳定。
2. 头条作者页的 tab 内容不是首屏立即出现，必须滚动并等待动态列表注入；原 discovery metadata 只读取初始 DOM，容易拿不到 `/video/` 或 `/w/`。
3. 当 browser fallback 没拿到内容时，代码继续调用无 cookie 的 `api/pc/user/info?token=...` 探测，并把真实的 browser/content failure 覆盖成单一 `LOGIN_REQUIRED`，导致诊断误导。

## 修复内容

- `internal/browser/browser_pool.go`
  - headless 模式下优先选择 Playwright `chrome-headless-shell`，非 headless/交互登录仍优先使用本机 Chrome。

- `internal/source/service.go`
  - browser fallback 失败时保留 static/browser/access probe 三段错误，不再让无 cookie access probe 覆盖真实错误。
  - discovery metadata 改为滚动并等待 8 轮，每轮 1.5 秒，再统一收集 `hrefs/scripts/text/url`，用于发现头条动态 tab 内容。

## 验证命令

```bash
GOCACHE=/private/tmp/digcrawl-gocache \
GOMODCACHE=/Users/dig/projects/digcrawl/.tmp/go-mod-watchlist \
go test -tags digflow_embedded ./internal/browser ./internal/source ./internal/watchlist/service
```

结果：

```text
ok github.com/originaleric/digcrawl/internal/browser
ok github.com/originaleric/digcrawl/internal/source
ok github.com/originaleric/digcrawl/internal/watchlist/service
```

真实源 `test-discover`：

```bash
GOCACHE=/private/tmp/digcrawl-gocache \
GOMODCACHE=/Users/dig/projects/digcrawl/.tmp/go-mod-watchlist \
go run -tags digflow_embedded ./cmd/digcrawl watchlist source test-discover --config config/config.yaml --id <watch_id>
```

结果：

```text
toutiao-video-list-ca213b1aca67a8:
- toutiao:video:7676826719306826278
- toutiao:video:7676816309006385670

toutiao-wtt-list-ciag3h9k8zxccj3x:
- toutiao:1874541998639179
- toutiao:1874452834086915

toutiao-zsaf-video-list:
- toutiao:video:7677608045073203254
- toutiao:video:7677510685242228770
```

## 结论

本轮测试通过。最新源码下三个真实头条 watchlist 源均能发现 2 条内容，`LOGIN_REQUIRED` 已被定位为错误包装问题，真实抓取失败点是动态 tab 内容未等待/滚动注入。

常驻服务尚未重启，当前运行服务仍不会自动具备本次修复。
