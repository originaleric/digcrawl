# Collect 模块

DigGate 内置的**无宿主本地内容采集**：将链接保存为 Markdown / HTML / JSON，可选下载媒体到 `.assets/`，并维护 `index.json` 索引。

用户向命令与配置说明见仓库根目录 **[USAGE.md §8](../USAGE.md#8-本地内容采集collect)**。

## 处理流程

```text
diggate collect <url>
  → collect.Detect（平台 URL 或 http(s) 通用网页）
  → collect.Run → runtime.Execute
  → provider/platform 或 provider/browser
  → collect.Export（+ 媒体下载 + index Upsert）
```

## 命令示例

```bash
diggate collect "https://mp.weixin.qq.com/s/..."
diggate collect --file urls.txt --format md,html,json --out ./collect-output
diggate collect inbox
diggate collect inbox --watch
```

## 输出布局

```text
collect-output/
  index.json
  2026-05-21/
    文章标题.md
    文章标题.json
    文章标题.assets/
      media.json
      001.jpg

collect-inbox/
  *.url / *.txt          # 待采集（每行一个 URL）
  processed/             # 已成功
  failed/                # 失败记录
```

## 配置

`config/config.yaml` 中 `Collect` 段：`OutputDir`、`InboxDir`、`DownloadMedia`、`IndexEnabled`、`RateLimitMs` 等。浏览器域名白名单见 `LocalBrowser.AllowedDomains`。
