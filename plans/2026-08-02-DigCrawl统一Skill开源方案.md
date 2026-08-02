# DigCrawl 统一 Skill 开源方案

日期：2026-08-02

## 1. 目标

将 DigCrawl 作为用户本机运行的内容获取能力，提供给 Codex、Claude Code、WorkBuddy 及其他 Agent 使用。用户只需发送链接或描述任务，Agent 即可通过 DigCrawl 读取、提炼、归档或持续监控内容。

典型场景包括：微信公众号文章读取与总结、普通网页和社交平台内容提取、视频转写、资料归档与来源监控。

## 2. 命名与产品边界

统一使用 **`digcrawl`** 作为：

- 仓库与产品品牌；
- CLI 二进制和 MCP Server 名称；
- 面向 Agent 的 Skill 名称；
- 后续各平台适配包的统一前缀。

不创建 `digcrawl-content-reader` 等按单一功能切分的一级 Skill。内容读取、归档、OCR、ASR 与 Watchlist 是同一能力体系中的不同动作，通过用户意图或参数区分。

```text
digcrawl
├── read       默认：读取链接并返回结构化正文
├── summarize  读取后生成摘要、问答或提纲
├── archive    导出 Markdown / HTML / JSON，可选下载媒体
├── extract    OCR、ASR、链接/表格等内容提取
├── batch      批量 URL 采集
└── watch      创建、执行或检查持续监控
```

推荐的上层统一请求语义：

```json
{
  "action": "read | archive | extract | watch",
  "url": "https://mp.weixin.qq.com/...",
  "options": {
    "summary": false,
    "download_media": false,
    "ocr": false,
    "asr": false,
    "formats": ["markdown", "json"]
  }
}
```

底层不必强行合并现有细粒度 MCP 工具。Skill 根据 URL 平台路由到 `wechat.article.read`、各平台 `*.read`、`feed.read` 或 `browser.browse`，从而保留 Runtime 的最小权限、审计与故障诊断能力。

## 3. 架构

```text
Codex / Claude Code / WorkBuddy / 其他 Agent
                    │
      digcrawl Skill（意图识别、路由、安全和结果处理）
                    │
       MCP stdio 优先；HTTP Gateway / stdio 协议兼容
                    │
              DigCrawl Runtime
                    │
平台 Provider / 本机浏览器 / 用户自己的登录态
```

DigCrawl 本身已经具备 MCP stdio 服务（`digcrawl mcp`）、HTTP Gateway、stdio JSON 协议和统一 Tool Manifest。Skill 只承担跨 Agent 的调用规范和流程引导，不复制采集、解析、Cookie 管理或平台策略代码。

链接读取默认允许任意 `http/https` 目标，避免让普通用户维护站点白名单。DigCrawl 是本地用户工具，不承担网络隔离职责；`Gateway.AllowedDomains` 是企业或高安全场景的可选收紧策略，而不是普通用户的前置配置。

## 4. Agent 调用规则

1. 识别用户给出的 URL、任务意图和是否要求保存内容。
2. 按 URL 域名选择平台工具；微信公众号优先 `wechat.article.read`。
3. 仅当平台工具不适用时回退到 `browser.browse`。
4. 成功结果统一整理为标题、作者、发布时间、正文 Markdown、规范化 URL、媒体、平台元数据和采集时间。
5. 由 Agent 在结果之上完成摘要、问答、改写、分类或研究，不将模型推测伪装为读取结果。
6. 如遇 `LOGIN_REQUIRED`、`CAPTCHA_REQUIRED`、`RATE_LIMITED`、私密/删除内容等，原样解释原因并给出下一步；不得绕过访问控制。
7. 默认只读。下载媒体、写入归档、修改 Watchlist、浏览器交互登录等操作必须由用户明确提出。

## 5. 跨 Agent 接入

| 客户端 | 首选接入 | 适配内容 |
|---|---|---|
| Codex | MCP stdio：`digcrawl mcp --config ...` | `SKILL.md`、Codex MCP 配置示例与安装脚本 |
| Claude Code | MCP stdio：同一命令 | Claude 项目级 Skill/Command 与 MCP 配置示例 |
| WorkBuddy | 优先 MCP；无 MCP 时使用 HTTP Gateway 或 stdio adapter | 宿主专属配置与薄适配层 |
| 其他 Agent | MCP 优先；HTTP `/manifest`、`/tools/call` 兜底 | 基于 Tool Manifest 动态发现工具 |

所有适配器复用同一套 `digcrawl` Skill 路由策略和错误语义，不复制平台解析逻辑。

## 6. 开源边界

DigCrawl Core 与 Skill 同时开源并不冲突：前者是运行时和协议实现，后者是 Agent 调用规范。现有仓库为 MIT 许可证，第一期可让 Skill 与主仓库保持同一许可证、同一版本节奏。

| 层 | 可开源并随发行物提供 | 禁止提交、打包或共享 |
|---|---|---|
| DigCrawl Core | Go 代码、MCP、CLI、HTTP 协议、测试、无密钥示例配置 | 真实 token、API key、数据库密码、用户采集数据 |
| `digcrawl` Skill | `SKILL.md`、Agent 配置样例、调用策略、安装脚本 | 用户目录、浏览器 profile、Cookie、会话状态 |
| 平台访问 | 公开内容读取、用户主动完成登录后的本机会话复用 | 绕过登录、验证码、付费墙或平台访问控制 |
| 内容输出 | 用户本地生成的 Markdown/HTML/JSON | 第三方全文样本库、未获授权的媒体再分发 |

产品定位应明确写入 README 与 Skill：

> DigCrawl 是运行在用户本机的开源内容获取基础设施；`digcrawl` Skill 是其面向 Agent 的开源调用规范，不托管、不共享、不绕过用户的身份凭证与内容访问权限。

同时声明使用者应遵守目标平台条款、robots、速率限制、版权与适用法律；登录态始终仅保存在用户本机，Agent 不得读取、导出或上传。

## 7. 仓库组织

第一期在主仓库内维护，不单独拆库：

```text
.agents/skills/
└── digcrawl/
    ├── SKILL.md
    ├── references/
    │   ├── tool-routing.md
    │   └── error-handling.md
    ├── codex/
    │   └── config.toml.example
    ├── claude-code/
    │   └── .mcp.json.example
```

Phase 1 已提供 Codex、Claude Code 的 MCP 配置样例。WorkBuddy 等其他 MCP 客户端使用同一条 `digcrawl mcp --config <profile>` 命令接入；待其配置格式稳定后再补宿主专属示例。

当 Skill 需要独立发布节奏、独立维护社区适配或发布到多家市场时，再拆分为 `digcrawl-skill` 仓库；其版本通过兼容矩阵标明支持的 DigCrawl Runtime 版本。

## 8. 分期实施

### Phase 1：链接读取 MVP

- 新建 `skills/digcrawl/SKILL.md`；
- 提供 Codex、Claude Code 的 MCP 安装样例；
- 使用最小安全工具集：`gateway.ping`、`wechat.article.read`、其他平台 `*.read`、`feed.read`、`browser.browse`；
- 统一成功输出和结构化错误处理；
- 验收微信公众号公开文章、动态网页、视频页、短链、登录失效与频控等真实场景。

### Phase 2：归档与内容增强

- 接入 `collect` 归档、媒体下载和本地索引；
- 允许明确请求触发 OCR、ASR 与结构化提取；
- 支持批量 URL、研究资料包和可追溯来源清单。

### Phase 3：持续监控与发行

- 以显式确认方式接入 Watchlist 的创建、运行与状态检查；
- 提供一键安装：构建二进制、写入本地配置、注册 MCP、健康检查；
- 发布 GitHub Release，并按各 Agent 生态要求提交 Skill/插件市场。

## 9. 开源前检查清单

1. 扫描 Git 历史、配置、测试报告和构建产物，确认无 API key、token、Cookie、数据库凭证和个人数据。
2. 所有示例配置都使用 `${ENV_VAR}` 或占位符；真实配置仅存在于 `.gitignore` 覆盖范围内。
3. 当前 `config/config.yaml` 和 `config/config.go` 中存在看似具体的 X OAuth `ConsumerKeySecret` 值；若该值有效，公开前必须立即撤销/轮换，并改为环境变量注入，不能编译进默认配置。
4. 确认第三方依赖许可证、第三方平台条款与 README 中的使用限制说明。
5. 为 MCP `AllowedTools`、`AllowedDomains`、本地监听、用户确认和结构化错误建立自动化测试。
6. 以干净的临时用户目录验证：从安装到读取一篇公开微信文章，无需修改源码或暴露任何敏感信息。
