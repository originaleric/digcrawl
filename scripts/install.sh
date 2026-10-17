#!/bin/sh
# Install a verified DigCrawl release for macOS or Linux, including its
# portable Agent Skill and available local host adapters.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh -s -- --version v1.2.3
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh -s -- --install-dir "$HOME/bin"
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh -s -- --skip-codex

set -eu

repository="originaleric/digcrawl"
release_tag="${DIGCRAWL_VERSION:-}"
install_dir="${DIGCRAWL_INSTALL_DIR:-$HOME/.local/bin}"
codex_home="${CODEX_HOME:-$HOME/.codex}"
codex_config="${DIGCRAWL_CODEX_CONFIG:-$codex_home/config.toml}"
agent_data_home="${XDG_DATA_HOME:-$HOME/.local/share}/digcrawl"
agent_skill_dir="${DIGCRAWL_SKILL_DIR:-$agent_data_home/skills/digcrawl}"
claude_home="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
setup_agents="${DIGCRAWL_SETUP_AGENTS:-1}"
setup_codex="${DIGCRAWL_SETUP_CODEX:-1}"

usage() {
  cat <<'EOF'
Install DigCrawl from an official GitHub Release.

Usage: install.sh [--version <tag>] [--install-dir <directory>] [--skip-codex] [--skip-agent-setup]

Options:
  --version <tag>          Install a specific release tag, for example v1.2.3.
  --install-dir <directory> Install the binary here (default: $HOME/.local/bin).
  --skip-codex             Do not install the Codex adapter or update its MCP configuration.
  --skip-agent-setup       Install the binary only; do not install the portable Skill or configure any adapter.
  -h, --help               Show this help.

Environment variables:
  DIGCRAWL_VERSION         Same as --version.
  DIGCRAWL_INSTALL_DIR     Same as --install-dir.
  DIGCRAWL_CODEX_CONFIG    Codex config.toml to update (default: $CODEX_HOME/config.toml or $HOME/.codex/config.toml).
  DIGCRAWL_SKILL_DIR       Portable Agent Skill destination (default: $XDG_DATA_HOME/digcrawl/skills/digcrawl or $HOME/.local/share/digcrawl/skills/digcrawl).
  DIGCRAWL_SETUP_AGENTS    Set to 0 to skip portable Skill and auto-detected host setup.
  DIGCRAWL_SETUP_CODEX     Set to 0 to skip the Codex adapter.
  DIGCRAWL_SETUP_CLAUDE    Set to 1 to install the Claude Code Skill; 0 skips it; auto (default) installs only when Claude is detected.

The installer downloads the release archive and checksums.txt from GitHub,
verifies SHA-256 before installation, never uses sudo or edits shell files,
and installs the portable Agent Skill plus an MCP stdio launch contract. The
Agent Skills format standardizes the Skill contents, not a universal host
configuration location, so host adapters remain opt-in and scoped. Codex is
repaired automatically; when run inside a project with an existing
[mcp_servers.digcrawl] entry, that higher-priority project configuration is
repaired instead of only the user configuration.
EOF
}

fail() {
  printf '%s\n' "error: $*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a release tag"
      release_tag="$2"
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || fail "--install-dir requires a directory"
      install_dir="$2"
      shift 2
      ;;
    --skip-codex)
      setup_codex=0
      shift
      ;;
    --skip-agent-setup)
      setup_agents=0
      setup_codex=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

case "$(uname -s)" in
  Darwin) operating_system="darwin" ;;
  Linux) operating_system="linux" ;;
  *) fail "unsupported operating system: $(uname -s); download a release manually" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) architecture="amd64" ;;
  arm64|aarch64) architecture="arm64" ;;
  *) fail "unsupported architecture: $(uname -m); download a release manually" ;;
esac

temporary_dir="$(mktemp -d 2>/dev/null || mktemp -d -t digcrawl-install)"
cleanup() {
  rm -rf "$temporary_dir"
}
trap cleanup EXIT HUP INT TERM

profile_path() {
  if [ -n "${XDG_CONFIG_HOME:-}" ]; then
    printf '%s/digcrawl/digcrawl-skill.yaml' "$XDG_CONFIG_HOME"
    return
  fi
  printf '%s/.config/digcrawl/digcrawl-skill.yaml' "$HOME"
}

toml_escape() {
  # TOML basic strings escape backslashes and double quotes. Install paths are
  # user-controlled, so never interpolate them into config.toml unescaped.
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

codex_config_mode() {
  if stat -c '%a' "$1" >/dev/null 2>&1; then
    stat -c '%a' "$1"
    return
  fi
  stat -f '%Lp' "$1"
}

has_digcrawl_mcp_entry() {
  [ -f "$1" ] || return 1
  awk '
    /^[[:space:]]*\[\[?[[:space:]]*mcp_servers[[:space:]]*\.[[:space:]]*[\"]?digcrawl[\"]?[[:space:]]*[\].]/ {
      found = 1
      exit
    }
    END { exit(found ? 0 : 1) }
  ' "$1"
}

select_codex_config() {
  config_scope="user"
  [ -n "${DIGCRAWL_CODEX_CONFIG:-}" ] && {
    config_scope="explicit"
    return
  }

  current_dir="$(pwd -P)"
  while :; do
    candidate_config="$current_dir/.codex/config.toml"
    if has_digcrawl_mcp_entry "$candidate_config"; then
      codex_config="$candidate_config"
      config_scope="project"
      return
    fi

    # Do not inspect or create configuration outside the current project.
    [ -e "$current_dir/.git" ] && return
    parent_dir="$(dirname "$current_dir")"
    [ "$parent_dir" = "$current_dir" ] && return
    current_dir="$parent_dir"
  done
}

resolve_codex_config_target() {
  [ -L "$codex_config" ] || return 0

  link_target=$(readlink "$codex_config") || fail "could not resolve symbolic link $codex_config"
  [ -n "$link_target" ] || fail "could not resolve symbolic link $codex_config"
  case "$link_target" in
    /*) codex_config="$link_target" ;;
    *) codex_config="$(dirname "$codex_config")/$link_target" ;;
  esac
  [ ! -L "$codex_config" ] || fail "Codex configuration symlink chains are unsupported; set DIGCRAWL_CODEX_CONFIG to the final file"
}

write_codex_mcp_config() {
  config_dir=$(dirname "$codex_config")
  mkdir -p "$config_dir"
  config_tmp=$(mktemp "$config_dir/.config.toml.digcrawl.XXXXXX")
  config_source="$config_tmp.source"

  if [ -f "$codex_config" ]; then
    cp "$codex_config" "$config_source" || fail "could not read $codex_config"
    config_mode=$(codex_config_mode "$codex_config") || fail "could not read permissions for $codex_config"
    # Remove only the DigCrawl table and any table nested below it. Everything
    # else stays byte-for-byte intact, including comments and other MCP
    # servers. Codex itself writes this canonical table form.
    awk '
      function is_header(line, trimmed) {
        trimmed = line
        sub(/^[[:space:]]*/, "", trimmed)
        return trimmed ~ /^\[\[?/
      }
      function is_digcrawl_header(line, trimmed) {
        trimmed = line
        sub(/^[[:space:]]*\[\[?[[:space:]]*/, "", trimmed)
        return trimmed ~ /^mcp_servers[[:space:]]*\.[[:space:]]*[\"]?digcrawl[\"]?[[:space:]]*[\].]/
      }
      {
        if (is_header($0)) {
          skip = is_digcrawl_header($0)
        }
        if (!skip) print
      }
    ' "$config_source" >"$config_tmp" || fail "could not update $codex_config"
    chmod "$config_mode" "$config_tmp" || fail "could not preserve permissions for $codex_config"
    config_existed=1
  else
    : >"$config_tmp"
    chmod 600 "$config_tmp"
    config_existed=0
  fi

  if [ -s "$config_tmp" ]; then
    printf '\n' >>"$config_tmp"
  fi
  printf '%s\n' '[mcp_servers.digcrawl]' >>"$config_tmp"
  printf 'command = "%s"\n' "$(toml_escape "$install_dir/digcrawl")" >>"$config_tmp"
  printf 'args = ["mcp", "--idle-timeout", "15m", "--config", "%s"]\n' "$(toml_escape "$skill_profile")" >>"$config_tmp"

  if [ "$config_existed" = 1 ] && ! cmp -s "$config_source" "$codex_config"; then
    rm -f "$config_tmp" "$config_source"
    fail "Codex configuration changed during installation; no configuration was replaced, retry the installer"
  fi
  if [ "$config_existed" = 0 ] && [ -e "$codex_config" ]; then
    rm -f "$config_tmp"
    fail "Codex configuration was created during installation; no configuration was replaced, retry the installer"
  fi
  mv "$config_tmp" "$codex_config" || fail "could not replace $codex_config"
  [ "$config_existed" = 0 ] || rm -f "$config_source"
}

install_skill_at() {
  skill_source="$temporary_dir/skills/digcrawl"
  skill_target="$1"
  [ -f "$skill_source/SKILL.md" ] || fail "release archive did not contain the DigCrawl Agent Skill"

  skill_root=$(dirname "$skill_target")
  skill_dir="$skill_target"
  mkdir -p "$skill_root"
  skill_stage="$(mktemp -d "$skill_root/.digcrawl.stage.XXXXXX")"
  cp -R "$skill_source"/. "$skill_stage"/ || fail "could not stage the DigCrawl Agent Skill"
  [ -f "$skill_stage/SKILL.md" ] || fail "staged DigCrawl Agent Skill is incomplete"

  skill_backup=""
  if [ -e "$skill_dir" ] || [ -L "$skill_dir" ]; then
    skill_backup="$skill_root/.digcrawl.previous.$$"
    [ ! -e "$skill_backup" ] && [ ! -L "$skill_backup" ] || fail "could not safely replace $skill_dir"
    mv "$skill_dir" "$skill_backup" || fail "could not stage the existing DigCrawl Agent Skill"
  fi
  if ! mv "$skill_stage" "$skill_dir"; then
    [ -z "$skill_backup" ] || mv "$skill_backup" "$skill_dir"
    fail "could not install the DigCrawl Agent Skill"
  fi
  [ -z "$skill_backup" ] || rm -rf "$skill_backup"
}

ensure_skill_profile() {
  skill_profile=$(profile_path)
  if [ ! -f "$skill_profile" ]; then
    "$install_dir/digcrawl" init --profile skill
  fi
  "$install_dir/digcrawl" config validate --config "$skill_profile"
}

retire_standard_bridge() {
  bridge_controller="$1"
  bridge_profile=$(profile_path)
  # A fresh install has no previous Bridge to stop. Keep this a successful
  # no-op under set -e so installation can create the first Skill profile.
  [ -f "$bridge_profile" ] || return 0
  "$bridge_controller" bridge stop --config "$bridge_profile"
  printf '%s\n' "Stopped the previous DigCrawl Bridge for $bridge_profile before replacing the installed binary."
}

install_claude_skill_if_detected() {
  case "${DIGCRAWL_SETUP_CLAUDE:-auto}" in
    0) return ;;
    1) ;;
    auto|"")
      [ -d "$claude_home" ] || command -v claude >/dev/null 2>&1 || return
      ;;
    *) fail "DIGCRAWL_SETUP_CLAUDE must be auto, 0, or 1" ;;
  esac
  install_skill_at "$claude_home/skills/digcrawl"
  printf '%s\n' "Installed DigCrawl Agent Skill for Claude Code in $claude_home/skills/digcrawl"
}

setup_agent_assets() {
  [ "$setup_agents" = "0" ] && return

  ensure_skill_profile
  install_skill_at "$agent_skill_dir"
  install_claude_skill_if_detected
  printf '%s\n' "Installed portable DigCrawl Agent Skill in $agent_skill_dir"
  printf '%s\n' "For any MCP host, configure stdio command '$install_dir/digcrawl mcp --config $skill_profile'; host-specific examples are in $agent_skill_dir."
}

setup_codex_mcp() {
  [ "$setup_codex" = "0" ] && return

  ensure_skill_profile
  # Preserve a user's dotfiles link: modify its target rather than replacing
  # the link with a regular file during the atomic rename below.
  resolve_codex_config_target
  install_skill_at "$codex_home/skills/digcrawl"
  write_codex_mcp_config
  printf '%s\n' "Installed DigCrawl Agent Skill for Codex in $codex_home/skills/digcrawl"
  printf '%s\n' "Configured Codex MCP server 'digcrawl' in $codex_config ($config_scope scope)"
  printf '%s\n' "Restart Codex Desktop, then start a new task to load the repaired MCP tool catalog."
}

if [ -z "$release_tag" ]; then
  printf '%s\n' "Looking up the latest DigCrawl release..."
  curl --fail --location --silent --show-error \
    "https://api.github.com/repos/$repository/releases/latest" \
    --output "$temporary_dir/release.json"
  release_tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$temporary_dir/release.json" | head -n 1)"
  [ -n "$release_tag" ] || fail "could not determine the latest release tag"
fi

archive_version="${release_tag#v}"
archive_name="digcrawl_${archive_version}_${operating_system}_${architecture}.tar.gz"
release_url="https://github.com/$repository/releases/download/$release_tag"

printf '%s\n' "Downloading DigCrawl $release_tag for $operating_system/$architecture..."
curl --fail --location --silent --show-error --retry 3 \
  "$release_url/$archive_name" \
  --output "$temporary_dir/$archive_name"
curl --fail --location --silent --show-error --retry 3 \
  "$release_url/checksums.txt" \
  --output "$temporary_dir/checksums.txt"

expected_checksum="$(awk -v name="$archive_name" '{ file = $2; sub(/^\*/, "", file); if (file == name) { print $1; exit } }' "$temporary_dir/checksums.txt")"
[ -n "$expected_checksum" ] || fail "checksum for $archive_name was not found in checksums.txt"

if command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "$temporary_dir/$archive_name" | awk '{ print $1 }')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$temporary_dir/$archive_name" | awk '{ print $1 }')"
else
  fail "shasum or sha256sum is required to verify the download"
fi

[ "$expected_checksum" = "$actual_checksum" ] || fail "SHA-256 verification failed; nothing was installed"

tar -xzf "$temporary_dir/$archive_name" -C "$temporary_dir"
[ -f "$temporary_dir/digcrawl" ] || fail "release archive did not contain the digcrawl binary"
[ -f "$temporary_dir/skills/digcrawl/SKILL.md" ] || fail "release archive did not contain the DigCrawl Agent Skill"

umask 022
[ "$setup_codex" = "0" ] || select_codex_config
mkdir -p "$install_dir"
# Use the verified staged binary to take over the previous runtime before
# overwriting its executable. This avoids an executing-file replacement race
# and guarantees the next MCP process starts the just-installed binary.
retire_standard_bridge "$temporary_dir/digcrawl"
install -m 0755 "$temporary_dir/digcrawl" "$install_dir/digcrawl"

printf '%s\n' "Installed DigCrawl $release_tag to $install_dir/digcrawl"
"$install_dir/digcrawl" version

setup_agent_assets
setup_codex_mcp

case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *)
    printf '%s\n' "Add $install_dir to PATH to run 'digcrawl' directly. The installer did not modify your shell configuration."
    ;;
esac
