package x

import (
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "x"
	ContentType  = "post"
	CookieDomain = "x.com"
)

const (
	DefaultWaitSelector    = "article[data-testid='tweet'], [data-testid='tweet']"
	DefaultContentSelector = "article[data-testid='tweet'], [data-testid='tweetText']"
)

// PostBrowseAttempts 单帖读取兜底（不展开 thread）。
var PostBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: "article[data-testid='tweet']", ContentSelector: "article[data-testid='tweet']"},
	{WaitSelector: "[data-testid='tweetText']", ContentSelector: "[data-testid='tweetText']"},
	{WaitSelector: "main", ContentSelector: "main"},
}

// MetadataScript 登录墙/限流识别 + 首条 tweet 提取。
func MetadataScript() string {
	return platform.WithParseCountJS(`() => {
  const warnings = ['thread_not_expanded'];
  const pick = (sels) => {
    for (const s of sels) {
      const el = document.querySelector(s);
      if (el) {
        const t = (el.innerText || el.textContent || '').trim();
        if (t) return t;
      }
    }
    return '';
  };
  const detectPageState = () => {
    const body = (document.body?.innerText || '').slice(0, 5000);
    const path = location.pathname || '';
    if (['Log in', 'Sign in', '登录', 'Something went wrong'].some(h => body.includes(h))
        && document.querySelector('[data-testid="login"], form[action*="login"]')) {
      return { error_code: 'LOGIN_REQUIRED', error_message: '需要登录 X' };
    }
    if (body.includes('Rate limit') || body.includes('rate limit exceeded')) {
      return { error_code: 'RATE_LIMITED', error_message: '触发访问频率限制' };
    }
    if (body.includes('These Tweets are protected') || body.includes('账号受保护')) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '受保护账号内容' };
    }
    if (body.includes('This Post is from an account that no longer exists') || body.includes('doesn’t exist')) {
      return { error_code: 'CONTENT_DELETED', error_message: '帖子不存在或已删除' };
    }
    if (body.includes('Sensitive content') || body.includes('敏感内容')) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '敏感内容需登录确认' };
    }
    if (body.includes('Unavailable in your country') || body.includes('地区')) {
      return { error_code: 'REGION_RESTRICTED', error_message: '地区限制' };
    }
    if (!path.includes('/status/')) {
      warnings.push('not_a_status_url');
    }
    return {};
  };

  const tryMeta = () => {
    const title = document.querySelector('meta[property="og:title"]')?.content || '';
    const desc = document.querySelector('meta[property="og:description"]')?.content || '';
    if (!title && !desc) return null;
    return { extraction_source: 'meta', title, text: desc };
  };

  const tryDOM = () => {
    const article = document.querySelector('article[data-testid="tweet"]');
    const scope = article || document;
    const text = pick(['[data-testid="tweetText"]', 'article div[lang]']);
    const author = pick(['[data-testid="User-Name"] a', '[data-testid="User-Names"] span']);
    const time = scope.querySelector('time')?.getAttribute('datetime') || '';
    const imgs = Array.from(scope.querySelectorAll('img[src*="pbs.twimg"]'))
      .map(i => i.src).filter(Boolean);
    const quote = scope.querySelector('[data-testid="quoteTweet"]');
    if (quote) warnings.push('quote_tweet_present');
    return {
      extraction_source: 'dom',
      text,
      author_name: author,
      published_at: time,
      likes: parseCount(pick(['[data-testid="like"]', 'button[data-testid="like"]'])),
      reposts: parseCount(pick(['[data-testid="retweet"]'])),
      comments: parseCount(pick(['[data-testid="reply"]'])),
      bookmarks: parseCount(pick(['[data-testid="bookmark"]'])),
      image_urls: [...new Set(imgs)].slice(0, 4),
    };
  };

  const state = detectPageState();
  if (state.error_code) return { ...state, warnings, extraction_source: 'page_state' };

  let merged = null;
  for (const fn of [tryMeta, tryDOM]) {
    const part = fn();
    if (!part) continue;
    merged = merged ? { ...merged, ...part, warnings } : { ...part, warnings };
  }
  if (!merged || !merged.text) {
    return {
      error_code: 'SELECTOR_STALE',
      error_message: '未能提取帖子正文',
      warnings,
      extraction_source: 'none',
    };
  }
  return merged;
}`)
}
