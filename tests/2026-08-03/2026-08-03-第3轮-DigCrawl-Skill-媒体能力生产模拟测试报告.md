# 2026-08-03-第3轮-DigCrawl-Skill-媒体能力生产模拟测试报告

## 1. 测试结论

- 结论：失败。
- 项目：DigCrawl 本地 MCP Skill 的 OCR、ASR 与视频摘要前置链路。
- 测试时间：2026-08-03（Asia/Shanghai）。
- 测试人：Codex。
- 本轮目标：将 Collect 的 OCR/ASR 运行配置同步到 Skill profile，并以真实图片、视频 URL 验证 MCP 调用链路。

真实图片 URL 的浏览器、截图、OCR 配置注入链路通过；真实 B 站视频 URL 的元数据读取也通过。但显式请求 `include_asr` 时，若平台输出中没有可下载的视频或音频资源，MCP 仍返回成功，且既没有 `asr_text`，也没有诊断错误。这使调用方误以为转写已完成，属于 P1。

此外，当前本机没有可用于真实推理的 OCR Provider 凭据，Collect 配置指定的本地 ASR Python 命令及 `faster_whisper` 依赖也不可用。因此本轮不能把 stub OCR 的接线成功表述为真实 OCR 模型验证，也不能完成真实 ASR 或视频摘要推理验证。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；由当前工作区构建的临时 `digcrawl` 二进制 |
| 启动入口 | `digcrawl mcp --config <临时媒体测试配置>` |
| 配置来源 | 根级 `config/config.yaml` 的 OCR/ASR 运行字段同步到 `config/digcrawl-skill.example.yaml` 的 `Skill.OCR`、`Skill.ASR` |
| Skill 开关 | OCR、ASR 继续保持显式 opt-in；本轮临时 profile 仅为接线验证启用它们 |
| 外部依赖 | 真实公开网络、本地 Chromium、B 站公开接口、FFmpeg |

## 3. 配置与依赖就绪性

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| Skill OCR 配置同步 | configured | `Skill.OCR` 与 Collect 的根级 `OCR` 完全一致。 |
| Skill ASR 配置同步 | configured | `Skill.ASR` 与根级 `ASR` 的运行字段一致；仅 `Enabled` 保持 Skill opt-in。 |
| 配置回归测试 | passed | `go test ./config` 通过，断言上述同步关系。 |
| FFmpeg | ready | 本机可用。 |
| 真实 OCR Provider | missing | 根级 OCR 未启用且无 Provider 凭据；不得伪造真实 OCR 验证。 |
| 本地 ASR 命令 | missing | Collect 配置的 `.venv-asr/bin/python` 不可用。 |
| `faster_whisper` | missing | 当前可用 Python 环境未安装该依赖。 |
| 视频摘要 LLM | not-ready | 其前提 ASR 尚未获得可用转写，且本轮未配置真实 LLM 调用。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 配置同步回归 | `go test ./config` | 通过 | Skill OCR 全量相等；Skill ASR 除显式开关外相等。 |
| MCP 二进制构建 | `go build ./cmd/digcrawl` | 通过 | 使用当前源码启动测试。 |
| 真实图片 URL | `browser.browse`：`https://httpbin.org/image/png`，`include_ocr:true` | 接线通过 | 返回 14,011 bytes 截图 artifact、`ocr_count: 1`；临时 profile 为 `stub`，非真实模型验证。 |
| 真实视频 URL | `bilibili.video.read`：`https://www.bilibili.com/video/BV1xx411c7mD`，`include_media:true`、`include_asr:true` | 失败 | MCP 成功返回视频元数据，但媒体仅有 `image`，无视频/音频；也无转写或错误。 |
| 输出脱敏归档 | MCP 结果摘要 | 通过 | 已保存结构化摘要，不含密钥、Cookie 或响应头。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 图片 URL 抓取 | 读取真实图片页面并产出本地浏览器截图 | 成功 | 通过 |
| OCR 配置注入 | `include_ocr` 使用 Skill profile | 成功调用 stub Provider | 接线通过 |
| 视频 URL 抓取 | 读取真实公开视频元数据 | 成功 | 通过 |
| 显式 ASR 请求 | 取得 `asr_text`，或返回明确错误 | 成功响应但两者皆无 | 失败 |
| 真实 OCR 推理 | 使用真实 Provider 与凭据执行 | Provider 未就绪 | 阻塞 |
| 真实 ASR/摘要推理 | 下载真实媒体并执行本地 ASR/LLM | 媒体与 ASR 运行环境均未就绪 | 阻塞 |

## 6. Findings

### [P1] 显式 ASR 无可转写媒体时被静默吞掉

- 位置：`skill/enrich.go:65`、`skill/enrich.go:138`。
- 失败模式：`Enhance` 调用 `enrichASRFromDownloadedMedia` 后不检查返回值；当 `downloaded` 为空，或其中没有 `video` / `audio` 类型时，函数只返回 `false`，不会写入 `asr_error`，上层依旧返回 `nil`。
- 生产证据：真实 `bilibili.video.read` 对 `BV1xx411c7mD` 的响应标记为 `content_type: video`，但 `media` 仅含图片。传入 `include_asr:true` 后 MCP 返回 success，`asr_text` 与 `platform_metadata.asr_error` 均不存在。详见同目录结果摘要。
- 影响：Agent/客户端无法区分“转写成功但为空”与“未发生转写”，可能继续基于不存在的转写做摘要或回答。
- 建议：在现有 Skill 编排边界上，对显式 ASR 在未产生转写时返回明确失败；不需要增加新领域或抽象。这也应覆盖媒体清单为空、没有视频/音频、下载失败或转写失败等已有失败分支。

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| 真实 OCR API | 无本地启用的 Provider 凭据 | 在用户配置真实 Provider 后，以同一真实图片 URL 复测识别质量与错误传播。 |
| 本地真实 ASR | Collect 指定的虚拟环境及 `faster_whisper` 缺失 | 补齐 Collect 已选定的本地运行环境后，以可下载的公开视频/音频 URL 复测。 |
| 视频摘要 | 依赖有效 `asr_text` | 先关闭本报告的 P1，再使用真实 LLM Provider 复测摘要。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-08-03/2026-08-03-第3轮-DigCrawl-Skill-媒体能力生产模拟测试报告.md` | 本轮生产模拟结论与 Finding。 |
| MCP 结果摘要 | `tests/2026-08-03/2026-08-03-第3轮-DigCrawl-Skill-媒体能力-MCP结果摘要.json` | 已脱敏的真实 URL 调用结果。 |
| 配置样例 | `config/digcrawl-skill.example.yaml` | 已同步 Collect OCR/ASR 运行配置。 |
| 配置测试 | `config/config_test.go` | 防止未来同步漂移。 |

## 9. 最终说明

本轮没有对发现的问题实施修复。真实 URL 验证已经证明配置复制与图片接线生效，但 ASR 的成功语义存在缺口，且真实模型运行依赖不完整；因此不能将媒体能力标记为生产就绪。
