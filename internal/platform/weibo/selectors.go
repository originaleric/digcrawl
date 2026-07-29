package weibo

const (
	DefaultWaitSelector    = "body"
	DefaultContentSelector = "body"
)

func MetadataScript() string {
	return `() => {
  const textOf = (sel) => document.querySelector(sel)?.textContent?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const title = textOf('.weibo-text') || textOf('[class*=weibo-text]') || document.title || meta('og:title');
  const description = meta('description') || textOf('.weibo-text') || textOf('[class*=detail]');
  const author = textOf('.m-text-cut') || textOf('[class*=nickname]') || textOf('[class*=author]');
  const published = textOf('[class*=time]') || textOf('.time');
  const imageUrls = Array.from(document.images).map(img => img.currentSrc || img.src).filter(Boolean);
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {title, description, author_name: author, published_at: published, image_urls: imageUrls, video_urls: videoUrls, extraction_source: 'browser'};
}`
}
