# 2026-07-04 X 内容抓取 API 与 oEmbed 兜底实现同步

## 背景

本次更新面向 X/Twitter 单帖内容抓取。目标是在无稳定浏览器登录态的情况下，尽量通过官方 API/公开端点完成公开帖子采集，并避免把登录墙、删除页、不可见壳页误导出为正常内容。

## 主要更新

- 新增 X 平台 API 优先读取链路：
  - Tweet lookup API 优先。
  - 公开 `publish.twitter.com/oembed` 作为公开帖子兜底。
  - Browser 仅作为最后 fallback。
- 新增 X API 配置：
  - `Platform.X.APIEnabled`
  - `Platform.X.OEmbedEnabled`
  - `Platform.X.AuthMode`
  - `Platform.X.APIBaseURL`
  - `Platform.X.TokenURL`
  - `Platform.X.OEmbedURL`
  - `Platform.X.ConsumerKey`
  - `Platform.X.ConsumerKeySecret`
  - `Platform.X.BearerToken`
  - `Platform.X.APITimeoutSec`
  - `Platform.X.APIMaxRetries`
- 增加 X OAuth2 app-only token 获取与缓存。
- 增加 Tweet lookup JSON 到统一 `platform.Content` 的映射。
- 增加 oEmbed HTML 到统一 `platform.Content` 的映射，支持正文、作者、发布时间、HTML、canonical URL。
- 增强 X Browser fallback 校验：
  - 登录墙不再作为正常内容导出。
  - 删除/不可见/不存在壳页不再作为正常内容导出。
  - 空正文返回结构化错误。
- 增强错误分类：
  - token endpoint 和 Tweet lookup 先按 HTTP status 分类，再解析 JSON。
  - 非 JSON 错误体也能正确输出 `LOGIN_REQUIRED`、`RATE_LIMITED`、`CONTENT_NOT_FOUND`、`SCOPE_VIOLATION` 等结构化诊断。
  - `api_auth_mode` 会反映实际认证模式，不再硬编码。
- README 和 USAGE 已同步 X 抓取策略说明。

## 测试与验证

- 单元测试：
  - `GOCACHE=/private/tmp/digcrawl-gocache go test ./internal/platform/x`
  - `GOCACHE=/private/tmp/digcrawl-gocache go test ./...`
- 生产级回归：
  - 使用 5 条公开 X 帖子执行 `digcrawl collect`。
  - 结果：5/5 成功。
  - 最终通道：`read_channel=api`。
  - 最终来源：`extraction_source=oembed`。
  - Browser 未进入主路径。
- 测试报告：
  - `tests/2026-07-04/2026-07-04-X内容抓取生产级测试报告.md`

## 重要结论

当前配置中的 X 凭据可完成 token 交换，但 Tweet lookup 返回 `402`，表示账号套餐或权限尚不能直接读取 Tweet lookup。公开帖子采集已通过官方 oEmbed fallback 保持可用；后续如果开通 Tweet lookup 权限，现有链路会自动优先使用 Tweet lookup。

## 安全说明

本项目为内部项目，X API 凭据按需求写入配置文件。测试报告与 update 文档不记录实际 key、secret 或 bearer token。
