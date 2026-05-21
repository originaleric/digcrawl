package collect

import (
	"fmt"
	"net/url"
	"strings"

	browserprov "github.com/originaleric/diggate/provider/browser"
	platformprov "github.com/originaleric/diggate/provider/platform"
)

// Match maps a URL to a platform label and gateway tool name.
type Match struct {
	Platform string
	Tool     string
}

type urlRule struct {
	platform string
	tool     string
	hosts    []string
}

var rules = []urlRule{
	{platform: "wechat", tool: platformprov.ToolWechatArticleRead, hosts: []string{"mp.weixin.qq.com", "weixin.qq.com"}},
	{platform: "xiaohongshu", tool: platformprov.ToolXiaohongshuNoteRead, hosts: []string{"xiaohongshu.com", "www.xiaohongshu.com", "xhslink.com"}},
	{platform: "douyin", tool: platformprov.ToolDouyinVideoRead, hosts: []string{"douyin.com", "www.douyin.com", "iesdouyin.com"}},
	{platform: "x", tool: platformprov.ToolXPostRead, hosts: []string{"x.com", "twitter.com", "www.twitter.com"}},
}

// Detect picks the platform tool for a content URL.
func Detect(rawURL string) (Match, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return Match{}, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return Match{}, fmt.Errorf("invalid url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	for _, r := range rules {
		if hostMatches(host, r.hosts) {
			return Match{Platform: r.platform, Tool: r.tool}, nil
		}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "http" || scheme == "https" {
		return Match{Platform: "web", Tool: browserprov.ToolBrowserBrowse}, nil
	}
	return Match{}, fmt.Errorf("unsupported url %q", rawURL)
}

func hostMatches(host string, allowed []string) bool {
	for _, h := range allowed {
		h = strings.ToLower(h)
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}
