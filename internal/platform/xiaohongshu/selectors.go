package xiaohongshu

const (
	PlatformName = "xiaohongshu"
	ContentType  = "note"
	CookieDomain = "www.xiaohongshu.com"
)

// DefaultWaitSelector 笔记页主内容等待选择器（页面变更时可集中调整）。
const DefaultWaitSelector = "#detail-desc, .note-content, .interaction-container"

// DefaultContentSelector 正文容器选择器。
const DefaultContentSelector = "#detail-desc, .note-content, .content"

// MetadataScript 提取标题、作者、互动与图片引用。
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
  const title = pick(['#detail-title', '.title', 'meta[property="og:title"]']);
  const author = pick(['.username', '.author-wrapper .name', '.user-name']);
  const likes = num(['.like-wrapper .count', '[class*="like"] .count']);
  const comments = num(['.chat-wrapper .count', '[class*="comment"] .count']);
  const collects = num(['.collect-wrapper .count', '[class*="collect"] .count']);
  const imgs = Array.from(document.querySelectorAll('.note-content img, .swiper-slide img, img'))
    .map(i => i.src || i.getAttribute('data-src'))
    .filter(Boolean);
  const tags = Array.from(document.querySelectorAll('a.tag, .tag-item'))
    .map(el => (el.innerText || '').trim())
    .filter(Boolean);
  return {
    title: title || document.title || '',
    author_name: author,
    likes, comments, bookmarks: collects,
    image_urls: [...new Set(imgs)].slice(0, 20),
    tags
  };
})()`
}
