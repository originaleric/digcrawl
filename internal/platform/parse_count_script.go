package platform

// ParseCountJS 供各平台 metadata script 内联的互动数字解析 helper。
const ParseCountJS = `
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
`
