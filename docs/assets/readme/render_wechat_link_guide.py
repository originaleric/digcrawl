#!/usr/bin/env python3
"""Render bilingual link-copy instructions from user-provided WeChat screenshots."""
from pathlib import Path
from html import escape
import base64
import hashlib
import re

ROOT = Path(__file__).resolve().parent
STYLE = (ROOT / 'hero.zh-CN.svg').read_text().split('<style>')[1].split('</style>')[0]

def text(x, y, value, size=20, cls='', weight=400):
    return f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" class="{cls}">{escape(value)}</text>'

def image_uri(name):
    return 'data:image/png;base64,' + base64.b64encode((ROOT / 'wechat-channels' / name).read_bytes()).decode()

for lang, filename in [('zh-CN', 'README.zh-CN.md'), ('en', 'README.md')]:
    zh = lang == 'zh-CN'
    title = '获取微信视频链接与元宝授权' if zh else 'Copy a Channels link and authorize Yuanbao'
    steps = [('打开视频卡片', '微信中的分享视频'), ('复制视频链接', '分享菜单 → 复制链接'), ('发给 Agent', '提出读取或转写需求'), ('授权并保存会话', '首次使用或会话失效时'), ('获取视频与转写', 'Agent 根据内容生成摘要')] if zh else [('Open video card', 'In a WeChat conversation'), ('Copy video link', 'From the sharing menu'), ('Send to Agent', 'Ask to read or transcribe'), ('Authorize Yuanbao', 'Only when requested'), ('Read & transcribe', 'Agent summarizes results')]
    body = '<rect x="1" y="1" width="1198" height="848" rx="20" fill="var(--bg)" stroke="var(--line)"/>'
    body += text(28, 46, title, 26, '', 750)
    for i, (label, sub) in enumerate(steps):
        x = 28 + i * 230
        body += f'<rect x="{x}" y="72" width="216" height="94" rx="12" fill="var(--surface)" stroke="var(--line)"/>'
        body += text(x + 16, 99, str(i + 1).zfill(2), 13, 'accent mono', 650)
        body += text(x + 16, 128, label, 17, '', 650)
        body += text(x + 16, 151, sub, 13, 'muted')
        if i < 4:
            body += f'<path d="M{x+219} 119h9m-5-5 5 5-5 5" class="flow"/>'
    for x in [28, 418, 808]:
        body += f'<rect x="{x}" y="194" width="364" height="586" rx="16" fill="var(--surface)" stroke="var(--line)"/>'
    body += text(48, 229, '微信聊天中的视频卡片' if zh else 'VIDEO CARD IN WECHAT', 17, '', 600)
    body += f'<image x="48" y="249" width="324" height="510" preserveAspectRatio="xMidYMid meet" href="{image_uri("video-card.png")}"/>'
    body += text(438, 229, '分享菜单 · 复制链接' if zh else 'SHARING MENU · COPY LINK', 15, '', 600)
    # Show the complete privacy-edited sharing screenshot, including video,
    # status bar, and all sharing controls; retain the copy-link annotation.
    body += '<svg x="438" y="249" width="324" height="510" viewBox="0 0 851 1848">'
    body += f'<image x="0" y="0" width="851" height="1848" href="{image_uri("share-menu-redacted.png")}"/>'
    body += '<rect x="700" y="1455" width="140" height="180" rx="18" fill="none" stroke="var(--accent)" stroke-width="6"/>'
    body += '</svg>'
    body += text(828, 229, '腾讯元宝 · 微信授权' if zh else 'YUANBAO · WECHAT AUTHORIZATION', 15, '', 600)
    body += f'<image x="828" y="249" width="324" height="510" preserveAspectRatio="xMidYMid meet" href="{image_uri("yuanbao-authorization.png")}"/>'
    body += text(28, 817, '元宝会话可复用；首次使用或会话失效时，按提示完成微信授权并确认保存，再重试原链接。' if zh else 'Reuse the Yuanbao session. When requested, authorize with WeChat, confirm saving, and retry the link.', 16, 'muted')
    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="850" viewBox="0 0 1200 850" role="img" aria-labelledby="title"><title id="title">{escape(title)}</title><style>{STYLE}</style>{body}</svg>\n'
    path = ROOT / 'wechat-channels' / f'link-flow.{lang}.svg'
    path.write_text(svg)
    readme = ROOT.parents[2] / filename
    content = readme.read_text()
    url = f'./docs/assets/readme/wechat-channels/link-flow.{lang}.svg'
    digest = hashlib.sha256(path.read_bytes()).hexdigest()[:8]
    content = re.sub(re.escape(url) + r'(?:\?v=[^" ]+)?', url + '?v=' + digest, content)
    readme.write_text(content)
print('Rendered bilingual WeChat link-copy guides.')
