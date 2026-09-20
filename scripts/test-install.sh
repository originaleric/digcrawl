#!/bin/sh
# Test the release installer against an isolated fake GitHub delivery.
# It exercises the same shell script a user runs, including replacement of a
# stale Codex MCP path, without touching a real user configuration directory.

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/digcrawl-install-test.XXXXXX")

cleanup() {
  rm -rf "$test_root"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf '%s\n' "FAIL: $*" >&2
  exit 1
}

case "$(uname -s)" in
  Darwin) operating_system=darwin ;;
  Linux) operating_system=linux ;;
  *) fail "unsupported test platform: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) architecture=amd64 ;;
  arm64|aarch64) architecture=arm64 ;;
  *) fail "unsupported test architecture: $(uname -m)" ;;
esac

release_tag=v0.0.0-install-test
archive_name="digcrawl_0.0.0-install-test_${operating_system}_${architecture}.tar.gz"
fixture_dir="$test_root/fixture"
install_dir="$test_root/bin"
xdg_dir="$test_root/xdg"
xdg_data_dir="$test_root/xdg-data"
codex_home="$test_root/codex"
codex_config="$codex_home/config.toml"
claude_home="$test_root/claude"
fake_bin_dir="$test_root/fake-bin"
mkdir -p "$fixture_dir" "$fake_bin_dir" "$(dirname "$codex_config")"

# Every file in the official Skill must be declared in the release manifest.
# This keeps a future Skill addition from silently becoming source-only.
skill_file_list="$test_root/skill-files"
(cd "$repo_root/.agents/skills/digcrawl" && find . -type f | LC_ALL=C sort) >"$skill_file_list"
while IFS= read -r skill_file; do
  skill_source=".agents/skills/digcrawl/${skill_file#./}"
  skill_destination_dir=$(dirname "skills/digcrawl/${skill_file#./}")
  awk -v src="      - src: $skill_source" -v dst="        dst: $skill_destination_dir" -v strip="        strip_parent: true" '
    $0 == src {
      if ((getline destination) > 0 && destination == dst && (getline strip_parent) > 0 && strip_parent == strip) found = 1
    }
    END { exit(found ? 0 : 1) }
  ' "$repo_root/.goreleaser.yaml" || fail "release manifest does not package $skill_source"
done <"$skill_file_list"

(cd "$repo_root" && go build -o "$fixture_dir/digcrawl" ./cmd/digcrawl)
mkdir -p "$fixture_dir/skills"
cp -R "$repo_root/.agents/skills/digcrawl" "$fixture_dir/skills/digcrawl"
tar -czf "$fixture_dir/$archive_name" -C "$fixture_dir" digcrawl skills
tar -tzf "$fixture_dir/$archive_name" >"$test_root/archive-files"
grep -Fqx 'skills/digcrawl/SKILL.md' "$test_root/archive-files" || fail "release archive fixture does not contain the Agent Skill"
if command -v shasum >/dev/null 2>&1; then
  checksum=$(shasum -a 256 "$fixture_dir/$archive_name" | awk '{ print $1 }')
else
  checksum=$(sha256sum "$fixture_dir/$archive_name" | awk '{ print $1 }')
fi
printf '%s  %s\n' "$checksum" "$archive_name" >"$fixture_dir/checksums.txt"
printf '%s\n' '{"tag_name":"v0.0.0-install-test"}' >"$fixture_dir/release.json"

cat >"$fake_bin_dir/curl" <<'EOF'
#!/bin/sh
set -eu
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift 2 ;;
    *) url=$1; shift ;;
  esac
done
case "$url" in
  */releases/latest) source="$DIGCRAWL_INSTALL_TEST_FIXTURE/release.json" ;;
  */checksums.txt) source="$DIGCRAWL_INSTALL_TEST_FIXTURE/checksums.txt" ;;
  */digcrawl_*.tar.gz) source="$DIGCRAWL_INSTALL_TEST_FIXTURE/${url##*/}" ;;
  *) printf '%s\n' "unexpected URL: $url" >&2; exit 1 ;;
esac
cp "$source" "$output"
EOF
chmod 755 "$fake_bin_dir/curl"

cat >"$codex_config" <<'EOF'
model = "gpt-test"

[mcp_servers.keep]
command = "/usr/local/bin/keep"
args = ["serve"]

[mcp_servers.digcrawl-helper]
command = "/usr/local/bin/digcrawl-helper"
args = ["serve"]

[mcp_servers.digcrawl]
command = "/private/tmp/digcrawl-deleted/digcrawl"
args = ["mcp"]

[mcp_servers.digcrawl.env]
OLD_VALUE = "must disappear"
EOF

run_user_install() {
  PATH="$fake_bin_dir:$PATH" \
    DIGCRAWL_INSTALL_TEST_FIXTURE="$fixture_dir" \
    DIGCRAWL_INSTALL_DIR="$install_dir" \
    CODEX_HOME="$codex_home" \
    CLAUDE_CONFIG_DIR="$claude_home" \
    DIGCRAWL_SETUP_CLAUDE=1 \
    XDG_CONFIG_HOME="$xdg_dir" \
    XDG_DATA_HOME="$xdg_data_dir" \
    sh "$repo_root/scripts/install.sh" --version "$release_tag"
}

run_user_install

[ -x "$install_dir/digcrawl" ] || fail "installer did not create the stable binary"
profile="$xdg_dir/digcrawl/digcrawl-skill.yaml"
[ -f "$profile" ] || fail "installer did not create the Skill profile"
portable_skill="$xdg_data_dir/digcrawl/skills/digcrawl"
diff -ru "$repo_root/.agents/skills/digcrawl" "$portable_skill" >/dev/null || fail "installer did not install the portable official Agent Skill"
diff -ru "$repo_root/.agents/skills/digcrawl" "$codex_home/skills/digcrawl" >/dev/null || fail "installer did not install the complete official Codex Skill"
diff -ru "$repo_root/.agents/skills/digcrawl" "$claude_home/skills/digcrawl" >/dev/null || fail "installer did not install the complete Claude Code Skill"
grep -F 'model = "gpt-test"' "$codex_config" >/dev/null || fail "global Codex config was not preserved"
grep -F 'command = "/usr/local/bin/keep"' "$codex_config" >/dev/null || fail "other MCP server was not preserved"
grep -F 'command = "/usr/local/bin/digcrawl-helper"' "$codex_config" >/dev/null || fail "similarly named MCP server was not preserved"
grep -F "command = \"$install_dir/digcrawl\"" "$codex_config" >/dev/null || fail "DigCrawl command did not use installed binary"
grep -F 'args = ["mcp", "--idle-timeout", "15m", "--config",' "$codex_config" >/dev/null || fail "DigCrawl Codex adapter did not enable bounded idle recovery"
grep -F "\"$profile\"" "$codex_config" >/dev/null || fail "DigCrawl args did not use the generated Skill profile"
if grep -F '/private/tmp/digcrawl-deleted/digcrawl' "$codex_config" >/dev/null || grep -F 'OLD_VALUE' "$codex_config" >/dev/null; then
  fail "stale DigCrawl MCP configuration survived repair"
fi
if [ "$(grep -c '^\[mcp_servers\.digcrawl\]$' "$codex_config")" -ne 1 ]; then
  fail "expected exactly one DigCrawl MCP entry after first install"
fi

# A normal upgrade/reinstall must remain idempotent rather than appending a
# second entry that would make Codex reject config.toml.
run_user_install
if [ "$(grep -c '^\[mcp_servers\.digcrawl\]$' "$codex_config")" -ne 1 ]; then
  fail "reinstall produced duplicate DigCrawl MCP entries"
fi

# Codex project configuration has higher precedence than the user config. If
# a current project already owns a stale DigCrawl entry, repair that exact
# configuration without scanning or creating configuration in other projects.
project_root="$test_root/project"
project_config="$project_root/.codex/config.toml"
mkdir -p "$project_root/.git" "$(dirname "$project_config")"
cat >"$project_config" <<'EOF'
[mcp_servers.digcrawl]
command = "/private/tmp/project-digcrawl-deleted/digcrawl"
args = ["mcp"]

[mcp_servers.digcrawl.env]
OLD_VALUE = "must disappear"
EOF
(cd "$project_root" && run_user_install)
grep -F "command = \"$install_dir/digcrawl\"" "$project_config" >/dev/null || fail "project Codex config did not use installed binary"
if grep -F '/private/tmp/project-digcrawl-deleted/digcrawl' "$project_config" >/dev/null || grep -F 'OLD_VALUE' "$project_config" >/dev/null; then
  fail "stale project DigCrawl configuration survived repair"
fi
if [ "$(grep -c '^\[mcp_servers\.digcrawl\]$' "$project_config")" -ne 1 ]; then
  fail "expected exactly one project DigCrawl MCP entry after repair"
fi

# A dotfiles-managed Codex config can be a symbolic link. Repair the linked
# target while retaining the link and its restrictive permissions.
linked_root="$test_root/linked-config"
linked_target="$linked_root/dotfiles/config.toml"
linked_config="$linked_root/codex/config.toml"
linked_codex_home="$linked_root/codex-home"
mkdir -p "$(dirname "$linked_target")" "$(dirname "$linked_config")"
cat >"$linked_target" <<'EOF'
[mcp_servers.digcrawl]
command = "/private/tmp/symlinked-digcrawl-deleted/digcrawl"
args = ["mcp"]

[mcp_servers.digcrawl.env]
TOKEN = "must-stay-private"
EOF
chmod 600 "$linked_target"
ln -s "$linked_target" "$linked_config"
PATH="$fake_bin_dir:$PATH" \
  DIGCRAWL_INSTALL_TEST_FIXTURE="$fixture_dir" \
  DIGCRAWL_INSTALL_DIR="$install_dir" \
  DIGCRAWL_CODEX_CONFIG="$linked_config" \
  CODEX_HOME="$linked_codex_home" \
  DIGCRAWL_SETUP_CLAUDE=0 \
  XDG_CONFIG_HOME="$xdg_dir" \
  XDG_DATA_HOME="$xdg_data_dir" \
  sh "$repo_root/scripts/install.sh" --version "$release_tag"
[ -L "$linked_config" ] || fail "installer replaced the user's Codex configuration symlink"
grep -F "command = \"$install_dir/digcrawl\"" "$linked_target" >/dev/null || fail "installer did not repair the linked Codex configuration target"
if grep -F '/private/tmp/symlinked-digcrawl-deleted/digcrawl' "$linked_target" >/dev/null || grep -F 'TOKEN = ' "$linked_target" >/dev/null; then
  fail "stale linked DigCrawl configuration survived repair"
fi
if stat -c '%a' "$linked_target" >/dev/null 2>&1; then
  linked_mode=$(stat -c '%a' "$linked_target")
else
  linked_mode=$(stat -f '%Lp' "$linked_target")
fi
[ "$linked_mode" = 600 ] || fail "installer widened permissions on the linked Codex configuration target"

# A host-neutral install must work even when the Codex adapter is explicitly
# disabled. It delivers the portable Skill and a Claude-compatible Skill, but
# does not create a Codex configuration directory.
generic_root="$test_root/generic-only"
generic_install_dir="$generic_root/bin"
generic_config_dir="$generic_root/xdg"
generic_data_dir="$generic_root/xdg-data"
generic_codex_home="$generic_root/codex"
generic_claude_home="$generic_root/claude"
PATH="$fake_bin_dir:$PATH" \
  DIGCRAWL_INSTALL_TEST_FIXTURE="$fixture_dir" \
  DIGCRAWL_INSTALL_DIR="$generic_install_dir" \
  CODEX_HOME="$generic_codex_home" \
  CLAUDE_CONFIG_DIR="$generic_claude_home" \
  DIGCRAWL_SETUP_CLAUDE=1 \
  XDG_CONFIG_HOME="$generic_config_dir" \
  XDG_DATA_HOME="$generic_data_dir" \
  sh "$repo_root/scripts/install.sh" --version "$release_tag" --skip-codex
diff -ru "$repo_root/.agents/skills/digcrawl" "$generic_data_dir/digcrawl/skills/digcrawl" >/dev/null || fail "host-neutral install did not deliver the portable Agent Skill"
diff -ru "$repo_root/.agents/skills/digcrawl" "$generic_claude_home/skills/digcrawl" >/dev/null || fail "host-neutral install did not deliver the Claude Code Skill"
[ ! -e "$generic_codex_home" ] || fail "--skip-codex unexpectedly created Codex configuration"

"$install_dir/digcrawl" config validate --config "$profile" >/dev/null
if ! printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"install-test","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' |
  "$install_dir/digcrawl" mcp --config "$profile" 2>"$test_root/mcp.stderr" |
  grep -F '"name":"wechat.article.read"' >/dev/null; then
  fail "installed MCP server did not expose the WeChat reader"
fi
printf '%s\n' 'PASS: verified release installer delivers portable Skills and repairs effective Codex MCP paths'
