# 2026-08-03-第4轮-DigCrawl-Skill-ASR错误语义复测报告

## 1. 测试结论

- 结论：通过（第 3 轮 P1 已关闭）。
- 测试时间：2026-08-03（Asia/Shanghai）。
- 测试人：Codex。
- 验收目标：真实视频 URL 在显式 `include_asr:true` 未获得可转写音视频时，必须返回可供 MCP 客户端消费的明确失败，不能静默成功。

## 2. 修复说明

Skill 在完成既有 ASR 尝试后，仅对调用方显式请求 ASR 的情形检查 `asr_text`。未得到转写时，复用既有 `platform_metadata.asr_error`；没有更具体原因时返回 `no downloaded video/audio asset was available for ASR`。未新增配置、领域或抽象。

视频摘要原有的“没有转写即失败”规则保持不变；显式 ASR 与摘要同时请求时，ASR 失败会更早、以同样清晰的原因终止。

## 3. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 针对性回归 | `go test ./skill ./provider/platform ./provider/browser ./transport/mcp` | 通过 | Skill 单测覆盖无音视频资产的显式 ASR 请求。 |
| 全量回归 | `go test ./...` | 通过 | 全部 Go 包通过。 |
| 静态检查 | `go vet ./...`、`git diff --check` | 通过 | 无静态问题或空白错误。 |
| 真实图片 URL | `browser.browse`：`https://httpbin.org/image/png`，`include_ocr:true` | 通过 | MCP 成功，产出截图 artifact 与 `ocr_count: 1`；Provider 为测试 stub。 |
| 真实视频 URL | `bilibili.video.read`：`https://www.bilibili.com/video/BV1xx411c7mD`，`include_media:true`、`include_asr:true` | 通过 | MCP 返回 `isError: true`、`TOOL_EXEC_FAILED`，错误为 `ASR unavailable: no downloaded video/audio asset was available for ASR`。 |

## 4. P1 关闭证据

| 第 3 轮失败行为 | 修复后行为 |
| --- | --- |
| MCP 成功，且既无 `asr_text` 也无错误 | MCP 明确失败，并保留已抓取的视频元数据作为结构化上下文 |
| Agent 无法判断是否可继续生成视频摘要 | Agent 可依据 `TOOL_EXEC_FAILED` 与具体原因停止或改用可下载媒体源 |

## 5. 残余限制

真实 OCR Provider、Collect 指定的本地 ASR Python 环境与 `faster_whisper` 仍未就绪，所以本轮验证的是“失败语义完整”，而不是实际语音转写质量。依赖补齐后，仍需使用含可下载音视频资源的真实 URL 复测 ASR 和视频摘要。

## 6. 产物

| 产物 | 路径 |
| --- | --- |
| 本报告 | `tests/2026-08-03/2026-08-03-第4轮-DigCrawl-Skill-ASR错误语义复测报告.md` |
| 上轮失败记录 | `tests/2026-08-03/2026-08-03-第3轮-DigCrawl-Skill-媒体能力生产模拟测试报告.md` |
