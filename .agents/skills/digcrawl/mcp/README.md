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
failure. The standard launch form bounds initialization wait, then keeps the
initialized connection until its host closes stdin. The Codex adapter uses
explicit `--idle-timeout 0`, disabling both initialization and idle reclamation.
A positive idle timeout is opt-in and requires host reconnection support.

The containing directory is an Agent Skills-compatible bundle: `SKILL.md` is
the entry point, and `references/` contains progressively loaded guidance.
Copy or link the complete `digcrawl` directory—not only `SKILL.md`—into the
directory your host documents for user or project Skills. The release installer
also keeps a portable copy under its printed data path. It installs the Codex
adapter automatically and installs the Claude Code Skill when that host is
detected (or when `DIGCRAWL_SETUP_CLAUDE=1` is set); MCP registration remains
host-owned outside Codex.

The stdio process forwards calls to one private Bridge per profile. The Bridge
owns the Runtime and Chrome and survives stdio reconnects. After upgrading,
close old DigCrawl MCP connections, run the installed binary's
`digcrawl bridge restart --config <actual-profile-absolute-path>`, then reconnect.
A manifest build mismatch requires this explicit step.

For browser failures, preserve the structured `error_details` beside `error`.
When `handled: true`, the browser owner has already made its retry decision;
follow `next_actions` before a new read. Tools depend on the active profile:
use `tools/list` and do not assume an independent `browser.snapshot` is exposed.
The default Skill uses `browser.browse` with `mode: "snapshot"` instead.

The Bridge reserves a durable, profile-scoped fault recovery budget under its
existing lifecycle lock: up to three automatic recoveries, with two-second
backoff. Initial starts, starts after verified controlled stops, and explicit
operator restarts neither consume recovery attempts nor erase previous failures.
Stopping an already-absent crashed worker does not mark its exit clean. Proxy
reconnection does not reset the budget. Version 1 records migrate conservatively
to version 2; older binaries cannot directly consume version 2 records. Sustained successful RPC
observations reset it after ten minutes (gaps over one minute break continuity).
This is process liveness, not browser/application readiness. `doctor` reports
transport, build, recovery and browser operation separately. Explicit
`bridge restart` allows one operator start without refunding automatic budget.
An interrupted post-dispatch call reports `execution_state=unknown`; do not replay
it blindly, including readers that can save media under the active profile.

During `bridge stop`, starts are rejected. After stop finishes, a later request
may start again. An interrupted stop can leave a private `.stop-*` intent; inspect
the matching owner/cleanup before operator removal, never bulk-delete locks.

When configured OCR is enabled, generic browser reads acquire bounded image bytes
inside the existing page session (including same-origin blobs) and submit inline
images. Cross-origin images use an anonymous fetch first, permitting public
CORS-* CDNs; an authorization/fetch failure permits at most one credentialed
attempt within the same deadline. Browser CORS and domain policy still apply. No browser credential is sent to the OCR service. CORS/access failures
remain failures; the original body is retained. Per-image failures or truncated
image budgets report degraded OCR, rather than claiming complete recognition.
