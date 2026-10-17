#!/usr/bin/env python3
"""Render bilingual README comparison cards from verified, sanitized examples."""
from pathlib import Path
import base64
import hashlib
import json
import re
import unicodedata
import xml.etree.ElementTree as ET
from html import escape

ROOT = Path(__file__).resolve().parent
NS = '{http://www.w3.org/2000/svg}'
DATA = json.loads((ROOT / 'examples/manifest.json').read_text())
STYLE = (ROOT / 'hero.zh-CN.svg').read_text().split('<style>')[1].split('</style>')[0]
STYLE += '''
.platform-logo{fill:var(--text)}.logo-wechat{fill:#07C160}.logo-xiaohongshu{fill:#FF2442}.logo-bilibili{fill:#00A1D6}.logo-youtube{fill:#FF0000}
.status-ok{fill:#167b51}.status-error{fill:#a45a0a}
@media(prefers-color-scheme:dark){.status-ok{fill:#76d7a3}.status-error{fill:#ffd18b}}
'''

def text(x, y, value, size=20, cls='', weight=400):
    return f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" class="{cls}">{escape(str(value))}</text>'

def wrap(value, width, size):
    """Conservative width estimate, preserving English words and CJK characters."""
    def measure(s):
        return sum(size * (1 if unicodedata.east_asian_width(c) in 'WF' else .36 if c.isspace() else .64) for c in s)
    lines, line = [], ''
    for token in re.findall(r"[A-Za-z0-9]+(?:['’-][A-Za-z0-9]+)*|\s+|.", value):
        if line and measure(line + token) > width:
            lines.append(line.rstrip())
            line = ''
        line += token.lstrip() if not line else token
    if line.strip():
        lines.append(line.rstrip())
    return lines

def logo(cid):
    root = ET.parse(ROOT / 'logos' / (cid + '.svg')).getroot()
    paths = ''.join(ET.tostring(e, encoding='unicode') for e in root if e.tag == NS + 'path')
    return f'<svg x="28" y="25" width="32" height="32" viewBox="0 0 24 24" aria-hidden="true" class="platform-logo logo-{cid}">{paths}</svg>'

def panel(x):
    return f'<rect x="{x}" y="119" width="554" height="441" rx="16" fill="var(--surface)" stroke="var(--line)"/>'

for case in DATA['cases']:
    cid = case['id']
    for lang in ['zh-CN', 'en']:
        zh = lang == 'zh-CN'
        name = case['name'] if zh else case['name_en']
        topic = case['topic'] if zh else case['topic_en']
        ok = case['status'] == 'success'
        body = '<rect x="1" y="1" width="1198" height="598" rx="20" fill="var(--bg)" stroke="var(--line)"/>'
        body += logo(cid) + text(72, 47, name.upper(), 16, '', 650)
        body += text(28, 94, topic, 30, '', 750)
        body += panel(28) + panel(618)
        original = case['original']
        if original['kind'] == 'screenshot':
            label = ('原页面截图' if zh else 'ORIGINAL PAGE')
            if cid == 'youtube':
                label = '会话同步后 · 原视频页面' if zh else 'AFTER SESSION SYNC · ORIGINAL VIDEO'
            body += text(48, 151, label, 14, 'muted', 600)
            image_path = ROOT / original['path']
            uri = 'data:image/jpeg;base64,' + base64.b64encode(image_path.read_bytes()).decode()
            body += '<rect x="44" y="168" width="522" height="320" rx="8" fill="#fff" stroke="var(--line)"/>'
            body += f'<image x="45" y="169" width="520" height="318" preserveAspectRatio="xMidYMid meet" href="{uri}"/>'
        else:
            body += text(48, 151, '原文摘录（非页面截图）' if zh else 'SOURCE EXCERPT · NOT A SCREENSHOT', 14, 'muted', 600)
            body += text(52, 209, case['author'], 17, 'muted', 550)
            y = 257
            for line in wrap(case['title'], 476, 24):
                body += text(52, y, line, 24, '', 700)
                y += 34
            y += 25
            for value in case['excerpt'].splitlines():
                for line in wrap(value, 476, 18):
                    body += text(52, y, line, 18)
                    y += 27
        body += text(48, 524, case['author'] or ('来源作者未返回' if zh else 'Source author not returned'), 17, '', 550)
        domain = re.sub(r'^https?://', '', case['url']).split('/')[0]
        body += text(48, 548, domain, 13, 'muted mono')
        label = ('登录后 · 抓取结果' if zh else 'AFTER SIGN-IN · DIGCRAWL') if case.get('before_login') else ('抓取结果' if zh else 'DIGCRAWL RESULT')
        body += text(642, 151, label, 14, 'muted', 600)
        body += text(1048 if ok else 996, 151, 'SUCCESS' if ok else case['error']['code'], 13, 'status-ok mono' if ok else 'status-error mono', 650)
        title = case['title'] if ok else 'Sign in to confirm you’re not a bot'
        title_lines = wrap(title, 498, 22)
        if cid == 'bilibili':
            title_lines = title.replace('【', '\n【', 1).splitlines()
        assert len(title_lines) <= 3, (cid, title_lines)
        for i, line in enumerate(title_lines):
            body += text(642, 194 + i * 29, line, 22, '', 650)
        body += '<path d="M642 277H1148" class="line"/>'
        if cid == 'xiaohongshu':
            lines = ['正文：能力范式转向 · 组织形态重组', '画面：个人常驻 / 编程 / 工作流 / 研究 / 创意', '返回正文及 2 条图片引用'] if zh else ['Text: capability and organization shifts', 'Image: Personal / Coding / Workflow / Research / Creative', 'Note text and 2 image references retrieved']
        elif cid == 'youtube' and ok:
            lines = case['excerpt'].splitlines() + ['字幕正文未取回' if zh else 'Caption text was not retrieved']
        elif ok:
            lines = case['excerpt'].splitlines()
        else:
            lines = ['来源要求完成本机登录验证。', '尚未取得正文、字幕或转写结果。'] if zh else ['The source requires local sign-in verification.', 'No text, captions, or transcript were retrieved.']
        lines = [line for value in lines for line in wrap(value, 498, 19)]
        assert len(lines) <= 6, (cid, lang, lines)
        for i, line in enumerate(lines):
            body += text(642, 312 + i * 28, line, 19, '' if ok else 'muted')
        if ok:
            count = case['markdown_characters']
            body += text(642, 506, f'Markdown {count:,} 字符' if zh else f'{count:,} Markdown characters', 15, 'muted mono')
            labels = {'image': '图片' if zh else 'image', 'video': '视频' if zh else 'video', 'audio': '音频' if zh else 'audio'}
            media = ' · '.join(f'{labels.get(k,k)} × {v}' for k,v in case['media_references'].items())
            body += text(642, 535, ('媒体引用 ' if zh else 'MEDIA REFERENCES ') + media, 14, 'muted mono')
        else:
            body += text(642, 535, '如实返回访问状态' if zh else 'ACCESS STATE REPORTED AS RETURNED', 14, 'muted mono')
        title = name + ' · ' + topic
        desc = '原始来源与实际抓取结果对照。' if zh else 'Original source beside the actual retrieval result.'
        result = f'<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="600" viewBox="0 0 1200 600" role="img" aria-labelledby="title desc"><title id="title">{escape(title)}</title><desc id="desc">{desc}</desc><style>{STYLE}</style>{body}</svg>\n'
        (ROOT / 'examples' / f'{cid}.{lang}.svg').write_text(result)
for lang, name in [('zh-CN', 'README.zh-CN.md'), ('en', 'README.md')]:
    readme = ROOT.parents[2] / name
    content = readme.read_text()
    for case in DATA['cases']:
        path = f"examples/{case['id']}.{lang}.svg"
        digest = hashlib.sha256((ROOT / path).read_bytes()).hexdigest()[:8]
        url = './docs/assets/readme/' + path
        content = re.sub(re.escape(url) + r'(?:\?v=[^" ]+)?', url + '?v=' + digest, content)
    readme.write_text(content)
print('Rendered 10 bilingual comparison cards and refreshed README image URLs.')
