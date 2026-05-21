package douyin

const (
	PlatformName = "douyin"
	ContentType  = "video"
	CookieDomain = "www.douyin.com"
)

const (
	DefaultWaitSelector    = "video, .video-info-detail, #root"
	DefaultContentSelector = ".video-info-detail, .account-card, #root"
)

// MetadataScript 提取抖音视频页文案、作者与互动数据。
func MetadataScript() string {
	return `(() => {
  const pick = (sels) => {
    for (const s of sels) {
      const el = document.querySelector(s);
      if (el) {
        const t = (el.innerText || el.textContent || el.content || '').trim();
        if (t) return t;
      }
    }
    return '';
  };
  const num = (sels) => {
    const raw = pick(sels).replace(/[^0-9]/g, '');
    return raw ? parseInt(raw, 10) : 0;
  };
  const title = pick(['h1', '.title', 'meta[property="og:title"]']);
  const desc = pick(['.video-info-detail span', '[data-e2e="video-desc"]', 'meta[property="og:description"]']);
  const author = pick(['.account-name', '[data-e2e="video-author-name"]', '.author-name']);
  const cover = document.querySelector('meta[property="og:image"]')?.content
    || document.querySelector('video')?.poster
    || '';
  return {
    title: title || document.title || '',
    description: desc,
    author_name: author,
    likes: num(['[data-e2e="like-count"]', '.like-count']),
    comments: num(['[data-e2e="comment-count"]', '.comment-count']),
    shares: num(['[data-e2e="share-count"]', '.share-count']),
    cover_url: cover || ''
  };
})()`
}
