# Reddit 抓取与 Watchlist 实现测试报告

## 范围

- Reddit URL normalize。
- Collect detector 路由。
- Reddit provider 工具注册与 schema 编译。
- Watchlist `Options` 透传。
- Reddit listing 到 watchlist `FeedItem` 映射。
- 全仓 Go 单元测试和编译测试。

## 命令

```bash
TMPDIR=/private/tmp GOCACHE=$PWD/.tmp/go-build GOMODCACHE=$PWD/.tmp/go-mod go test ./...
```

## 结果

状态：PASSED

说明：

- 全量 Go 测试通过，退出码为 0。
- 首次测试时使用系统 Go cache 被 sandbox 权限阻止，已改用仓库内 `.tmp/go-build` / `.tmp/go-mod`。
- macOS clang/xcrun 仍打印 `/var/folders/.../xcrun_db-*` cache 权限噪声，但未导致测试失败。

## 新增覆盖

- `internal/platform/reddit/url_test.go`
  - subreddit 单帖 URL 规范化。
  - `redd.it/{id}` 短链规范化。
  - listing URL 不被识别为单帖。
- `internal/platform/reddit/client_test.go`
  - `more` 评论 children 收集、去重和 `ExpandMoreLimit` 限制。
  - `time_range` 参数归一化。
  - 429 `Retry-After` / `MinRetryAfterSec` header-driven 退避。
- `collect/detector_test.go`
  - Reddit 单帖命中 `reddit.post.read`。
  - Reddit listing URL 回退到 web/browser。
- `internal/watchlist/service_test.go`
  - Watchlist item `Options` 透传。
- `internal/watchlist/feed_test.go`
  - Reddit post/comment listing 映射为稳定 `FeedItem`。
  - Reddit subreddit/user URL 参数解析。
  - 空 `Type` 的 Reddit post URL 自动推断为 `reddit_post_comments`，避免被 `/r/` subreddit 规则误判。

## 未执行项

- 未执行真实 Reddit API 外网请求。

原因：

- 当前环境没有提供 Reddit app OAuth 凭证。
- 为避免误用未授权抓取，默认配置不允许无 OAuth 自动浏览器读取 Reddit。

可重试前置：

```bash
export REDDIT_CLIENT_ID="<client-id>"
export REDDIT_CLIENT_SECRET="<client-secret>"
```

然后执行：

```bash
digcrawl collect "https://www.reddit.com/r/golang/comments/<id>/<title>/" --format md,json --config config/config.yaml
digcrawl watchlist run --once --config config/config.yaml
```

## Review 修复验证

修复项：

- `inferRedditWatchlistType` 在空 `Type` 时优先识别 Reddit post URL。
- 帖子 URL `https://www.reddit.com/r/{sub}/comments/{id}/...` 不再被 subreddit 自动推断分支吞掉。

验证命令：

```bash
TMPDIR=/private/tmp GOCACHE=/private/tmp/digcrawl-go-build GOMODCACHE=/private/tmp/digcrawl-go-mod go test ./internal/watchlist
git diff --check
```

结果：

- `go test ./internal/watchlist` 通过。
- `git diff --check` 通过。
