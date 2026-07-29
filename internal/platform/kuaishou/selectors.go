package kuaishou

const (
	DefaultWaitSelector    = "body"
	DefaultContentSelector = "body"
)

func MetadataScript() string {
	return `() => {
  const textOf = (sel) => document.querySelector(sel)?.textContent?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const title = textOf('h1') || textOf('[class*=title]') || document.title || meta('og:title');
  const description = meta('description') || textOf('[class*=desc]') || textOf('[class*=caption]');
  const author = textOf('[class*=user-name]') || textOf('[class*=author]') || textOf('[class*=profile]');
  const published = textOf('[class*=time]') || textOf('[class*=date]');
  const cover = meta('og:image');
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {title, description, author_name: author, published_at: published, cover_url: cover, video_urls: videoUrls, extraction_source: 'browser'};
}`
}
