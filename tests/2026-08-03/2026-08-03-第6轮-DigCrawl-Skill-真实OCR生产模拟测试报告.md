# 2026-08-03-第6轮-DigCrawl-Skill-真实OCR生产模拟测试报告

## 1. 测试结论

- 结论：通过。
- 项目：DigCrawl 本地 MCP Skill 的独立 OpenAI-compatible OCR。
- 测试时间：2026-08-03（Asia/Shanghai）。
- 测试人：Codex。
- 本轮核心输出要求：使用用户授权的本地 Provider 凭据，通过真实公开图片 URL 验证 Skill OCR 能产生可消费的非空文本；凭据不得进入 Git 或测试产物。

## 2. 项目与环境识别

| 项目 | 结果 |
| --- | --- |
| 语言/运行时 | Go；当前工作区构建的临时 `digcrawl` 二进制 |
| 启动入口 | `digcrawl mcp --config config/digcrawl-skill.local.yaml` |
| 测试命令 | MCP stdio 调用 `browser.browse`，传入真实图片 URL、`action:screenshot`、`include_ocr:true` |
| 配置文件 | 已忽略的 `config/digcrawl-skill.local.yaml`；未被 Git 跟踪 |
| 依赖服务 | 本地 Chromium、公开图片页、OpenAI-compatible 视觉模型 API |
| 核心业务输出 | `platform_metadata.ocr_results`、`ocr_count`、截图 artifact |

## 3. 配置检查

| 配置/依赖 | 状态 | 说明 |
| --- | --- | --- |
| 本地 Skill OCR profile | configured | 仅本机文件，已由 `.gitignore` 排除。 |
| Provider 协议 | configured | `openai_compatible`，调用 `/chat/completions`。 |
| Provider 凭据 | configured | 从用户指定的 Workspace 本地配置复制；本报告不记录其值。 |
| 视觉模型 | configured | `glm-5v-turbo`；本仓库既有真实 OpenAI-compatible OCR 验收曾使用该模型。 |
| 本地浏览器与 artifact store | configured | 本轮成功抓取图片并生成截图 artifact。 |
| Git 凭据保护 | passed | profile 命中 `.gitignore`，未被 Git 跟踪。 |

## 4. 执行记录

| 步骤 | 命令/API | 结果 | 关键证据 |
| --- | --- | --- | --- |
| 构建 | `go build -o /private/tmp/digcrawl-skill-ocr-prodtest ./cmd/digcrawl` | 通过 | 使用当前源码启动 MCP。 |
| 首次真实请求 | `glm-4.6v-flash`，`browser.browse` → `https://httpbin.org/image/png` | 外部模型不可用 | OCR 结果记录 HTTP 429；浏览器和 Skill 注入均成功，不作为代码 Finding。 |
| 既有模型复验 | `glm-5v-turbo`，同一 MCP 请求 | 通过 | MCP `isError:false`；`ocr_count:1`；Provider 为 `openai_compatible`；截图 artifact 存在。 |
| 文本质量最小验收 | 读取脱敏结构化结果 | 通过 | 唯一 OCR 结果无 error，文本非空，长度 111 字符。 |

## 5. 输出质量验收

| 验收项 | 预期 | 实际 | 结果 |
| --- | --- | --- | --- |
| 真实 Provider 调用 | 不是 stub，使用实际凭据 | `openai_compatible` 调用成功 | 通过 |
| 图片 OCR | 返回至少一条非空结果 | `ocr_count:1`，文本非空 | 通过 |
| MCP 可消费性 | 结构化输出包含 OCR 诊断与 artifact | 返回 `ocr_results` 和截图 artifact | 通过 |
| 凭据边界 | 不写入版本控制或报告 | 本地 profile 被忽略；报告不包含密钥 | 通过 |

## 6. Findings

本轮未发现代码或架构 Finding。

`glm-4.6v-flash` 的 HTTP 429 已通过使用仓库既有真实验收过的 `glm-5v-turbo` 复验排除为 Provider 模型可用性/配额问题，不影响 DigCrawl OpenAI-compatible OCR 链路的通过结论。

## 7. 跳过项与残余风险

| 项目 | 原因 | 后续建议 |
| --- | --- | --- |
| 多图片/文档版面质量 | 本轮只验证真实单图片 MCP 主链路 | 需要时使用表格、长图等样本做单独质量验收。 |
| Provider 限流重试 | 当前 Provider 返回 429 时会结构化记录单图错误，不会自动重试 | 若后续确有稳定性需求，再单独评估是否需要重试策略。 |

## 8. 产物清单

| 产物 | 路径 | 说明 |
| --- | --- | --- |
| 本报告 | `tests/2026-08-03/2026-08-03-第6轮-DigCrawl-Skill-真实OCR生产模拟测试报告.md` | 本轮真实 OCR 验收记录。 |
| 本地测试 profile | `config/digcrawl-skill.local.yaml` | 已忽略的本机凭据配置；不纳入版本控制。 |
| 临时 MCP 结果 | `/private/tmp/digcrawl-skill-ocr-prodtest-result-round2.json` | 仅本机临时文件，已按脱敏摘要验收。 |

## 9. 最终说明

用户要求的真实 OCR 验收已通过。DigCrawl 使用自身的 OpenAI-compatible OCR 客户端和本地 Skill profile，不调用 DigFlow/DigEino OCR 运行时；本次只复用了用户指定的本地 Provider 凭据。
