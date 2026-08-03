# Collect 模块

DigCrawl 内置的**无宿主本地内容采集**：将链接保存为 Markdown / HTML / JSON，可选下载媒体到 `.assets/`，并按 `PersistMode` 维护 `index.json` 和/或 SQLite（`collect.db`）。

用户向命令与配置说明见仓库根目录 **[USAGE.md §8](../USAGE.md#8-本地内容采集collect)**。

## 处理流程

```text
digcrawl collect <url>
  → collect.Detect（平台 URL 或 http(s) 通用网页）
  → collect.Run → runtime.Execute
  → provider/platform 或 provider/browser
  → collect.Export（+ 媒体下载）
  → persist（index.json / SQLite，由 PersistMode 决定）
```

## 命令示例

```bash
digcrawl collect "https://mp.weixin.qq.com/s/..."
digcrawl collect --file urls.txt --format md,html,json --out ./collect-output
digcrawl collect inbox
digcrawl collect inbox --watch
digcrawl collect list                    # 需 PersistMode=sqlite|both
digcrawl collect show <id|url>
digcrawl collect migrate-index           # 从 index.json 导入 SQLite
```

## 平台读取

Collect 会先按 URL 检测平台并调用对应读取工具。微信公众号公开文章优先使用 HTTP HTML 静态解析，成功时输出 `read_channel=http`、`extraction_source=html`；网络 transient 或解析状态不确定时才进入浏览器兜底。验证码、频控、删除、私密、登录要求和跳出微信域名会作为结构化错误返回，避免把平台壳页或风控页写成正常文章。

Reddit 单帖 URL 会路由到 `reddit.post.read`，包括 `https://www.reddit.com/r/{sub}/comments/{id}/...`、`https://www.reddit.com/comments/{id}/...` 和 `https://redd.it/{id}`。subreddit/user/listing URL 不会误路由到单帖工具；这些来源应通过 `reddit.subreddit.read`、`reddit.user.read` 或 watchlist `reddit_subreddit` / `reddit_user` 使用。

## 输出布局

```text
collect-output/
  index.json                 # PersistMode=index|both
  2026-05-21/
    文章标题.md
    文章标题.json
    文章标题.assets/
      media.json
      001.jpg

storage/digcrawl/
  collect.db                 # PersistMode=sqlite|both

collect-inbox/
  *.url / *.txt          # 待采集（每行一个 URL）
  processed/             # 已成功
  failed/                # 失败记录
```

导出文件名会移除非法路径字符，并按 UTF-8 边界限制长度；长中文标题会被安全截断，不会切断多字节字符。

## 配置

`config/config.yaml` 中 `Collect` 段：

| 字段 | 说明 |
|------|------|
| `PersistMode` | `files` / `index`（默认）/ `sqlite` / `both` |
| `DBPath` | SQLite 路径，默认 `storage/digcrawl/collect.db` |
| `DownloadMedia` | 下载 `media[]` 到 `.assets/`（公众号需 `include_media`） |
| `IndexEnabled` | `index`/`both` 模式下可显式关闭 index.json |

如启用全局 `OCR.Enabled`，Collect 会在导出时对图片做 OCR：下载媒体时优先读取 `.assets/` 本地图片，未下载媒体时使用远程图片 URL。OCR 结果写入 JSON 的 `platform_metadata.ocr_results`，并可按 `OCR.AppendToMarkdown` 追加到 Markdown。`OCR.Provider=openai_compatible` 时，本地 assets 会转换为 data URL 后调用视觉模型的 `/chat/completions` 接口。

浏览器域名白名单见 `LocalBrowser.AllowedDomains`。若本机 Chrome/Chromium headless 启动失败，可按根目录 `README.md` / `USAGE.md` 中的 Playwright `chrome-headless-shell` 方案配置 `LocalBrowser.ChromePath` 与 `LocalBrowser.ExtraArgs`。

## 与 Knowledge 入库的差异

Collect 以**本地文件为真源**（md/json/assets），SQLite 仅存元数据与路径；DigEino → Knowledge 的向量/业务库需宿主自行 ingest。JSON 工具输出中的 `media[]` 可供 ingest 解析远程 URL 或本地 `.assets/` 路径。
