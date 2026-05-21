package x

const (
	PlatformName = "x"
	ContentType  = "post"
	CookieDomain = "x.com"
)

const (
	DefaultWaitSelector    = "article[data-testid='tweet'], [data-testid='tweet']"
	DefaultContentSelector = "article[data-testid='tweet'], [data-testid='tweetText']"
)

// MetadataScript 提取 X/Twitter 帖子元数据。
func MetadataScript() string {
	return `(() => {
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
  const num = (sels) => {
    const raw = pick(sels).replace(/[^0-9]/g, '');
    return raw ? parseInt(raw, 10) : 0;
  };
  const text = pick(['[data-testid="tweetText"]', 'article div[lang]']);
  const author = pick(['[data-testid="User-Name"] a', '[data-testid="User-Names"] span']);
  const time = document.querySelector('time')?.getAttribute('datetime') || '';
  const imgs = Array.from(document.querySelectorAll('article img[src*="pbs.twimg"]'))
    .map(i => i.src).filter(Boolean);
  return {
    text,
    author_name: author,
    published_at: time,
    likes: num(['[data-testid="like"]', 'button[data-testid="like"]']),
    reposts: num(['[data-testid="retweet"]']),
    comments: num(['[data-testid="reply"]']),
    bookmarks: num(['[data-testid="bookmark"]']),
    image_urls: [...new Set(imgs)].slice(0, 4)
  };
})()`
}
