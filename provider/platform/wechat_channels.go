package platformprov

import (
	"context"

	"github.com/originaleric/digcrawl/config"
	iplatform "github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
)

var resolveWechatShareVideo = wechatchannels.ResolveShareVideo
var downloadWechatShareVideo = (*wechatchannels.ShareVideo).Download

// resolveWechatChannelsVideo adapts a private playable reference to the local
// MP4 input used by the shared platform reader.
func resolveWechatChannelsVideo(ctx context.Context, in iplatform.ReadInput, cfg *config.Config, publicRead platformReader) (*iplatform.Content, localVideoDownload, error) {
	content, video, err := resolveWechatShareVideo(ctx, in, cfg)
	if err != nil {
		return nil, nil, err
	}
	if content == nil || video == nil {
		return nil, nil, iplatform.NewReadError(iplatform.ErrPartialContent, "视频号未返回可下载的 MP4", wechatchannels.PlatformName, nil)
	}
	if in.IncludeScreenshot {
		page, err := publicRead(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		if page == nil || page.ScreenshotBase64 == "" {
			return nil, nil, iplatform.NewReadError(iplatform.ErrPartialContent, "视频号分享页未返回截图", wechatchannels.PlatformName, nil)
		}
		content.ScreenshotBase64 = page.ScreenshotBase64
		if len(content.Media) == 0 {
			content.Media = page.Media
		}
	}
	return content, func(ctx context.Context, maxBytes int64, retain bool, dir string) (string, func(), error) {
		if !retain {
			dir = ""
		}
		return downloadWechatShareVideo(video, ctx, maxBytes, dir)
	}, nil
}
