#!/bin/sh
# Install a verified DigCrawl release for macOS or Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh -s -- --version v1.2.3
#   curl -fsSL https://raw.githubusercontent.com/originaleric/digcrawl/main/scripts/install.sh | sh -s -- --install-dir "$HOME/bin"

set -eu

repository="originaleric/digcrawl"
release_tag="${DIGCRAWL_VERSION:-}"
install_dir="${DIGCRAWL_INSTALL_DIR:-$HOME/.local/bin}"

usage() {
  cat <<'EOF'
Install DigCrawl from an official GitHub Release.

Usage: install.sh [--version <tag>] [--install-dir <directory>]

Options:
  --version <tag>          Install a specific release tag, for example v1.2.3.
  --install-dir <directory> Install the binary here (default: $HOME/.local/bin).
  -h, --help               Show this help.

Environment variables:
  DIGCRAWL_VERSION         Same as --version.
  DIGCRAWL_INSTALL_DIR     Same as --install-dir.

The installer downloads the release archive and checksums.txt from GitHub,
verifies SHA-256 before installation, and never uses sudo or edits shell files.
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

umask 022
mkdir -p "$install_dir"
install -m 0755 "$temporary_dir/digcrawl" "$install_dir/digcrawl"

printf '%s\n' "Installed DigCrawl $release_tag to $install_dir/digcrawl"
"$install_dir/digcrawl" version

case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *)
    printf '%s\n' "Add $install_dir to PATH to run 'digcrawl' directly. The installer did not modify your shell configuration."
    ;;
esac
