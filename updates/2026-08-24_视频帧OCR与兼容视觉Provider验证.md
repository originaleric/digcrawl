# 2026-08-24 视频帧 OCR 与兼容视觉 Provider 验证

## 本次更新

- 完成通用本地视频抽帧 OCR 的生产验证：对已下载的 MP4，DigCrawl 通过受限的 `ffmpeg` 抽帧，再使用 `openai_compatible` 视觉 provider 提取画面文字。
- 视频 OCR 与 ASR 保持独立：没有有效语音、只有 BGM 的视频仍可通过内嵌字幕和画面文字产出内容；两项能力可独立开启和失败。
- 确认智谱的 OpenAI-compatible 视觉模型可作为 OCR provider 使用。配置应保持 `Provider: openai_compatible`，并使用支持 Chat Completions 图像输入的视觉模型（例如 `glm-5v-turbo`）；不新增 `zhipu_ocr` provider。
- 修复无文字帧语义：当 provider 返回合法 `choices` 但没有文字时，结果是正常空帧，不再记为 provider 错误；它不计入 `ocr_count`，也不会产生空的 Markdown/Text OCR 段。
- 已有 OCR 结果后追加视频帧时，历史总计数与本批渲染计数分离：历史成功结果仍被保留，本批全为空时不会追加空段。

## 使用边界

- 该能力默认关闭，需同时启用 `OCR.Enabled`、`OCR.VideoFrames.Enabled` 和 Collect 的 `DownloadMedia`。
- `IntervalSec`、`MaxFrames`、`MaxTotalFrames` 和 `TimeoutSec` 共同限制抽帧频率、远程 OCR 成本与本地执行时间。
- 抽出的 JPEG 存在于私有临时目录，识别结束后删除；导出的 OCR 元数据不包含临时文件路径。
- 若使用远程视觉 provider，视频帧会发送到该 provider；请只在确认数据处理边界后启用，并将 API Key 保存在本地私有配置中。

## 验证

- 使用 60 秒、仅含 BGM 但有画面字幕的本地视频，按 10 秒间隔抽取 6 帧。
- 6 帧均返回有效 OCR 文本，未出现帧级 provider 错误，且未泄露临时帧路径。
- 覆盖空 `choices` 内容、无 `choices` 协议异常、首次空 OCR、已有结果后追加空帧、已有结果后追加非空帧等状态。
- `go test ./...`、`go vet ./...` 与 `git diff --check` 通过。
