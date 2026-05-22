package platformprov

import (
	"github.com/originaleric/digcrawl/internal/platform/wechat"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var wechatDefaultDomains = []string{"mp.weixin.qq.com", "weixin.qq.com"}

// WechatArticleReadEntry returns registry entry for wechat.article.read.
func WechatArticleReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolWechatArticleRead,
		"Read a WeChat official account article via local browser with cookie reuse.",
		[]string{"browser", "wechat", "cookie.local", "platform.read"},
		wechatDefaultDomains,
		configDomains,
		artStore,
		wechat.ReadArticle,
	)
}
