package toutiao

// VideoMetadataScript extracts stable metadata from Toutiao video pages.
func VideoMetadataScript() string {
	return `() => {
  const parseCount = (raw) => {
    const s = String(raw || '').trim().replace(/,/g, '');
    if (!s || s === '-' || s === '赞' || s === '收藏') return 0;
    const m = s.match(/([\d.]+)\s*(万|億|亿|K|M|B)?/i);
    if (!m) return 0;
    const n = parseFloat(m[1]);
    if (!Number.isFinite(n)) return 0;
    const unit = (m[2] || '').toLowerCase();
    const factor = unit === '万' ? 10000 :
      unit === '亿' || unit === '億' ? 100000000 :
      unit === 'k' ? 1000 :
      unit === 'm' ? 1000000 :
      unit === 'b' ? 1000000000 : 1;
    return Math.round(n * factor);
  };
  const warnings = [];
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
  const uniq = (arr) => [...new Set(arr.map(v => String(v || '').trim()).filter(Boolean))];
  const detectPageState = () => {
    const body = (document.body?.innerText || '').slice(0, 4000);
    const loginHints = ['登录', '请登录', '扫码登录', '手机号登录'];
    const captchaHints = ['验证', '滑块', '安全验证', '拖动滑块'];
    const privateHints = ['无法查看', '内容不可见', '审核中'];
    if (loginHints.some(h => body.includes(h)) && document.querySelector('[class*="login"], .login-wrap')) {
      return { error_code: 'LOGIN_REQUIRED', error_message: '需要登录后查看' };
    }
    if (captchaHints.some(h => body.includes(h)) && document.querySelector('[class*="captcha"], [class*="verify"]')) {
      return { error_code: 'CAPTCHA_REQUIRED', error_message: '需要完成安全验证' };
    }
    if (privateHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '内容不可见' };
    }
    if (document.title && /404|not found/i.test(document.title)) {
      return { error_code: 'CONTENT_NOT_FOUND', error_message: '页面未找到' };
    }
    return {};
  };
  const meta = (name) => document.querySelector('meta[property="' + name + '"]')?.content
    || document.querySelector('meta[name="' + name + '"]')?.content || '';
  const validPublished = (raw) => {
    const s = String(raw || '').trim();
    if (!s) return '';
    if (/^\d{1,2}:\d{2}(?::\d{2})?(\s*\/\s*\d{1,2}:\d{2}(?::\d{2})?)?/.test(s)) return '';
    if (s.includes('/') && /^\d{1,2}:\d{2}/.test(s)) return '';
    if (s === '直播' || s === '正在直播') return '';
    return s;
  };
  const normalizePublished = (raw) => {
    const s = validPublished(raw);
    if (!s) return '';
    if (/^\d{10}$/.test(s)) {
      const d = new Date(Number(s) * 1000);
      return Number.isNaN(d.getTime()) ? '' : d.toISOString();
    }
    if (/^\d{13}$/.test(s)) {
      const d = new Date(Number(s));
      return Number.isNaN(d.getTime()) ? '' : d.toISOString();
    }
    return s;
  };
  const jsonPublished = () => {
    const text = Array.from(document.scripts).map(s => s.textContent || '').join('\n').slice(0, 600000)
      .replaceAll('\\\"', '"').replaceAll('\\\/', '/').replaceAll('\\u002F', '/');
    const patterns = [
      /"(?:publish_time|publishTime|published_at|publishedAt|create_time|createTime|display_time|displayTime|datePublished)"\s*:\s*"([^"]+)"/ig,
      /"(?:publish_time|publishTime|published_at|publishedAt|create_time|createTime)"\s*:\s*(\d{10,13})/ig
    ];
    for (const re of patterns) {
      let m;
      while ((m = re.exec(text))) {
        const v = normalizePublished(m[1]);
        if (v) return v;
      }
    }
    return '';
  };
  const domPublished = () => {
    const candidates = [
      meta('article:published_time'),
      meta('publish_time'),
      meta('datePublished'),
      document.querySelector('[itemprop="datePublished"]')?.content,
      document.querySelector('[datetime]')?.getAttribute('datetime'),
      pick(['[class*="publish-time"]', '[class*="publishTime"]', '[class*="publish"]'])
    ];
    for (const candidate of candidates) {
      const v = normalizePublished(candidate);
      if (v) return v;
    }
    return '';
  };
  const textLines = () => (document.body?.innerText || '')
    .split(/\n+/).map(s => s.trim()).filter(Boolean);
  const videos = () => uniq(Array.from(document.querySelectorAll('video')).flatMap(v => [
    v.currentSrc, v.src, v.getAttribute('src'),
    ...Array.from(v.querySelectorAll('source')).map(s => s.src || s.getAttribute('src'))
  ])).filter(u => /^https?:\/\//i.test(u));
  const scriptVideoURLs = () => {
    const html = Array.from(document.scripts).map(s => s.textContent || '').join('\n').slice(0, 300000);
    return uniq((html.match(/https?:\\?\/\\?\/[^"'\\<>\s]+(?:mime_type=video_mp4|\.mp4)[^"'\\<>\s]*/ig) || [])
      .map(s => s.replaceAll('\\\/', '/').replaceAll('\\u002F', '/').replaceAll('\\u0026', '&')));
  };
  const body = textLines();
  const title = (meta('og:title') || pick(['h1', '[class*="title"]']) || document.title || '').replace(/\s*-\s*今日头条\s*$/, '').trim();
  let description = meta('og:description') || pick(['[class*="abstract"]', '[class*="desc"]']);
  if (!description) {
    const idx = body.findIndex(v => title && v.includes(title));
    if (idx >= 0 && body[idx + 1] && !['评论', '分享', '推荐视频'].includes(body[idx + 1])) {
      description = body[idx + 1];
    }
  }
  const state = detectPageState();
  if (state.error_code) return { ...state, warnings, extraction_source: 'page_state', page_kind: 'video' };
  const video_urls = uniq([...videos(), ...scriptVideoURLs()]).slice(0, 5);
  const cover_url = meta('og:image') || document.querySelector('video')?.poster || '';
  const author_name = pick(['[class*="media"] [class*="name"]', '[class*="author"]', '.name']);
  const published_at = jsonPublished() || domPublished();
  const deletedHints = ['内容不存在', '已删除', '找不到', '视频不存在'];
  const fullText = document.body?.innerText || '';
  if (!video_urls.length && deletedHints.some(h => fullText.includes(h))) {
    return { error_code: 'CONTENT_DELETED', error_message: '视频不存在或已删除', warnings, extraction_source: 'page_state', page_kind: 'video' };
  }
  const out = {
    extraction_source: video_urls.length ? 'dom_video' : 'dom',
    page_kind: 'video',
    title,
    description,
    text: description,
    author_name,
    published_at,
    cover_url,
    video_urls,
    body_text_length: (document.body?.innerText || '').length,
    final_url: String(location.href || ''),
    page_title: document.title || '',
    likes: parseCount(pick(['[class*="like"]'])),
    comments: parseCount(pick(['[class*="comment"]'])),
    shares: parseCount(pick(['[class*="share"]'])),
    warnings,
  };
  if (!out.description && video_urls.length) {
    out.error_code = 'PARTIAL_CONTENT';
    out.error_message = '视频简介为空，仅提取到媒体';
    out.warnings = [...warnings, 'description empty'];
  }
  if (!out.title && !video_urls.length) {
    return { error_code: 'SELECTOR_STALE', error_message: '未能提取视频信息', warnings, extraction_source: 'none', page_kind: 'video' };
  }
  return out;
}`
}
