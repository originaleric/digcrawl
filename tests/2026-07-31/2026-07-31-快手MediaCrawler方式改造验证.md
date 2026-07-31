# 2026-07-31 快手 MediaCrawler 方式改造验证

## 背景

前一轮快手真实 URL 测试中，`kuaishou.video.read` 主要依赖本机浏览器读取，匿名或风控场景会卡到浏览器等待超时，错误语义也不够接近平台实际状态。

本轮参考 MediaCrawler 的快手实现思路：浏览器主要用于登录态/Cookie，详情和列表数据优先通过快手 Web GraphQL 接口读取。

## 改造内容

- 新增 `internal/platform/kuaishou/client.go`，封装快手 Web GraphQL 请求。
- 快手视频详情优先调用 `visionVideoDetail`，失败后按平台错误码处理。
- 快手用户列表优先调用 `visionProfilePhotoList`，失败后回落到 HTML/browser 链接发现。
- GraphQL 返回风控响应时映射为 `CAPTCHA_REQUIRED`，不再继续进入长时间浏览器超时。
- `kuaishou.video.read` provider capabilities 增加 `api`。
- README、USAGE 和功能手册同步说明快手 API-first 与本机 Cookie 前置条件。

## 验证命令

```bash
GOCACHE=/private/tmp/digcrawl-gocache go test ./internal/platform/kuaishou ./internal/source ./provider/platform ./collect
GOCACHE=/private/tmp/digcrawl-gocache go test ./...
GOCACHE=/private/tmp/digcrawl-gocache go build -o /private/tmp/digcrawl-prod-20260731-kuaishou-api/digcrawl ./cmd/digcrawl
/private/tmp/digcrawl-prod-20260731-kuaishou-api/digcrawl collect --config config/config.yaml --out /private/tmp/digcrawl-prod-20260731-kuaishou-api/out2 --format json https://www.kuaishou.com/short-video/3xqxmjkthzpckus
```

## 验证结果

- `go test ./internal/platform/kuaishou ./internal/source ./provider/platform ./collect`：通过。
- `go test ./...`：通过。
- `go build`：二进制生成成功；Go 在模块 stat cache 写入时输出权限警告，但命令退出码为 0，产物存在。
- 真实快手 URL smoke：260ms 返回结构化风控错误。

```text
[gateway-audit] tool=kuaishou.video.read ... status=error err_code=CAPTCHA_REQUIRED duration_ms=260
collect: 快手 GraphQL 触发风控，需要登录态或安全验证
```

## 结论

快手已从 browser-only 路径改为 Web GraphQL API-first。当前匿名真实 URL 在快手侧触发风控时可以快速、明确地返回 `CAPTCHA_REQUIRED`，不会再落入 35 秒或 90 秒浏览器等待超时。

后续如需真实采集成功，需要先在本机完成快手登录/安全验证，使 Cookie store 中存在可用的 `www.kuaishou.com` 登录态。
