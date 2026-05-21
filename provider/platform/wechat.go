package platformprov

import (
	"github.com/originaleric/diggate/runtime/artifact"
	"github.com/originaleric/diggate/runtime/registry"
	"github.com/originaleric/diggate/internal/platform/wechat"
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
