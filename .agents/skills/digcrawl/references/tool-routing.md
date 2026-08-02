# Tool routing

Select the most specific read tool. Pass the original URL and `format: ["markdown"]` unless a user asks for a different representation.

| URL family | Tool |
|---|---|
| `mp.weixin.qq.com`, `weixin.qq.com` | `wechat.article.read` |
| `xiaohongshu.com`, `xhslink.com` | `xiaohongshu.note.read` |
| `douyin.com`, `iesdouyin.com`, `v.douyin.com` | `douyin.video.read` |
| Toutiao article URLs | `toutiao.article.read` |
| Toutiao `/video/` URLs | `toutiao.video.read` |
| `bilibili.com`, `b23.tv` | `bilibili.video.read` |
| `weibo.com`, `m.weibo.cn` | `weibo.post.read` |
| `kuaishou.com`, `v.kuaishou.com` | `kuaishou.video.read` |
| `zhihu.com`, `zhuanlan.zhihu.com` | `zhihu.content.read` |
| `x.com`, `twitter.com` | `x.post.read` |
| Reddit post | `reddit.post.read` |
| Reddit subreddit listing | `reddit.subreddit.read` |
| Reddit user listing | `reddit.user.read` |
| `threads.net`, `threads.com` | `threads.post.read` |
| `instagram.com` | `instagram.content.read` |
| RSS or Atom URL | `feed.read` |
| Any other `http` or `https` URL | `browser.browse` |

Do not use `browser.browse` as a second attempt after a supported platform tool returns an access-control, captcha, rate-limit, private-content, or content-type error. A platform-specific tool has the better diagnostic and policy boundary. `Gateway.AllowedDomains`, when non-empty, is an operator restriction and still applies to browser fallback.

Use `use_cookie_domain` only when the user has explicitly stated that they have already completed a local login for the target platform and asks to use that session. Never ask the user to provide the cookie itself.
