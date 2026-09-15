package platformprov

// Gateway tool names for platform content reads (used by collect detector).
const (
	ToolWechatArticleRead    = "wechat.article.read"
	ToolXiaohongshuNoteRead  = "xiaohongshu.note.read"
	ToolDouyinVideoRead      = "douyin.video.read"
	ToolXPostRead            = "x.post.read"
	ToolToutiaoArticleRead   = "toutiao.article.read"
	ToolToutiaoVideoRead     = "toutiao.video.read"
	ToolRedditPostRead       = "reddit.post.read"
	ToolRedditSubredditRead  = "reddit.subreddit.read"
	ToolRedditUserRead       = "reddit.user.read"
	ToolThreadsPostRead      = "threads.post.read"
	ToolInstagramContentRead = "instagram.content.read"
	ToolBilibiliVideoRead    = "bilibili.video.read"
	ToolWeiboPostRead        = "weibo.post.read"
	ToolKuaishouVideoRead    = "kuaishou.video.read"
	ToolZhihuContentRead     = "zhihu.content.read"
	ToolYouTubeVideoRead     = "youtube.video.read"
	ToolFeedRead             = "feed.read"

	// ProviderVideoDownloadFailedCode identifies a failed optional video
	// retention after the platform content itself was read successfully. Callers
	// can preserve that successful content without treating arbitrary read
	// failures as recoverable.
	ProviderVideoDownloadFailedCode = "PROVIDER_VIDEO_DOWNLOAD_FAILED"
	// ProviderImageDownloadFailedCode identifies a failed optional image
	// retention after the content itself was read successfully.
	ProviderImageDownloadFailedCode = "PROVIDER_IMAGE_DOWNLOAD_FAILED"
)
