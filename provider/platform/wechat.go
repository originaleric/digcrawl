package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/wechat"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var wechatDefaultDomains = []string{"mp.weixin.qq.com", "weixin.qq.com"}

// WechatArticleReadEntry returns registry entry for wechat.article.read.
func WechatArticleReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolWechatArticleRead,
		"Read a WeChat official account article via local browser with cookie reuse.",
		[]string{"browser", "wechat", "cookie.local", "platform.read"},
		wechatDefaultDomains,
		configDomains,
		artStore,
		wechat.ReadArticle,
		optionalRuntimeConfig(runtimeCfg),
	)
}

// WechatChannelsReadEntry reads public page text and resolves requested videos
// using the local Yuanbao session.
func WechatChannelsReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolWechatChannelsRead,
		"Read a WeChat Channels share URL. Requested video media uses the local Yuanbao session; on LOGIN_REQUIRED, follow the login action for the active DigCrawl profile and retry.",
		[]string{"api", "browser", "wechat_channels", "cookie.local", "platform.read", "video"},
		wechatDefaultDomains,
		configDomains,
		artStore,
		wechatchannels.ReadShare,
		optionalRuntimeConfig(runtimeCfg),
		resolveWechatChannelsVideo,
	)
}
