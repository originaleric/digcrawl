package toutiao

import (
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName     = "toutiao"
	ContentType      = "article"
	VideoContentType = "video"
	CookieDomain     = "www.toutiao.com"
)

const (
	DefaultWaitSelector    = "article, main, .article-content, .tt-article-content, #article"
	DefaultContentSelector = "article, .article-content, .tt-article-content, main, #article"
)

var ArticleBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: DefaultWaitSelector, ContentSelector: DefaultContentSelector},
	{WaitSelector: "article", ContentSelector: "article"},
	{WaitSelector: ".article-content", ContentSelector: ".article-content"},
	{WaitSelector: ".tt-article-content", ContentSelector: ".tt-article-content"},
	{WaitSelector: "main", ContentSelector: "main"},
}

const (
	VideoWaitSelector    = "video, #root, body"
	VideoContentSelector = "body"
)

var VideoBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: "video", ContentSelector: "body"},
	{WaitSelector: "#root", ContentSelector: "#root"},
	{WaitSelector: "body", ContentSelector: "body"},
}
