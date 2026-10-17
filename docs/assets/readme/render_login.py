#!/usr/bin/env python3
"""Render the local login guide using public, credential-free screenshots."""
from pathlib import Path
from html import escape
import base64
import hashlib
import re

ROOT = Path(__file__).resolve().parent
STYLE = (ROOT / 'hero.zh-CN.svg').read_text().split('<style>')[1].split('</style>')[0]

def text(x, y, value, size=20, cls='', weight=400):
    return f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" class="{cls}">{escape(value)}</text>'

def photo(path, x, y, w, h):
    uri = 'data:image/jpeg;base64,' + base64.b64encode((ROOT / path).read_bytes()).decode()
    return f'<image x="{x}" y="{y}" width="{w}" height="{h}" preserveAspectRatio="xMidYMid meet" href="{uri}"/>'

def render(lang, filename, platform):
    zh = lang == 'zh-CN'
    steps = [('同意打开窗口', 'Agent 先征得同意'), ('自行完成登录', '在本机输入登录信息'), ('告诉 Agent 已完成', '保持登录窗口打开'), ('保存本机会话', '确认后关闭专用窗口'), ('重新读取链接', '使用保存的会话')] if zh else [('Approve opening', 'Agent asks first'), ('Sign in yourself', 'Enter details locally'), ('Tell the Agent', 'Keep the window open'), ('Save the session', 'Close after saving'), ('Read the link again', 'Reuse the local session')]
    title = '本机登录流程 · 小红书示例' if zh else 'Local sign-in · Xiaohongshu example'
    youtube = platform == 'youtube'
    if youtube:
        steps = [('同意打开 Chrome', 'Agent 先征得同意'), ('自行登录 Google', '在本机完成认证'), ('返回 YouTube', '告诉 Agent 已完成'), ('保存本机会话', '关闭窗口后同步'), ('重新读取视频', '使用已保存的会话')] if zh else [('Approve Chrome', 'Agent asks first'), ('Sign in to Google', 'Complete sign-in locally'), ('Return to YouTube', 'Tell the Agent you’re done'), ('Save the session', 'Close Chrome, then sync'), ('Read the video', 'Reuse the local session')]
        title = '本机登录流程 · YouTube' if zh else 'Local sign-in · YouTube'
    body = '<rect x="1" y="1" width="1198" height="638" rx="20" fill="var(--bg)" stroke="var(--line)"/>'
    body += text(28, 46, title, 26, '', 750)
    for i, (label, sub) in enumerate(steps):
        x = 28 + 230 * i
        body += f'<rect x="{x}" y="72" width="216" height="92" rx="12" fill="var(--surface)" stroke="var(--line)"/>'
        body += text(x + 14, 98, str(i + 1).zfill(2), 13, 'accent mono', 650)
        body += text(x + 14, 126, label, 18, '', 650)
        body += text(x + 14, 150, sub, 13, 'muted')
        if i < 4:
            body += f'<path d="M{x+219} 118h9m-5-5 5 5-5 5" class="line" style="stroke:var(--accent);stroke-width:2;fill:none"/>'
    for x in [28, 618]:
        body += f'<rect x="{x}" y="194" width="554" height="385" rx="16" fill="var(--surface)" stroke="var(--line)"/>'
    if youtube:
        before_label = '登录前 · 原页面提示' if zh else 'BEFORE SIGN-IN · ORIGINAL PAGE'
        after_label = '会话同步后 · 原视频页面' if zh else 'AFTER SESSION SYNC · ORIGINAL VIDEO'
        before_photo = 'examples/originals/youtube.jpg'
        after_photo = 'examples/originals/youtube-after-login.jpg'
    else:
        before_label = '平台登录界面' if zh else 'PLATFORM SIGN-IN FORM'
        after_label = '保存会话后读取原帖' if zh else 'POST READ AFTER SESSION SAVE'
        before_photo = 'login/xiaohongshu-login.jpg'
        after_photo = 'examples/originals/xiaohongshu.jpg'
    body += text(48, 226, before_label, 16, '', 600)
    body += text(638, 226, after_label, 16, '', 600)
    body += photo(before_photo, 48, 244, 514, 310)
    body += photo(after_photo, 638, 244, 514, 310)
    body += text(28, 613, '真实界面截图；登录信息由用户在本机填写。' if zh else 'Actual platform screens; users enter sign-in details on their own machine.', 16, 'muted')
    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="640" viewBox="0 0 1200 640" role="img" aria-labelledby="title"><title id="title">{escape(title)}</title><style>{STYLE}</style>{body}</svg>\n'
    stem = 'youtube-flow' if youtube else 'flow'
    path = ROOT / 'login' / f'{stem}.{lang}.svg'
    path.write_text(svg)
    readme = ROOT.parents[2] / filename
    content = readme.read_text()
    url = f'./docs/assets/readme/login/{stem}.{lang}.svg'
    digest = hashlib.sha256(path.read_bytes()).hexdigest()[:8]
    content = re.sub(re.escape(url) + r'(?:\?v=[^" ]+)?', url + '?v=' + digest, content)
    readme.write_text(content)
for lang, filename in [('zh-CN', 'README.zh-CN.md'), ('en', 'README.md')]:
    for platform in ['xiaohongshu', 'youtube']:
        render(lang, filename, platform)
print('Rendered bilingual login guides.')
