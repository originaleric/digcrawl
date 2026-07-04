# 2026-07-04 GLM-OCR 全库接入与智谱 Provider 修复同步

## 背景

本次更新围绕全库 OCR 能力落地与智谱 GLM-OCR provider 做完整闭环：配置、平台读取、浏览器读取、Collect 导出、媒体下载、本地 assets、真实智谱 OCR 生产验证和文档同步。

## 主要变更

- 新增全局 `OCR` 配置块，支持 `Enabled`、`Provider`、`APIKey`、`BaseURL`、`Model`、超时、图片数量、图片大小和追加输出控制。
- 新增 `internal/ocr` OCR enrichment 模块，统一把 OCR 结果写入 `platform_metadata.ocr_results` / `ocr_count` / `ocr_error`。
- 支持 `openai_compatible`、`zhipu_ocr` 和 `stub` provider；`glm_ocr` / `glm-ocr` 会归一为 `zhipu_ocr`。
- `zhipu_ocr` 远程图片走智谱 `layout_parsing` / `glm-ocr`，本地 assets 和截图走智谱 `files/ocr` 上传接口。
- 平台工具与浏览器工具在直接读取时会把截图作为显式 OCR 候选；Collect 导出时会在下载媒体后优先对本地 `.assets` 图片做 OCR。
- 修复本地 asset 扩展名问题：下载图片时根据 `Content-Type` / 文件内容修正扩展名，避免 PNG 被错误保存为 `001.50` 导致智谱 `files/ocr` 返回 HTTP 400。
- OCR 上传本地文件时也会按文件内容 sniff 真实图片类型，并修正 multipart filename，兼容历史坏扩展名文件。

## 文档同步

- `README.md` 增加 OCR provider 与智谱 GLM-OCR 能力说明。
- `USAGE.md` 增加 `OCR` 配置字段、`zhipu_ocr` provider、智谱默认 BaseURL / Model 和 YAML 示例。
- `collect/README.md` 增加 Collect 下载媒体后优先 OCR 本地 assets 的说明。
- `config/config.yaml` 增加 OCR 默认配置示例。

## 测试与验证

- `GOCACHE=/private/tmp/digcrawl-gocache go test ./collect ./internal/ocr`
- `GOCACHE=/private/tmp/digcrawl-gocache go test ./...`
- 使用真实智谱 API key 做 GLM-OCR 生产级测试，报告中仅保留脱敏 key。
- 第2轮生产测试发现本地文件扩展名问题；修复后第3轮重测 5 张公网图片、10 个 OCR case，10/10 通过。
- 明文 key 扫描通过，测试产物中未发现完整 API key。

## 测试报告

- `tests/2026-07-04/2026-07-04-智谱OCR生产级测试报告.md`
- `tests/2026-07-04/2026-07-04-第1轮-GLM-OCR生产级测试报告.md`
- `tests/2026-07-04/2026-07-04-第2轮-GLM-OCR生产级测试报告.md`
- `tests/2026-07-04/2026-07-04-第3轮-GLM-OCR扩展名修复验证报告.md`

## 当前状态

GLM-OCR provider 已具备落地条件。剩余风险主要来自第三方接口限流、源站图片下载拒绝、provider 服务波动或特殊图片格式限制，系统会把这些错误写入 OCR metadata，避免静默失败。
