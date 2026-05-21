package xiaohongshu

import (
	"github.com/originaleric/diggate/internal/platform"
)

const (
	PlatformName = "xiaohongshu"
	ContentType  = "note"
	CookieDomain = "www.xiaohongshu.com"
)

// DefaultWaitSelector 笔记页主内容等待选择器。
const DefaultWaitSelector = "#detail-desc, .note-content, .interaction-container, .note-detail, main"

// DefaultContentSelector 正文容器选择器。
const DefaultContentSelector = "#detail-desc, .note-content, .content, .note-detail"

// NoteBrowseAttempts selector 兜底顺序（与 wechat 多组重试一致）。
var NoteBrowseAttempts = []platform.BrowseAttempt{
	{WaitSelector: "#detail-desc", ContentSelector: "#detail-desc"},
	{WaitSelector: ".note-content", ContentSelector: ".note-content"},
	{WaitSelector: ".note-detail", ContentSelector: ".note-detail"},
	{WaitSelector: "main", ContentSelector: "main"},
}

// MetadataScript 页面状态探测 + hydration/meta/DOM 三层提取。
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
    const loginHints = ['登录', '请登录', '扫码登录', '手机号登录', '登录后查看'];
    const captchaHints = ['验证', '滑块', '安全验证', '拖动滑块'];
    const deletedHints = ['笔记不存在', '内容不存在', '已删除', '找不到'];
    const privateHints = ['仅作者可见', '无法查看', '内容不可见', '私密'];
    if (loginHints.some(h => body.includes(h)) && document.querySelector('.login-container, .login-modal, [class*="login"]')) {
      return { error_code: 'LOGIN_REQUIRED', error_message: '需要登录后查看' };
    }
    if (captchaHints.some(h => body.includes(h)) && document.querySelector('[class*="captcha"], [class*="verify"], .verify-wrap')) {
      return { error_code: 'CAPTCHA_REQUIRED', error_message: '需要完成安全验证' };
    }
    if (deletedHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_DELETED', error_message: '笔记不存在或已删除' };
    }
    if (privateHints.some(h => body.includes(h))) {
      return { error_code: 'CONTENT_PRIVATE', error_message: '内容不可见' };
    }
    if (document.title && /404|not found/i.test(document.title)) {
      return { error_code: 'CONTENT_NOT_FOUND', error_message: '页面未找到' };
    }
    return {};
  };

  const tryHydration = () => {
    const candidates = [
      window.__INITIAL_STATE__,
      window.__INITIAL_SSR_STATE__,
      window.__REDUX_STATE__,
      window.__pinia?.state?.value,
    ].filter(Boolean);
    for (const root of candidates) {
      try {
        const note = root?.note?.noteDetailMap || root?.noteDetailMap || root?.note?.firstNote;
        const keys = note && typeof note === 'object' ? Object.keys(note) : [];
        const item = keys.length ? (note[keys[0]]?.note || note[keys[0]] || note) : null;
        const n = item?.note || item;
        if (!n || typeof n !== 'object') continue;
        const title = n.title || n.displayTitle || '';
        const desc = n.desc || n.description || '';
        const user = n.user || n.author || {};
        const interact = n.interactInfo || n.interaction || {};
        const imgs = (n.imageList || n.images || []).map(img => img.urlDefault || img.url || img.infoList?.[0]?.url).filter(Boolean);
        const video = n.video?.media?.stream?.h264?.[0]?.masterUrl || n.video?.url || '';
        const tags = (n.tagList || n.tags || []).map(t => t.name || t).filter(Boolean);
        const kind = (n.type || n.noteType || '').toString().toLowerCase();
        return {
          extraction_source: 'hydration',
          title: title || '',
          text: desc || '',
          author_name: user.nickname || user.name || '',
          author_id: user.userId || user.id || '',
          likes: parseCount(interact.likedCount || interact.liked_count || interact.likes),
          comments: parseCount(interact.commentCount || interact.comment_count),
          bookmarks: parseCount(interact.collectedCount || interact.collect_count || interact.collected),
          image_urls: [...new Set(imgs)].slice(0, 20),
          video_url: video || '',
          content_kind: kind.includes('video') ? 'video' : 'note',
          tags,
        };
      } catch (e) { /* continue */ }
    }
    return null;
  };

  const tryMetaTags = () => {
    const og = (name) => document.querySelector('meta[property="' + name + '"]')?.content
      || document.querySelector('meta[name="' + name + '"]')?.content || '';
    const title = og('og:title') || document.title || '';
    const desc = og('og:description') || '';
    const image = og('og:image') || '';
    if (!title && !desc && !image) return null;
    return {
      extraction_source: 'meta',
      title,
      text: desc,
      image_urls: image ? [image] : [],
    };
  };

  const tryDOM = () => {
    const title = pick(['#detail-title', '.title', 'h1']);
    const text = pick(['#detail-desc', '.note-content', '.content', '.desc']);
    const author = pick(['.username', '.author-wrapper .name', '.user-name', '.name']);
    const likes = parseCount(pick(['.like-wrapper .count', '[class*="like"] .count']));
    const comments = parseCount(pick(['.chat-wrapper .count', '[class*="comment"] .count']));
    const collects = parseCount(pick(['.collect-wrapper .count', '[class*="collect"] .count']));
    const imgs = Array.from(document.querySelectorAll('.note-content img, .swiper-slide img, .note-slider img, img'))
      .map(i => i.currentSrc || i.src || i.getAttribute('data-src'))
      .filter(u => u && !u.startsWith('data:'));
    const videoEl = document.querySelector('video');
    const videoUrl = videoEl?.currentSrc || videoEl?.src || '';
    const tags = Array.from(document.querySelectorAll('a.tag, .tag-item, #hash-tag'))
      .map(el => (el.innerText || '').trim()).filter(Boolean);
    const hasVideo = !!videoEl || !!document.querySelector('[class*="video-player"]');
    if (!title && !text && imgs.length === 0 && !videoUrl) return null;
    return {
      extraction_source: 'dom',
      title: title || document.title || '',
      text,
      author_name: author,
      likes, comments, bookmarks: collects,
      image_urls: [...new Set(imgs)].slice(0, 20),
      video_url: videoUrl,
      content_kind: hasVideo ? 'video' : 'note',
      tags,
    };
  };

  const state = detectPageState();
  if (state.error_code) {
    return { ...state, warnings, extraction_source: 'page_state' };
  }

  const layers = [tryHydration, tryMetaTags, tryDOM];
  let merged = null;
  for (const fn of layers) {
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

  if (!merged.text && (merged.image_urls?.length || merged.video_url)) {
    merged.error_code = 'PARTIAL_CONTENT';
    merged.warnings = [...(merged.warnings || []), 'text empty, media only'];
  }
  return merged;
})()`
}
