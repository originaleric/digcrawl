package platformprov

import (
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/platform/zhihu"
	"github.com/originaleric/digcrawl/runtime/artifact"
	"github.com/originaleric/digcrawl/runtime/registry"
)

var zhihuDefaultDomains = []string{"zhihu.com", "www.zhihu.com", "zhuanlan.zhihu.com"}

func ZhihuContentReadEntry(configDomains []string, artStore artifact.Store, runtimeCfg ...*config.Config) registry.Entry {
	return platformReadEntryWithConfig(
		ToolZhihuContentRead,
		"Read a Zhihu answer, article or video page via local browser with structured output.",
		[]string{"browser", "zhihu", "cookie.local", "platform.read"},
		zhihuDefaultDomains,
		configDomains,
		artStore,
		zhihu.ReadContent,
		optionalRuntimeConfig(runtimeCfg),
	)
}
