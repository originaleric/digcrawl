# Source discovery

`source.discover` enumerates source links through DigCrawl's existing source readers. The host owns scheduling, deduplication, detail collection, summaries, and notifications.

## Availability and configuration

Confirm `source.discover` through MCP `tools/list` or HTTP `GET /manifest` for the actual profile. This build includes it in newly generated Skill profiles and `config/digcrawl-skill.example.yaml`. An existing custom profile with a non-empty `Gateway.AllowedTools` needs an explicit operator addition. Follow the existing upgrade/reconnection procedure when loading an updated binary or profile.

Paging requires `Gateway.ArtifactEnabled=true` and a writable `Gateway.ArtifactDir`. Snapshots use `Gateway.ArtifactTTLMinutes` and retain their original expiration while pages are consumed.

## Input

| Field | Contract |
|---|---|
| `url` | Required source URL |
| `source_kind` | Required kind from the active tool schema |
| `platform` | Required for `source_kind=article`. For other kinds, an optional value must match the platform below |
| `limit` | Entries per response, 1–100, default 100 |
| `checkpoint` | Returned `next_checkpoint` passed unchanged, at most 2048 characters |

Only these input fields are accepted. Use MCP `tools/call` with `name: source.discover` and the following `arguments` object, replacing the example source URL.

```json
{
  "url": "https://example.org/feed.xml",
  "source_kind": "rss",
  "limit": 20
}
```

## Supported kinds

| `source_kind` | Platform |
|---|---|
| `article` | Caller-supplied `platform`, such as `web` or `toutiao` |
| `rss` | `rss` |
| `sitemap` | `sitemap` |
| `web_list` | `web` |
| `x_user` | `x` |
| `reddit_subreddit`, `reddit_user` | `reddit` |
| `wechat_list` | `wechat` |
| `toutiao_author` | `toutiao` |
| `xiaohongshu_user` | `xiaohongshu` |
| `douyin_user` | `douyin` |
| `bilibili_user` | `bilibili` |
| `youtube_channel` | `youtube` |
| `weibo_user` | `weibo` |
| `kuaishou_user` | `kuaishou` |
| `zhihu_user` | `zhihu` |
| `threads_user` | `threads` |
| `instagram_user` | `instagram` |

`article` returns the caller-supplied detail URL. Use the matching detail tool to read its content. Account/list kinds expose the existing readers' visible results. Actual availability depends on the platform and current access conditions.

## Output and coverage

The result includes `items`, `coverage`, `reason`, `has_more`, `next_checkpoint`, and `observed_at`. Each item includes `content_key`, `canonical_url`, `platform`, `title`, `author`, `published_at`, and `observed_at`. Source fields such as title, author, and publication time may be empty. The following response illustrates a finite feed containing one entry.

```json
{
  "items": [
    {
      "content_key": "example-entry",
      "canonical_url": "https://example.org/article",
      "platform": "rss",
      "title": "Example entry",
      "author": "",
      "published_at": "",
      "observed_at": "2026-11-24T00:00:00Z"
    }
  ],
  "coverage": "complete",
  "reason": "all entries in the current feed/sitemap response; older disappeared entries are not recoverable",
  "has_more": false,
  "next_checkpoint": "",
  "observed_at": "2026-11-24T00:00:00Z"
}
```

| `coverage` | Meaning |
|---|---|
| `complete` | Enumerated the supplied detail URL or all entries in the current finite RSS/sitemap response |
| `partial` | The source reader reached its item limit |
| `unknown` | Account/web-list visible snapshot with unresolved historical coverage |

Older entries that disappeared upstream remain outside the current feed/sitemap response. An empty list with `unknown` coverage leaves historical coverage unresolved. The adapter currently requests up to 1000 reader items and marks reaching that limit as `partial`. Account/web-list readers provide no upstream historical continuation.

## Paging and recovery

When `has_more=true`, repeat the call using the same `url`, `source_kind`, and platform identity, with the returned `next_checkpoint` as `checkpoint`. `limit` can change. Keep checkpoint values opaque.

Pages belong to one saved immutable snapshot. `has_more` reports remaining pages within that snapshot independently of `coverage`. The checkpoint is tied to the source identity and is rechecked against the current domain policy.

- `CHECKPOINT_EXPIRED` means the saved snapshot expired, disappeared, or could not be read. Start discovery again without a checkpoint and keep the host baseline until the fresh scan has been processed.
- `CHECKPOINT_UNAVAILABLE` means paging needs an available artifact store. Have the operator check artifact settings and write access, then start a fresh scan.
- `SOURCE_LIMIT_EXCEEDED` means snapshot item or byte limits were exceeded. Narrow the source. Reducing response `limit` only changes paging.
- `INVALID_INPUT` includes unknown input fields, invalid budgets, malformed checkpoints, and source/platform identity mismatches.
- `CAPABILITY_UNAVAILABLE` indicates an unsupported source kind or missing required article platform.

Continue with detail reads for returned URLs. Account/list access failures retain the existing error handling. Preserve login, captcha, rate-limit, and domain errors. Automatic browser fallback is disabled for discovery, and the call does not enable login, media saving, or ASR. See [error handling](error-handling.md).
