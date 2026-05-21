# Collect 模块

DigGate 内置的**无宿主本地内容采集**：将链接保存为 Markdown / HTML / JSON，可选下载媒体到 `.assets/`，并按 `PersistMode` 维护 `index.json` 和/或 SQLite（`collect.db`）。

用户向命令与配置说明见仓库根目录 **[USAGE.md §8](../USAGE.md#8-本地内容采集collect)**。

## 处理流程

```text
diggate collect <url>
  → collect.Detect（平台 URL 或 http(s) 通用网页）
  → collect.Run → runtime.Execute
  → provider/platform 或 provider/browser
  → collect.Export（+ 媒体下载）
  → persist（index.json / SQLite，由 PersistMode 决定）
```

## 命令示例

```bash
diggate collect "https://mp.weixin.qq.com/s/..."
diggate collect --file urls.txt --format md,html,json --out ./collect-output
diggate collect inbox
diggate collect inbox --watch
diggate collect list                    # 需 PersistMode=sqlite|both
diggate collect show <id|url>
diggate collect migrate-index           # 从 index.json 导入 SQLite
```

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

storage/diggate/
  collect.db                 # PersistMode=sqlite|both

collect-inbox/
  *.url / *.txt          # 待采集（每行一个 URL）
  processed/             # 已成功
  failed/                # 失败记录
```

## 配置

`config/config.yaml` 中 `Collect` 段：

| 字段 | 说明 |
|------|------|
| `PersistMode` | `files` / `index`（默认）/ `sqlite` / `both` |
| `DBPath` | SQLite 路径，默认 `storage/diggate/collect.db` |
| `DownloadMedia` | 下载 `media[]` 到 `.assets/`（公众号需 `include_media`） |
| `IndexEnabled` | `index`/`both` 模式下可显式关闭 index.json |

浏览器域名白名单见 `LocalBrowser.AllowedDomains`。

## 与 Knowledge 入库的差异

Collect 以**本地文件为真源**（md/json/assets），SQLite 仅存元数据与路径；DigEino → Knowledge 的向量/业务库需宿主自行 ingest。JSON 工具输出中的 `media[]` 可供 ingest 解析远程 URL 或本地 `.assets/` 路径。
