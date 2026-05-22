package douyin

import (
	"github.com/originaleric/digcrawl/internal/platform"
)

const (
	PlatformName = "douyin"
	ContentType  = "video"
	CookieDomain = "www.douyin.com"
)

const (
	DefaultWaitSelector    = "video, .video-info-detail, #root, .player-container"
	DefaultContentSelector = ".video-info-detail, .account-card, #root, .player-container"
)

// VideoBrowseAttempts 采集兜底 selector。
var VideoBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: "video", ContentSelector: ".video-info-detail"},
	{WaitSelector: ".video-info-detail", ContentSelector: ".video-info-detail"},
	{WaitSelector: "#root", ContentSelector: "#root"},
}

// MetadataScript 页面状态 + hydration/meta/DOM 提取。
func MetadataScript() string {
	return platform.ParseCountJS + `(() => {
  const warnings = [];
  const pick = (sels) => {
    for (const s of sels) {
      const el = document.querySelector(s);
      if (el) {
        const t = (el.innerText || el.textContent || el.content || '').trim();
        if (t) return t;
      }
    }
    return '';
  };
  const detectPageState = () => {
    const body = (document.body?.innerText || '').slice(0, 4000);
    if (['登录', '请登录', '扫码登录'].some(h => body.includes(h)) && document.querySelector('[class*="login"]')) {
      return { error_code: 'LOGIN_REQUIRED', error_message: '需要登录' };
    }
    if (['验证', '验证码', '安全验证'].some(h => body.includes(h))) {
      return { error_code: 'CAPTCHA_REQUIRED', error_message: '需要安全验证' };
    }
    if (['视频不存在', '作品不存在', '已删除', '找不到'].some(h => body.includes(h))) {
      return { error_code: 'CONTENT_DELETED', error_message: '视频不存在或已删除' };
    }
    if (['仅自己可见', '无法查看', '私密'].some(h => body.includes(h))) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '内容不可见' };
    }
    return {};
  };

  const tryHydration = () => {
    const roots = [window.__INIT_PROPS__, window.__RENDER_DATA__, window.SSR_RENDER_DATA].filter(Boolean);
    for (const root of roots) {
      try {
        const str = typeof root === 'string' ? root : JSON.stringify(root);
        const m = str.match(/"desc"\s*:\s*"([^"]+)"/);
        const titleM = str.match(/"shareTitle"\s*:\s*"([^"]+)"/) || str.match(/"title"\s*:\s*"([^"]+)"/);
        if (m || titleM) {
          return {
            extraction_source: 'hydration',
            title: titleM ? titleM[1] : '',
            description: m ? m[1] : '',
          };
        }
      } catch (e) {}
    }
    return null;
  };

  const tryMeta = () => {
    const title = document.querySelector('meta[property="og:title"]')?.content || '';
    const desc = document.querySelector('meta[property="og:description"]')?.content || '';
    const cover = document.querySelector('meta[property="og:image"]')?.content || '';
    if (!title && !desc) return null;
    return { extraction_source: 'meta', title, description: desc, cover_url: cover };
  };

  const tryDOM = () => {
    const title = pick(['h1', '.title', '[data-e2e="video-title"]']);
    const desc = pick(['.video-info-detail span', '[data-e2e="video-desc"]']);
    const author = pick(['.account-name', '[data-e2e="video-author-name"]']);
    const cover = document.querySelector('meta[property="og:image"]')?.content
      || document.querySelector('video')?.poster || '';
    return {
      extraction_source: 'dom',
      title: title || document.title || '',
      description: desc,
      author_name: author,
      likes: parseCount(pick(['[data-e2e="like-count"]', '.like-count'])),
      comments: parseCount(pick(['[data-e2e="comment-count"]'])),
      shares: parseCount(pick(['[data-e2e="share-count"]'])),
      cover_url: cover,
    };
  };

  const state = detectPageState();
  if (state.error_code) return { ...state, warnings, extraction_source: 'page_state' };

  let merged = null;
  for (const fn of [tryHydration, tryMeta, tryDOM]) {
    const part = fn();
    if (!part) continue;
    merged = merged ? { ...merged, ...part } : part;
  }
  if (!merged || (!merged.title && !merged.description)) {
    return {
      error_code: 'SELECTOR_STALE',
      error_message: '未能提取视频信息',
      warnings,
      extraction_source: 'none',
    };
  }
  if (!merged.description) {
    merged.error_code = 'PARTIAL_CONTENT';
    merged.warnings = [...warnings, 'description empty'];
  }
  return merged;
})()`
}
