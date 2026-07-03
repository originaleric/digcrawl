package toutiao

import (
	platform "github.com/originaleric/digcrawl/internal/platform"
)

// MetadataScript 页面状态 + hydration/meta/DOM 提取。
func MetadataScript() string {
	return platform.ParseCountJS + `(() => {
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
	  const detectPageState = () => {
	    const body = (document.body?.innerText || '').slice(0, 4000);
	    const href = String(location.href || '').toLowerCase();
	    const html = document.documentElement?.innerHTML?.slice(0, 12000).toLowerCase() || '';
	    const videoMarkers = ['awemevideo', 'shortvideo', 'hotsoon_video', 'video_oracle', 'sslocal://awemevideo'];
	    const loginHints = ['登录', '请登录', '扫码登录', '手机号登录'];
	    const captchaHints = ['验证', '滑块', '安全验证', '拖动滑块'];
	    const deletedHints = ['内容不存在', '已删除', '找不到', '文章不存在'];
	    const privateHints = ['无法查看', '内容不可见', '审核中'];
	    if (href.includes('/video/') || videoMarkers.some(h => html.includes(h))) {
	      return { error_code: 'CONTENT_TYPE_MISMATCH', error_message: '当前 URL 不是头条文章页', page_kind: 'video' };
	    }
	    if (loginHints.some(h => body.includes(h)) && document.querySelector('[class*="login"], .login-wrap')) {
	      return { error_code: 'LOGIN_REQUIRED', error_message: '需要登录后查看' };
	    }
    if (captchaHints.some(h => body.includes(h)) && document.querySelector('[class*="captcha"], [class*="verify"]')) {
      return { error_code: 'CAPTCHA_REQUIRED', error_message: '需要完成安全验证' };
    }
    if (deletedHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_DELETED', error_message: '文章不存在或已删除' };
    }
    if (privateHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '内容不可见' };
    }
    if (document.title && /404|not found/i.test(document.title)) {
      return { error_code: 'CONTENT_NOT_FOUND', error_message: '页面未找到' };
    }
    return {};
  };

  const deepFindArticle = (root, depth = 0) => {
    if (!root || depth > 6) return null;
    if (typeof root !== 'object') return null;
    const title = root.title || root.Title || root.display_title || '';
    const text = root.content || root.abstract || root.summary || root.desc || root.text || '';
    const authorObj = root.media_info || root.author || root.source || root.mediaInfo || {};
    const author = authorObj.name || authorObj.source || authorObj.author_name || root.source || '';
    const publish = root.publish_time || root.publishTime || root.datetime || root.display_time || '';
    if (title || text) {
      const imgs = (root.image_list || root.images || root.gallery || []).map(i => (typeof i === 'string' ? i : (i?.url || i?.src))).filter(Boolean);
      return {
        extraction_source: 'hydration',
        title: String(title || ''),
        text: typeof text === 'string' ? text : String(text || ''),
        author_name: String(author || ''),
        published_at: publish ? String(publish) : '',
        image_urls: [...new Set(imgs)].slice(0, 20),
      };
    }
    for (const v of Object.values(root)) {
      if (v && typeof v === 'object') {
        const found = deepFindArticle(v, depth + 1);
        if (found) return found;
      }
    }
    return null;
  };

  const tryScriptJSON = () => {
    for (const s of document.querySelectorAll('script[type="application/json"], script[type="application/ld+json"]')) {
      try {
        const obj = JSON.parse(s.textContent || '{}');
        const found = deepFindArticle(obj);
        if (found) return found;
      } catch (e) {}
    }
    return null;
  };

  const tryHydration = () => {
    const candidates = [
      window.__INITIAL_STATE__,
      window.RENDER_DATA,
      window.__NEXT_DATA__,
      window.__NUXT__,
    ].filter(Boolean);
    for (const root of candidates) {
      try {
        const direct = root?.articleInfo || root?.article?.info || root?.props?.pageProps?.articleInfo || root?.data?.article;
        if (direct && typeof direct === 'object') {
          const found = deepFindArticle(direct);
          if (found) return found;
        }
        const found = deepFindArticle(root);
        if (found) return found;
      } catch (e) {}
    }
    return tryScriptJSON();
  };

  const tryMetaTags = () => {
    const og = (name) => document.querySelector('meta[property="' + name + '"]')?.content
      || document.querySelector('meta[name="' + name + '"]')?.content || '';
    const title = og('og:title') || document.title || '';
    const desc = og('og:description') || '';
    const author = og('bytedance:author') || og('article:author') || '';
    const published = og('article:published_time') || '';
    const image = og('og:image') || '';
    if (!title && !desc) return null;
    return {
      extraction_source: 'meta',
      title,
      text: desc,
      author_name: author,
      published_at: published,
      image_urls: image ? [image] : [],
    };
  };

  const tryDOM = () => {
    const title = pick(['h1', '.article-title', '.title', '[class*="title"]']);
    const text = pick(['article', '.article-content', '.tt-article-content', '.article-box', 'main']);
	    const author = pick(['.name', '.author-name', '.source', '[class*="author"]', '.media-name', '[class*="media"] [class*="name"]']);
	    const published = pick(['.time', '.publish-time', 'time', '[class*="time"]', '[class*="publish"]']);
    const imgs = Array.from(document.querySelectorAll('article img, .article-content img, main img'))
      .map(i => i.currentSrc || i.src || i.getAttribute('data-src'))
      .filter(u => u && !u.startsWith('data:'));
    if (!title && !text && imgs.length === 0) return null;
    return {
      extraction_source: 'dom',
      title: title || document.title || '',
      text,
      author_name: author,
      published_at: published,
      image_urls: [...new Set(imgs)].slice(0, 20),
    };
  };

  const state = detectPageState();
  if (state.error_code) {
    return { ...state, warnings, extraction_source: 'page_state' };
  }

  let merged = null;
  for (const fn of [tryHydration, tryMetaTags, tryDOM]) {
    const part = fn();
    if (!part) continue;
    merged = merged ? { ...merged, ...part, warnings } : { ...part, warnings };
    if (merged.title && merged.text) break;
  }

	  if (!merged) {
    return {
      error_code: 'SELECTOR_STALE',
      error_message: '未能从页面提取结构化内容',
      warnings: ['hydration/meta/dom extraction failed'],
      extraction_source: 'none',
    };
	  }

	  merged.final_url = String(location.href || '');
	  merged.page_title = document.title || '';
	  merged.body_text_length = (merged.text || '').length;
	  if (!merged.text && (merged.image_urls?.length)) {
	    merged.error_code = 'PARTIAL_CONTENT';
	    merged.warnings = [...(merged.warnings || []), 'text empty, media only'];
  }
  return merged;
})()`
}
