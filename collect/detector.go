package collect

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/originaleric/digcrawl/internal/feed"
	"github.com/originaleric/digcrawl/internal/platform/instagram"
	"github.com/originaleric/digcrawl/internal/platform/reddit"
	"github.com/originaleric/digcrawl/internal/platform/threads"
	browserprov "github.com/originaleric/digcrawl/provider/browser"
	platformprov "github.com/originaleric/digcrawl/provider/platform"
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
	{platform: "douyin", tool: platformprov.ToolDouyinVideoRead, hosts: []string{"douyin.com", "www.douyin.com", "iesdouyin.com", "v.douyin.com"}},
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
	if feed.IsFeedURL(rawURL) {
		return Match{Platform: feed.PlatformName, Tool: platformprov.ToolFeedRead}, nil
	}
	if reddit.IsRedditHost(host) && reddit.IsPostURL(rawURL) {
		return Match{Platform: reddit.PlatformName, Tool: platformprov.ToolRedditPostRead}, nil
	}
	if threads.IsPostURL(rawURL) {
		return Match{Platform: threads.PlatformName, Tool: platformprov.ToolThreadsPostRead}, nil
	}
	if instagram.IsMediaURL(rawURL) {
		return Match{Platform: instagram.PlatformName, Tool: platformprov.ToolInstagramContentRead}, nil
	}
	if hostMatches(host, []string{"toutiao.com", "www.toutiao.com", "m.toutiao.com"}) {
		path := strings.ToLower(u.EscapedPath())
		if strings.Contains(path, "/video/") {
			return Match{Platform: "toutiao", Tool: platformprov.ToolToutiaoVideoRead}, nil
		}
		return Match{Platform: "toutiao", Tool: platformprov.ToolToutiaoArticleRead}, nil
	}
	if isBilibiliVideoHostPath(host, u.EscapedPath()) {
		return Match{Platform: "bilibili", Tool: platformprov.ToolBilibiliVideoRead}, nil
	}
	if isWeiboPostHostPath(host, u.EscapedPath()) {
		return Match{Platform: "weibo", Tool: platformprov.ToolWeiboPostRead}, nil
	}
	if isKuaishouVideoHostPath(host, u.EscapedPath()) {
		return Match{Platform: "kuaishou", Tool: platformprov.ToolKuaishouVideoRead}, nil
	}
	if isZhihuContentHostPath(host, u.EscapedPath()) {
		return Match{Platform: "zhihu", Tool: platformprov.ToolZhihuContentRead}, nil
	}
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

func isBilibiliVideoHostPath(host, path string) bool {
	path = strings.ToLower(path)
	return host == "b23.tv" || strings.HasSuffix(host, ".b23.tv") || ((host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com")) && strings.Contains(path, "/video/"))
}

func isWeiboPostHostPath(host, path string) bool {
	if !(host == "m.weibo.cn" || host == "weibo.com" || strings.HasSuffix(host, ".weibo.com")) {
		return false
	}
	path = strings.ToLower(path)
	if strings.Contains(path, "/detail/") || strings.Contains(path, "/status/") {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parts[0])) {
	case "u", "profile", "login", "signup", "search", "tv", "p":
		return false
	default:
		return true
	}
}

func isKuaishouVideoHostPath(host, path string) bool {
	path = strings.ToLower(path)
	return host == "v.kuaishou.com" || ((host == "kuaishou.com" || strings.HasSuffix(host, ".kuaishou.com")) && (strings.Contains(path, "/short-video/") || strings.Contains(path, "/photo/")))
}

func isZhihuContentHostPath(host, path string) bool {
	path = strings.ToLower(path)
	return (host == "zhihu.com" || strings.HasSuffix(host, ".zhihu.com")) && (strings.Contains(path, "/question/") || strings.Contains(path, "/p/") || strings.Contains(path, "/zvideo/"))
}
