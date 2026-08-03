# 2026-08-03-第1轮-DigCrawl-Skill-生产模拟测试报告

## 1. 测试结论

- 结论：失败（存在 P1，详见 Findings）。
- 项目：DigCrawl 新增本地 MCP Skill（提交 `da36722`）。
- 测试时间：2026-08-03 10:18–10:23（Asia/Shanghai）。
- 测试人：Codex。
- 本轮核心输出要求：通过 MCP stdio 使用真实公开 URL 完成 RSS 内容抓取、浏览器正文抓取与截图；确认 Phase 1 的 LLM 使用边界。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；本轮由当前源码构建临时 `digcrawl` 二进制 |
| 启动入口 | `digcrawl mcp --config <临时生产测试配置>` |
| 测试命令 | `env GOCACHE=/private/tmp/digcrawl-gocache go test ./... && go vet ./...` |
| 配置文件 | 基于 `config/digcrawl-skill.example.yaml` 的临时副本；artifact 与 cookie 目录均位于 `/private/tmp` |
| 依赖服务 | 真实公开网络、已安装 Chromium、MCP stdio 客户端 |
| 核心业务输出 | MCP `feed.read`、`browser.browse` 的结构化文本/Markdown 与 MCP 图片 artifact |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| BrowserEnabled | configured | 临时 profile 显式启用；截图路径实际启动本地 Chromium 并成功。 |
| Artifact store | configured | 启用，TTL 为 1440 分钟，默认 inline 上限 10 MiB。 |
| OCR | skippable | Phase 1 profile 显式关闭，未调用 OCR/LLM。 |
| ASR | skippable | Phase 1 profile 显式关闭，未调用 ASR/LLM。 |
| LLM/API Key | skippable | 本轮成功路径均不涉及 LLM；依约未读取、未使用真实 API Key。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 构建当前版本 | `go build -o /private/tmp/digcrawl-skill-prodtest ./cmd/digcrawl` | 通过 | 由提交 `da36722` 构建。 |
| MCP 握手与工具发现 | `initialize`、`tools/list` | 通过 | 协商版本 `2025-03-26`；暴露 `gateway.ping`、`browser.browse`、`feed.read`。 |
| 实时 RSS 抓取 | `feed.read`：`https://hnrss.org/frontpage` | 通过 | 1.698 s 返回标题 `Hacker News: Front Page`、20 条实时条目、Markdown 与 `structuredContent`。 |
| 浏览器截图 | `browser.browse`：`https://example.com/`，`action=screenshot` | 通过 | 1.175 s 返回 `image/png`，17,092 bytes，同时返回 artifact URI 与 MCP `image` content。 |
| 浏览器正文（默认请求） | `browser.browse`：`https://example.com/`，仅 `mode=full` | 失败 | 60.001 s 后返回 `TOOL_EXEC_FAILED` / `未能提取页面正文`。 |
| 浏览器正文（显式选择器） | 同上，增加 `content_selector=body` | 通过 | 1.166 s 返回 `Example Domain` 正文和 Markdown，说明浏览器导航与提取能力均可用。 |
| 回归与静态检查 | `go test ./... && go vet ./...` | 通过 | 全部 Go 包测试通过，`go vet` 无输出。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| MCP 兼容性 | 客户端可初始化、发现并调用 Skill 工具 | 初始化、工具发现和真实调用均成功 | 通过 |
| 实时内容抓取 | 返回可消费的结构化内容 | RSS 返回 20 条实时条目、文本与 Markdown | 通过 |
| 截图交付 | 有效图片与可追踪 artifact | 返回 `image/png`、17,092 bytes、`digcrawl-artifact://` URI 和 24 小时到期时间 | 通过 |
| 默认正文抓取 | 仅给 URL 即可取得普通网页正文 | 对仅含 `body` 的公开页面超时失败 | 失败 |
| LLM 调用纪律 | 仅在启用的路径调用真实 Key | Profile 中所有 LLM 相关路径关闭；未发生调用 | 通过 |

## 6. Findings

```text
[P1] browser.browse 的默认 full 提取无法覆盖仅含 body 的普通网页
Location/Command: internal/browser/browser_browse.go 的 extractMainContent；MCP tools/call browser.browse {"url":"https://example.com/","mode":"full"}
Reproduction: 使用 Phase 1 Skill 的默认浏览器请求抓取 https://example.com/；60.001 秒后返回 TOOL_EXEC_FAILED / “未能提取页面正文”。同一 URL 显式传入 content_selector=body 则 1.166 秒成功。
Expected: Skill 文档允许用户只提供 URL 时，普通公开网页应能在工具默认路径中提取 body 正文。
Actual: 默认选择器依次等待 #js_content、article、main、[role='main']；在全局 60 秒工具时限内耗尽后，body 回退没有生效。用户必须知晓内部选择器并手工传参才可读取该类页面。
Suggested direction: 让默认正文回退与整体工具时限协同，确保 body 候选在时限内被尝试；保持现有显式 content_selector 语义，不需新增配置或领域抽象。
```

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| OCR/ASR/LLM 真实 API | Phase 1 profile 明确关闭，且本轮 URL 场景不需要该路径 | 在后续启用对应能力的 profile 中，使用用户配置的真实 Key 单独做一次受控验收。 |
| 登录态社媒 URL | 本轮验证 Skill 公共 URL 核心链路，不使用个人 Cookie 或第三方账号 | 需要验证微信等受登录影响平台时，另行由用户授权并提供可用本地登录态。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-08-03/2026-08-03-第1轮-DigCrawl-Skill-生产模拟测试报告.md` | 可复核的生产模拟记录。 |
| 截图 artifact | `/private/tmp/digcrawl-skill-prodtest-2026-08-03/artifacts/mcp_52f8db0b-b614-40a7-b539-fb21f1796a8d_screenshot.bin` | 17,092 bytes；临时测试产物，不纳入仓库。 |

## 9. 最终说明

真实 RSS、真实浏览器截图、MCP 结构化输出和 artifact 存储均已通过；但 Skill 最常用的“只提供 URL 获取正文”默认路径对普通页面失败，因此本轮不满足上线级通过标准。修复该 P1 后，应重新执行相同的默认正文抓取与现有 RSS/截图回归。
