package platformprov

import (
	"github.com/originaleric/digcrawl/internal/platform/weibo"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var weiboDefaultDomains = []string{"weibo.com", "www.weibo.com", "m.weibo.cn"}

func WeiboPostReadEntry(configDomains []string, artStore artifact.Store) registry.Entry {
	return platformReadEntry(
		ToolWeiboPostRead,
		"Read a Weibo post page API-first with local browser fallback and structured output.",
		[]string{"browser", "weibo", "cookie.local", "platform.read"},
		weiboDefaultDomains,
		configDomains,
		artStore,
		weibo.ReadPost,
	)
}
