# 2026-08-25 YouTube 视频采集接入与通用视频 OCR 边界

## 本次更新

- 新增 `youtube.video.read`：支持公开视频、Shorts、嵌入页、直播页和 `youtu.be` 短链的 URL 归一化与本地浏览器读取。
- 输出标题、简介、频道、发布时间、封面和可公开读取的字幕；不导出短期签名播放 URL。
- 增加 Collect URL 路由、配置白名单，以及 Watchlist 的 `youtube_video` 和 `youtube_channel` 发现、规范化与 `youtube:<video_id>` 去重。
- 页面必须提供与请求 URL 一致的 `ytInitialPlayerResponse.videoId` 才会返回成功；播放器未初始化会进入现有有限重试，避免把加载页或错误页误报为视频内容。
- 字幕读取为增强步骤：最多尝试三个轨道并共享八秒总预算；字幕端点不可用时保留页面元数据并记录警告。

## 视频 ASR 与 OCR 的归属

- YouTube provider 当前不下载或输出播放媒体 URL，因此不会直接触发 ASR 或视频帧 OCR；它优先使用 YouTube 的公开字幕轨。
- 视频帧 OCR 是 `main` 已有的通用 Collect 服务，而非微信视频号专属功能。任意 provider 只要输出可下载的 `type=video` 媒体，并显式启用 `Collect.DownloadMedia`、`OCR.Enabled` 与 `OCR.VideoFrames.Enabled`，都会复用“下载本地视频 → ffmpeg 抽帧 → OCR provider 识别”的同一条链路。
- 视频号本地 `capture --transcribe` 目前直接复用通用 ASR；若后续要做抽帧 OCR，应接入上述既有 Collect/OCR 服务，而不新增视频号专用 OCR。

## 验证

- `go test ./...`
- `go vet ./...`
- YouTube URL 归一化、错误页/未初始化身份校验、字幕 URL 脱敏与超时边界、Collect 路由、Watchlist 发现和配置白名单均有自动化测试。
