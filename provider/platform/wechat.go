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

// WechatChannelsReadEntry reads a public WeChat Channels share page without
// connecting to the desktop client or importing a stored WeChat session.
func WechatChannelsReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolWechatChannelsRead,
		"Read a public WeChat Channels share URL through a local browser without desktop WeChat or cookie reuse.",
		[]string{"browser", "wechat_channels", "platform.read"},
		wechatDefaultDomains,
		configDomains,
		artStore,
		wechatchannels.ReadShare,
	)
}
