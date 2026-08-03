# 2026-08-03-第5轮-DigCrawl-Skill-ASR-P1生产复验报告

## 1. 测试结论

- 结论：通过。
- 项目：DigCrawl 本地 MCP Skill。
- 测试时间：2026-08-03（Asia/Shanghai）。
- 测试人：Codex。
- 本轮核心输出要求：确认第 3 轮“显式 ASR 静默成功”的 P1 已关闭；对于真实视频 URL，无法获得可转写媒体时必须向 MCP 客户端返回明确失败，而非成功空结果。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；当前工作区构建的临时 `digcrawl` 二进制 |
| 启动入口 | `digcrawl mcp --config <临时媒体测试 profile>` |
| 测试命令 | 本地 MCP stdio 客户端依次调用 `browser.browse`、`bilibili.video.read` |
| 配置文件 | 仅用于测试的本地 profile；媒体下载与 ASR 开启，OCR 使用 stub |
| 依赖服务 | 真实公开网络、本地 Chromium、httpbin 图片页、B 站公开接口 |
| 核心业务输出 | MCP 的 `isError`、结构化 `error.code` / `error.message` 与已抓取内容 |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| Skill 媒体下载 | configured | 测试 profile 启用，并使用本地临时目录。 |
| Skill ASR 开关 | configured | 测试 profile 显式开启，以验证请求语义。 |
| 浏览器与 artifact store | configured | 图片调用成功产出截图 artifact。 |
| 公开图片与视频 URL | configured | 两个真实 URL 均可访问。 |
| 真实 OCR Provider | skippable | 本轮验证 P1 错误语义，OCR 使用 stub；不将其计为真实 OCR 推理。 |
| 本地真实 ASR 运行时 | missing | Collect 配置的 `.venv-asr` 与 `faster_whisper` 尚未就绪；本轮不依赖它来确认无媒体时的错误语义。 |
| 视频摘要 LLM | skippable | 本轮未请求摘要，且其前提是可用转写。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 构建 | `go build -o /private/tmp/digcrawl-skill-media-prodtest ./cmd/digcrawl` | 通过 | 使用当前工作区源码运行 MCP。 |
| 真实图片 smoke | `browser.browse`：`https://httpbin.org/image/png`，`include_ocr:true` | 通过 | `isError:false`，`ocr_count:1`，且有截图 artifact。 |
| P1 真实视频复验 | `bilibili.video.read`：`https://www.bilibili.com/video/BV1xx411c7mD`，`include_media:true`、`include_asr:true` | 通过 | `isError:true`，`error.code: TOOL_EXEC_FAILED`，错误为 `ASR unavailable: no downloaded video/audio asset was available for ASR`。 |
| 既有全量回归 | `go test ./...`、`go vet ./...` | 通过 | 修复后第 4 轮已在完整本机环境执行并通过；本轮未变更源码。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 显式 ASR 无媒体 | 不应静默成功 | 返回 MCP `isError:true` | 通过 |
| 错误可消费性 | 客户端可得到机器可识别与可读原因 | `TOOL_EXEC_FAILED` 加具体错误消息 | 通过 |
| 已读取内容保留 | 出错时仍保留已抓取的视频元数据，便于客户端展示/诊断 | `structuredContent` 保留标题、作者、媒体清单与平台元数据 | 通过 |
| 图片能力回归 | 不受 ASR 失败语义影响 | 图片请求继续成功 | 通过 |

## 6. Findings

本轮未发现阻塞性问题。第 3 轮 P1 已关闭：此前的真实 B 站用例由“成功且无结果”变为明确 MCP 工具失败。

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| 真实 OCR 质量 | 本地未配置真实 Provider 凭据 | 用户配置后，以真实图片复测识别质量。 |
| 成功 ASR 与视频摘要 | 本地 ASR 运行时未就绪，且本 URL 未暴露可下载音视频资源 | 补齐 Collect 既有 ASR 环境后，选取能提供直接音视频资源的平台 URL 进行端到端复测。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-08-03/2026-08-03-第5轮-DigCrawl-Skill-ASR-P1生产复验报告.md` | 本轮真实 URL 验收记录。 |
| 第 4 轮复测报告 | `tests/2026-08-03/2026-08-03-第4轮-DigCrawl-Skill-ASR错误语义复测报告.md` | 包含代码回归与修复细节。 |

## 9. 最终说明

本轮以真实图片和视频 URL 复验了用户关心的 P1：它已经收掉。当前仍不能宣称真实 ASR、OCR 或视频摘要模型已生产就绪；这些能力需要在现有 Collect 运行依赖与相应 Provider 配置完整后单独验收。
