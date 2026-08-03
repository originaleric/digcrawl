# 2026-08-03-第2轮-DigCrawl-Skill-生产模拟测试报告

## 1. 测试结论

- 结论：通过。
- 项目：DigCrawl 新增本地 MCP Skill；修复第 1 轮发现的默认正文抓取问题。
- 测试时间：2026-08-03 10:26–10:28（Asia/Shanghai）。
- 测试人：Codex。
- 本轮核心输出要求：默认仅传 URL 的 MCP `browser.browse` 应成功返回真实网页的正文与 Markdown。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；当前工作区源码构建的临时 `digcrawl` 二进制 |
| 启动入口 | `digcrawl mcp --config <临时生产测试配置>` |
| 修改范围 | `provider/browser/entries.go`：默认 `full` 请求在未指定选择器时传入 `body` |
| 配置文件 | Phase 1 profile 的临时副本；浏览器和 artifact store 均启用 |
| 依赖服务 | 真实公开网络、已安装 Chromium、MCP stdio 客户端 |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| BrowserEnabled | configured | 真实本地 Chromium 启动成功。 |
| 默认内容选择器 | configured | 仅在 `mode` 为空或为 `full` 且调用方未提供 `content_selector` 时使用 `body`。 |
| 显式内容选择器 | configured | 保持调用方传入值，不被默认值覆盖。 |
| OCR/ASR/LLM | skippable | Phase 1 profile 关闭该路径；本轮无需使用真实 API Key。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 受影响包回归 | `go test ./provider/browser ./transport/mcp ./cmd/digcrawl && go vet ./provider/browser` | 通过 | 全部通过。 |
| 修复后真实 MCP 验收 | `browser.browse`：`{"url":"https://example.com/","mode":"full"}` | 通过 | 2.834 s 返回标题 `Example Domain`、正文与 Markdown；请求未传 `content_selector`。 |
| 全量回归与静态检查 | `go test ./... && go vet ./...` | 通过 | 全部 Go 包通过；`go vet` 无输出。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 默认正文抓取 | 只提供 URL 即取得普通网页正文 | 成功返回 `Example Domain` 的正文与 Markdown | 通过 |
| MCP 结构化输出 | 客户端可直接消费 | 同时返回文本 content 与 `structuredContent` | 通过 |
| 显式配置兼容性 | 显式 `content_selector` 不受影响 | 实现仅在输入为空时写入 `body` | 通过 |
| 现有功能回归 | 所有 Go 测试与静态检查通过 | `go test ./... && go vet ./...` 通过 | 通过 |

## 6. Findings

本轮未发现阻塞性问题。

第 1 轮的 P1 已关闭：MCP Provider 现在与 collect 的通用网页采集路径一致，在默认完整读取时主动选择 `body`。

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| OCR/ASR/LLM 真实 API | Phase 1 profile 明确关闭，浏览器正文读取不涉及该能力 | 在后续启用该能力的 profile 中，单独执行使用真实 Key 的受控验收。 |
| 登录态社媒 URL | 本轮针对已修复的公共网页默认读取链路 | 需要时由用户授权本地登录态后，单独验证微信等平台。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-08-03/2026-08-03-第2轮-DigCrawl-Skill-生产模拟测试报告.md` | 修复后的生产模拟记录。 |

## 9. 最终说明

第 1 轮的真实 URL 失败用例已通过相同 MCP 调用完成复验。该修复复用了 collect 已采用的 `body` 选择器策略，未增加新配置、抽象或领域模型。
