package kuaishou

const (
	DefaultWaitSelector    = "html"
	DefaultContentSelector = "html"
)

func MetadataScript() string {
	return `() => {
  const textOf = (sel) => document.querySelector(sel)?.textContent?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const body = document.body?.innerText || '';
  if (body.includes('验证码') || body.includes('安全验证') || body.includes('滑动验证')) {
    return {error_code: 'CAPTCHA_REQUIRED', error_message: '快手页面需要安全验证', extraction_source: 'page_state'};
  }
  if (body.includes('登录') && body.includes('快手') && !document.querySelector('video')) {
    return {error_code: 'LOGIN_REQUIRED', error_message: '快手页面需要登录后查看', extraction_source: 'page_state'};
  }
  const title = textOf('h1') || textOf('[class*=title]') || document.title || meta('og:title');
  const description = meta('description') || textOf('[class*=desc]') || textOf('[class*=caption]');
  const author = textOf('[class*=user-name]') || textOf('[class*=author]') || textOf('[class*=profile]');
  const published = textOf('[class*=time]') || textOf('[class*=date]');
  const cover = meta('og:image');
  const videoUrls = Array.from(document.querySelectorAll('video source[src], video[src]')).map(el => el.src || el.getAttribute('src')).filter(Boolean);
  return {title, description, author_name: author, published_at: published, cover_url: cover, video_urls: videoUrls, extraction_source: 'browser'};
}`
}
