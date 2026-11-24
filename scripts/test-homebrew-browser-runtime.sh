#!/usr/bin/env bash
# Exercise the actual Cask preflight with a staged native CLI and private data.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# A real Bridge uses a Unix socket beneath HOME; keep this private fixture
# short enough for macOS's socket pathname limit even after /tmp canonicalizes.
TEST_ROOT="$(mktemp -d /tmp/dc-cask.XXXXXX)"
cleanup() {
  local status=$?
  if [[ -x "$TEST_ROOT/stage with spaces/digcrawl" && -f "$TEST_ROOT/config/digcrawl/digcrawl-skill.yaml" ]]; then
    if ! "$TEST_ROOT/stage with spaces/digcrawl" bridge stop --config "$TEST_ROOT/config/digcrawl/digcrawl-skill.yaml" >"$TEST_ROOT/cleanup.log" 2>&1; then
      echo "FAIL: Cask test owner cleanup incomplete; retained $TEST_ROOT" >&2
      exit 1
    fi
  fi
  rm -rf "$TEST_ROOT"
  exit "$status"
}
trap cleanup EXIT
mkdir -p "$TEST_ROOT/stage with spaces" "$TEST_ROOT/home" "$TEST_ROOT/config/digcrawl"
export HOME="$TEST_ROOT/home" XDG_CONFIG_HOME="$TEST_ROOT/config" XDG_DATA_HOME="$TEST_ROOT/data"
export DIGCRAWL_BROWSER_DIR="$TEST_ROOT/browsers" DIGCRAWL_CASK_STAGE="$TEST_ROOT/stage with spaces"
: "${DIGCRAWL_BROWSER_ARCHIVE_DIR:?set offline pinned archive directory for this acceptance test}"
go build -o "$DIGCRAWL_CASK_STAGE/digcrawl" "$ROOT/cmd/digcrawl"
profile="$XDG_CONFIG_HOME/digcrawl/digcrawl-skill.yaml"
printf '%s\n' 'LocalBrowser:' '  ChromePath: /legacy/desktop' '  CookieStoreDir: /preserve/private/cookies' > "$profile"
export DIGCRAWL_CASK_ROOT="$ROOT"
ruby <<'RUBY'
require 'yaml'
configuration = YAML.load_file(File.join(ENV.fetch('DIGCRAWL_CASK_ROOT'), '.goreleaser.yaml'))
hooks = configuration.fetch('homebrew_casks').first.fetch('hooks')
context = Object.new
context.define_singleton_method(:staged_path) { ENV.fetch('DIGCRAWL_CASK_STAGE') }
context.define_singleton_method(:system_command) do |executable, args:, must_succeed:|
  success = system(executable, *args)
  raise 'Cask preflight failed' if must_succeed && !success
end
context.instance_eval(hooks.fetch('pre').fetch('install'), 'generated-cask-preflight', 1)
# A reconnect in the replacement window must be retired by the post hook.
cli = File.join(ENV.fetch('DIGCRAWL_CASK_STAGE'), 'digcrawl')
profile = File.join(ENV.fetch('XDG_CONFIG_HOME'), 'digcrawl', 'digcrawl-skill.yaml')
raise 'fixture Bridge start failed' unless system(cli, 'bridge', 'restart', '--config', profile)
context.instance_eval(hooks.fetch('post').fetch('install'), 'generated-cask-postflight', 1)
owners = IO.popen([cli, 'browser', 'migrate', '--config', profile], &:read)
raise 'Cask retained a replacement-window owner' if owners.include?('Owner PID=')
RUBY
test -f "$profile.before-isolated-browser"
grep -F '/preserve/private/cookies' "$profile" >/dev/null
if grep -F '/legacy/desktop' "$profile" >/dev/null; then echo 'legacy override survived Cask migration' >&2; exit 1; fi
"$DIGCRAWL_CASK_STAGE/digcrawl" browser runtime >/dev/null
"$DIGCRAWL_CASK_STAGE/digcrawl" browser runtime --interactive >/dev/null
printf '%s\n' 'PASS: Homebrew preflight prepares isolated browsers and migrates the standard profile'
