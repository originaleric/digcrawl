package bilibili

const (
	DefaultWaitSelector    = "body"
	DefaultContentSelector = "body"
)

func MetadataScript() string {
	return `() => {
  const textOf = (sel) => document.querySelector(sel)?.textContent?.trim() || '';
  const attr = (sel, name) => document.querySelector(sel)?.getAttribute(name)?.trim() || '';
  const meta = (name) => document.querySelector(` + "`meta[name=\"${name}\"], meta[property=\"${name}\"]`" + `)?.content?.trim() || '';
  const title = textOf('h1.video-title') || textOf('.video-title') || textOf('h1') || document.title || meta('og:title');
  const description = meta('description') || textOf('.video-desc') || textOf('.desc-info-text') || textOf('[class*=desc]');
  const author = textOf('.up-name') || textOf('.username') || textOf('[class*=up-info] a') || textOf('[class*=author]');
  const published = textOf('.pubdate-text') || textOf('.pubdate-ip-text') || textOf('[class*=pubdate]');
  const cover = meta('og:image') || attr('link[itemprop=thumbnailUrl]', 'href');
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {
    title,
    description,
    author_name: author,
    published_at: published,
    cover_url: cover,
    video_urls: videoUrls,
    extraction_source: 'browser'
  };
}`
}
