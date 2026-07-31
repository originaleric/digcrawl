package instagram

func MetadataScript() string {
	return `() => {
  const textOf = (sel, root = document) => root.querySelector(sel)?.textContent?.trim() || '';
  const attr = (sel, name, root = document) => root.querySelector(sel)?.getAttribute(name)?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const body = document.body?.innerText || '';
  if (body.includes('Try Again Later') || body.includes('temporarily blocked') || body.includes('challenge_required')) {
    return {error_code: 'RATE_LIMITED', error_message: 'Instagram request appears rate limited or challenged', extraction_source: 'page_state'};
  }
  if (body.includes('Sorry, this page isn') || body.includes('Page Not Found') || body.includes("Post isn't available") || document.title.includes("isn't available")) {
    return {error_code: 'CONTENT_NOT_FOUND', error_message: 'Instagram content not found', extraction_source: 'page_state'};
  }
  const article = document.querySelector('article') || document.querySelector('main') || document.body;
  const title = meta('og:title') || document.title || textOf('h1', article);
  const desc = meta('og:description') || textOf('article', article) || textOf('main');
  if (!desc && body.includes('Log in') && body.includes('Instagram')) {
    return {error_code: 'LOGIN_REQUIRED', error_message: 'Instagram page requires login', extraction_source: 'page_state'};
  }
  const author = (location.pathname.match(/^\/([^/]+)\//)?.[1] || '').replace(/^p$|^reel$|^tv$/i, '') || textOf('header a[href^="/"]', article);
  const published = attr('time', 'datetime', article) || meta('article:published_time');
  const imageUrls = Array.from(document.images).map(img => img.currentSrc || img.src).filter(Boolean);
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {
    title,
    description: desc,
    author_name: author,
    published_at: published,
    image_urls: [...new Set(imageUrls)].slice(0, 8),
    video_urls: [...new Set(videoUrls)].slice(0, 4),
    extraction_source: 'browser'
  };
}`
}
