package threads

func MetadataScript() string {
	return `() => {
  const textOf = (sel, root = document) => root.querySelector(sel)?.textContent?.trim() || '';
  const attr = (sel, name, root = document) => root.querySelector(sel)?.getAttribute(name)?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const body = document.body?.innerText || '';
  if (body.includes('Try again later') || body.includes('temporarily blocked')) {
    return {error_code: 'RATE_LIMITED', error_message: 'Threads request appears rate limited', extraction_source: 'page_state'};
  }
  const article = document.querySelector('article') || document.querySelector('main') || document.body;
  const title = meta('og:title') || document.title || textOf('h1', article);
  const desc = meta('og:description') || textOf('article', article) || textOf('main');
  if (!desc && body.includes('Log in') && body.includes('Threads')) {
    return {error_code: 'LOGIN_REQUIRED', error_message: 'Threads page requires login', extraction_source: 'page_state'};
  }
  const author = textOf('a[href^="/@"]', article) || (location.pathname.match(/^\/@([^/]+)/)?.[1] || '');
  const published = attr('time', 'datetime', article) || meta('article:published_time');
  const imageUrls = Array.from(document.images).map(img => img.currentSrc || img.src).filter(Boolean);
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {
    title,
    description: desc,
    author_name: author.replace(/^@/, ''),
    published_at: published,
    image_urls: [...new Set(imageUrls)].slice(0, 8),
    video_urls: [...new Set(videoUrls)].slice(0, 4),
    extraction_source: 'browser'
  };
}`
}
