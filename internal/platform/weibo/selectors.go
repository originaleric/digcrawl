package weibo

const (
	DefaultWaitSelector    = "body"
	DefaultContentSelector = "body"
)

func MetadataScript() string {
	return `() => {
  const textOf = (sel) => document.querySelector(sel)?.textContent?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const body = document.body?.innerText || '';
  const pageTitle = document.title || '';
  if (pageTitle.includes('Sina Visitor System') || body.includes('Sina Visitor System') || body.includes('微博访客系统')) {
    return {error_code: 'LOGIN_REQUIRED', error_message: '微博页面进入访客系统，需要登录态或安全验证', extraction_source: 'page_state'};
  }
  if (body.includes('登录') && (body.includes('微博') || body.includes('Weibo')) && !textOf('.weibo-text') && !textOf('[class*=weibo-text]')) {
    return {error_code: 'LOGIN_REQUIRED', error_message: '微博页面需要登录后查看', extraction_source: 'page_state'};
  }
  const title = textOf('.weibo-text') || textOf('[class*=weibo-text]') || document.title || meta('og:title');
  const description = meta('description') || textOf('.weibo-text') || textOf('[class*=detail]');
  const author = textOf('.m-text-cut') || textOf('[class*=nickname]') || textOf('[class*=author]');
  const published = textOf('[class*=time]') || textOf('.time');
  const imageUrls = Array.from(document.images).map(img => img.currentSrc || img.src).filter(Boolean);
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {title, description, author_name: author, published_at: published, image_urls: imageUrls, video_urls: videoUrls, extraction_source: 'browser'};
}`
}
