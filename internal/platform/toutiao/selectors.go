package toutiao

import (
	"github.com/originaleric/diggate/internal/platform"
)

const (
	PlatformName = "toutiao"
	ContentType  = "article"
	CookieDomain = "www.toutiao.com"
)

const (
	DefaultWaitSelector    = "article, main, .article-content, .tt-article-content, #article"
	DefaultContentSelector = "article, .article-content, .tt-article-content, main, #article"
)

var ArticleBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: "article", ContentSelector: "article"},
	{WaitSelector: ".article-content", ContentSelector: ".article-content"},
	{WaitSelector: ".tt-article-content", ContentSelector: ".tt-article-content"},
	{WaitSelector: "main", ContentSelector: "main"},
}
