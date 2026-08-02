# DigCrawl 统一 Skill 与 MCP 读取能力更新

日期：2026-08-02

## 本次更新

- 新增仓库级 `digcrawl` Skill，供 Codex、Claude Code 及其他 MCP 客户端按链接路由到平台读取工具或通用浏览器读取。
- 新增最小 MCP profile、Codex 与 Claude Code 配置样例；Phase 1 仅开放链接读取，不开放归档、下载媒体、Watchlist、浏览器交互和本地文件转写。
- MCP Tool Manifest 只公布 `AllowedTools` 允许的工具，并保留输入/输出 schema、结构化错误与 artifact 元数据。
- 支持用户明确请求截图时返回当前视口图片，图片持久化到本地 artifact store 并作为 MCP 图片附件发送。
- 新增 `Gateway.ArtifactMaxInlineBytes`，默认 `10485760`（10 MiB），用于限制截图 artifact 与 MCP 附件大小；过期 artifact 自动清理。
- `Gateway.AllowedDomains` 维持可选收紧策略：留空时允许任意 `http/https` 目标；本地运行的 DigCrawl 不承担网络隔离职责。

## 使用方式

复制 `config/digcrawl-skill.example.yaml` 到用户目录并替换路径占位符：

```bash
digcrawl mcp --config /absolute/path/to/digcrawl-skill.yaml
```

随后按 `.agents/skills/digcrawl/codex/config.toml.example` 或 `.agents/skills/digcrawl/claude-code/.mcp.json.example` 注册 MCP 服务。

## 验证

- `go test ./...`
- `go vet ./...`
- Skill 结构校验
- MCP stdio 集成测试：工具清单、schema、结构化错误与 profile 行为
