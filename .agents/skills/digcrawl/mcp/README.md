# Generic MCP stdio contract

DigCrawl exposes a local MCP server over standard input and standard output.
MCP standardizes that transport and its JSON-RPC messages, but it intentionally
does not define one configuration-file format or one Skill discovery directory
for every host.

Configure any MCP-capable host to launch the installed binary with these exact
values, substituting absolute paths:

```text
command: /absolute/path/to/digcrawl
args:    ["mcp", "--config", "/absolute/path/to/digcrawl-skill.yaml"]
```

The command writes MCP JSON-RPC messages only to stdout. It writes diagnostics
to stderr, so a host must not treat stderr output by itself as a protocol
failure. The standard launch form deliberately has no host-specific idle
timeout. The Codex example is an adapter exception because it needs to clean
up abandoned stdin pipes.

The containing directory is an Agent Skills-compatible bundle: `SKILL.md` is
the entry point, and `references/` contains progressively loaded guidance.
Copy or link the complete `digcrawl` directory—not only `SKILL.md`—into the
directory your host documents for user or project Skills. The release installer
also keeps a portable copy under its printed data path. It installs the Codex
adapter automatically and installs the Claude Code Skill when that host is
detected (or when `DIGCRAWL_SETUP_CLAUDE=1` is set); MCP registration remains
host-owned outside Codex.
