#!/usr/bin/env bash
# Regression checks for the service definitions rendered by the CDP browser
# manager. This test uses fake service managers and a harmless executable; it
# never starts a real browser or writes outside a temporary directory.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
SERVICE_SCRIPT="${SCRIPT_DIR}/digcrawl-cdp-browser-service.sh"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/digcrawl-cdp-service-test.XXXXXX")"

cleanup() {
  local status=$?
  rm -rf "${TEST_ROOT}"
  exit "${status}"
}
trap cleanup EXIT

fail() {
  echo "error: $*" >&2
  exit 1
}

[[ -x "${SERVICE_SCRIPT}" ]] || fail "service script is not executable: ${SERVICE_SCRIPT}"
mkdir -p "${TEST_ROOT}/linux-home" "${TEST_ROOT}/macos-home"
ln -s /usr/bin/true "${TEST_ROOT}/Chrome With Space"
mkdir -p "${TEST_ROOT}/bin"
ln -s /usr/bin/true "${TEST_ROOT}/bin/google-chrome"
CAPTURE_CHROME="${TEST_ROOT}/capture-chrome"
printf '%s\n' '#!/usr/bin/env bash' 'printf "%s\\n" "$@" > "${DIGCRAWL_TEST_ARGS_OUTPUT:?}"' > "${CAPTURE_CHROME}"
chmod 700 "${CAPTURE_CHROME}"
export TEST_ROOT SERVICE_SCRIPT

# Assert the effective Chrome invocation, rather than merely checking that a
# source file happens to contain the flag.
bash -c '
  set -euo pipefail
  curl() { return 22; }
  export -f curl
  HOME="$TEST_ROOT/run-foreground-home" DIGCRAWL_CHROME_PATH="$TEST_ROOT/capture-chrome" DIGCRAWL_TEST_ARGS_OUTPUT="$TEST_ROOT/chrome-args" "$SERVICE_SCRIPT" run-foreground
  grep -Fx -- "--mute-audio" "$TEST_ROOT/chrome-args"
'

bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  test_home="$TEST_ROOT/linux-home"
  export test_home
  systemctl() {
    case "$*" in
      *is-active*)
        test -f "$test_home/registered" || return 4
        test -f "$test_home/ready" && return 0
        return 3
        ;;
      *" stop "*) test -f "$test_home/registered" || return 4; rm -f "$test_home/ready" ;;
      *enable*) touch "$test_home/registered" "$test_home/ready" ;;
      *disable*) rm -f "$test_home/registered" "$test_home/ready" ;;
    esac
  }
  curl() { test -f "$test_home/ready" || return 22; printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:9323/devtools/browser/test\"}"; }
  ps() { test -f "$test_home/ready" || return 0; printf "%s\n" "chrome --remote-debugging-port=9323 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run --mute-audio"; }
  export -f uname systemctl curl ps
  HOME="$test_home" DIGCRAWL_CHROME_PATH="$TEST_ROOT/Chrome With Space" "$SERVICE_SCRIPT" install
  unit="$test_home/.config/systemd/user/digcrawl-cdp-browser.service"
  test -f "$unit"
  grep -F "Environment=\"DIGCRAWL_CHROME_PATH=$TEST_ROOT/Chrome With Space\"" "$unit"
'

# Auto-discovery must update the persisted state as well as the generated unit;
# otherwise the first later status/start call cannot load its own metadata.
bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  test_home="$TEST_ROOT/linux-discovery-home"
  export test_home
  systemctl() {
    case "$*" in
      *is-active*)
        test -f "$test_home/registered" || return 4
        test -f "$test_home/ready" && return 0
        return 3
        ;;
      *" stop "*) test -f "$test_home/registered" || return 4; rm -f "$test_home/ready" ;;
      *enable*) touch "$test_home/registered" "$test_home/ready" ;;
      *disable*) rm -f "$test_home/registered" "$test_home/ready" ;;
    esac
  }
  curl() { test -f "$test_home/ready" || return 22; printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:9323/devtools/browser/test\"}"; }
  ps() { test -f "$test_home/ready" || return 0; printf "%s\\n" "chrome --remote-debugging-port=9323 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run --mute-audio"; }
  export -f uname systemctl curl ps
  HOME="$test_home" PATH="$TEST_ROOT/bin:$PATH" "$SERVICE_SCRIPT" install
  metadata="$test_home/.local/state/digcrawl/cdp-browser-services/digcrawl-cdp-browser.tsv"
  grep -F "chrome_path	$TEST_ROOT/bin/google-chrome" "$metadata"
'

# Service identity and the complete launch configuration must survive a fresh
# shell. This is the failure mode that previously made custom units impossible
# to status or uninstall unless callers remembered transient environment vars.
bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  test_home="$TEST_ROOT/linux-custom-home"
  export test_home
  systemctl() {
    case "$*" in
      *is-active*)
        test -f "$test_home/registered" || return 4
        test -f "$test_home/ready" && return 0
        return 3
        ;;
      *" stop "*) test -f "$test_home/registered" || return 4; rm -f "$test_home/ready" ;;
      *enable*) touch "$test_home/registered" "$test_home/ready" ;;
      *disable*) rm -f "$test_home/registered" "$test_home/ready" ;;
    esac
  }
  curl() { test -f "$test_home/ready" || return 22; printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:19432/devtools/browser/test\"}"; }
  ps() { test -f "$test_home/ready" || return 0; printf "%s\n" "chrome --remote-debugging-port=19432 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run --mute-audio"; }
  export -f uname systemctl curl ps
  HOME="$test_home" DIGCRAWL_CDP_BROWSER_SERVICE_NAME="digcrawl-video" DIGCRAWL_CDP_PORT=19432 DIGCRAWL_CHROME_PATH="$TEST_ROOT/Chrome With Space" "$SERVICE_SCRIPT" install
  HOME="$test_home" "$SERVICE_SCRIPT" status | grep -F "127.0.0.1:19432"
  HOME="$test_home" DIGCRAWL_CDP_PORT=9323 "$SERVICE_SCRIPT" status | grep -F "127.0.0.1:19432"
  HOME="$test_home" "$SERVICE_SCRIPT" uninstall
  test ! -e "$test_home/.config/systemd/user/digcrawl-video.service"
  test ! -e "$test_home/.local/state/digcrawl/cdp-browser-services/active-service"
'

bash -c '
  set -euo pipefail
  uname() { printf "Darwin\\n"; }
  test_home="$TEST_ROOT/macos-custom-home"
  export test_home
  launchctl() {
    case "$*" in
      *bootstrap*) touch "$test_home/registered" ;;
      *kickstart*) touch "$test_home/registered" "$test_home/ready" ;;
      *bootout*) rm -f "$test_home/registered" "$test_home/ready" ;;
      *print*) test -f "$test_home/registered" && return 0; echo "Could not find service" >&2; return 113 ;;
    esac
  }
  curl() { test -f "$test_home/ready" || return 22; printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:19433/devtools/browser/test\"}"; }
  ps() { test -f "$test_home/ready" || return 0; printf "%s\n" "Google Chrome --remote-debugging-port=19433 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run --mute-audio"; }
  export -f uname launchctl curl ps
  HOME="$test_home" DIGCRAWL_CDP_BROWSER_SERVICE_LABEL="com.digcrawl.video" DIGCRAWL_CDP_PORT=19433 DIGCRAWL_CHROME_PATH="$TEST_ROOT/Chrome With Space" "$SERVICE_SCRIPT" install
  HOME="$test_home" "$SERVICE_SCRIPT" status | grep -F "127.0.0.1:19433"
  HOME="$test_home" DIGCRAWL_CDP_PORT=9323 "$SERVICE_SCRIPT" status | grep -F "127.0.0.1:19433"
  HOME="$test_home" "$SERVICE_SCRIPT" uninstall
  test ! -e "$test_home/Library/LaunchAgents/com.digcrawl.video.plist"
  test ! -e "$test_home/Library/Application Support/DigCrawl/cdp-browser-services/active-service"
'

bash -c '
  set -euo pipefail
  uname() { printf "Darwin\\n"; }
  test_home="$TEST_ROOT/macos-home"
  export test_home
  launchctl() {
    case "$*" in
      *bootstrap*) touch "$test_home/registered" ;;
      *kickstart*) touch "$test_home/registered" "$test_home/ready" "$test_home/chrome" ;;
      *bootout*) rm -f "$test_home/registered" "$test_home/ready" ;;
      *print*) test -f "$test_home/registered" && return 0; echo "Could not find service" >&2; return 113 ;;
    esac
  }
  curl() { test -f "$test_home/ready" || return 22; printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:9323/devtools/browser/test\"}"; }
  ps() { test -f "$test_home/chrome" || return 0; printf "%s\n" "Google Chrome --remote-debugging-port=9323 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run --mute-audio"; }
  # launchd removes the CDP listener before Chrome has released its profile.
  # The first wait iteration must observe that main process, then this fake
  # completes its graceful exit.
  sleep() { rm -f "$test_home/chrome"; }
  export -f uname launchctl curl ps sleep
  HOME="$test_home" DIGCRAWL_CHROME_PATH="$TEST_ROOT/Chrome With Space" "$SERVICE_SCRIPT" install
  plist="$test_home/Library/LaunchAgents/com.digcrawl.cdp-browser.plist"
  launcher="$test_home/Library/Application Support/DigCrawl/bin/com.digcrawl.cdp-browser.sh"
  test -f "$plist"
  test -x "$launcher"
  cmp -s "$SERVICE_SCRIPT" "$launcher"
  grep -F "<string>$launcher</string><string>run-foreground</string>" "$plist"
  grep -F "<key>DIGCRAWL_CHROME_PATH</key><string>$TEST_ROOT/Chrome With Space</string>" "$plist"
  if command -v plutil >/dev/null 2>&1; then plutil -lint "$plist"; fi
  printf "stale" > "$launcher"
  HOME="$test_home" "$SERVICE_SCRIPT" status 2>&1 | grep -F "staged launcher differs"
  HOME="$test_home" "$SERVICE_SCRIPT" restart
  cmp -s "$SERVICE_SCRIPT" "$launcher"
  HOME="$test_home" "$SERVICE_SCRIPT" stop
  test ! -e "$test_home/registered"
  test ! -e "$test_home/ready"
  test ! -e "$test_home/chrome"
'

# A launchd query failure is not proof that the KeepAlive job is absent.
if bash -c '
  set -euo pipefail
  uname() { printf "Darwin\\n"; }
  launchctl() { case "$*" in *print*) echo "launchd query failed" >&2; return 1 ;; esac; }
  curl() { return 22; }
  ps() { return 0; }
  export -f uname launchctl curl ps
  HOME="$TEST_ROOT/launchd-query-failure-home" "$SERVICE_SCRIPT" stop
'; then
  fail "a launchd query failure was accepted as a completed stop"
fi

# A systemd manager failure must never be mistaken for a completed stop.
if bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  systemctl() { return 1; }
  curl() { return 22; }
  ps() { return 0; }
  export -f uname systemctl curl ps
  HOME="$TEST_ROOT/systemd-failure-home" "$SERVICE_SCRIPT" stop
'; then
  fail "a systemd manager failure was accepted as a completed stop"
fi

# A service that is active but rejects the stop request must also fail rather
# than relying on an absent CDP endpoint as evidence of a completed stop.
if bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  systemctl() {
    case "$*" in
      *is-active*) return 0 ;;
      *" stop "*) return 1 ;;
    esac
  }
  curl() { return 22; }
  ps() { return 0; }
  export -f uname systemctl curl ps
  HOME="$TEST_ROOT/systemd-stop-failure-home" "$SERVICE_SCRIPT" stop
'; then
  fail "a rejected systemd stop was accepted as a completed stop"
fi

if bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  systemctl() { :; }
  curl() { printf "{}"; }
  export -f uname systemctl curl
  HOME="$TEST_ROOT/linux-home" "$SERVICE_SCRIPT" status
'; then
  fail "a non-CDP HTTP 200 response was accepted as a CDP endpoint"
fi

# A process can resemble the configured browser without actually being its
# main process. Require exact argument boundaries and reject helpers.
if bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  systemctl() { :; }
  curl() { printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:9323/devtools/browser/foreign\"}"; }
  ps() { printf "%s\\n" "$FAKE_CHROME_COMMAND"; }
  export -f uname systemctl curl ps
  test_home="$TEST_ROOT/spoof-home"
  export test_home
  export FAKE_CHROME_COMMAND
  for FAKE_CHROME_COMMAND in \
	"chrome --remote-debugging-port=9323 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run" \
    "chrome --remote-debugging-port=93230 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run" \
    "chrome --remote-debugging-port=9323 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser-other --no-first-run" \
    "chrome --remote-debugging-port=9323 --user-data-dir=$test_home/.local/share/digcrawl/cdp-browser --no-first-run --type=renderer"; do
    export FAKE_CHROME_COMMAND
    if HOME="$test_home" "$SERVICE_SCRIPT" status; then exit 0; fi
  done
  exit 1
'; then
  fail "a prefix or helper Chromium process was accepted as the service browser"
fi

if bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  systemctl() { :; }
  # A valid CDP endpoint on the expected port is insufficient when its Chrome
  # uses another profile. Installation must refuse it rather than borrowing
  # another local authenticated browser session.
  curl() { printf "{\"webSocketDebuggerUrl\":\"ws://127.0.0.1:9323/devtools/browser/foreign\"}"; }
  ps() { printf "%s\\n" "chrome --remote-debugging-port=9323 --user-data-dir=/tmp/not-digcrawl --mute-audio"; }
  export -f uname systemctl curl ps
  HOME="$TEST_ROOT/conflict-home" DIGCRAWL_CHROME_PATH="$TEST_ROOT/Chrome With Space" "$SERVICE_SCRIPT" install
'; then
  fail "a CDP endpoint using another browser profile was accepted"
fi

if bash -c '
  set -euo pipefail
  uname() { printf "Linux\\n"; }
  export -f uname
  HOME="$TEST_ROOT/linux-home" DIGCRAWL_CDP_BROWSER_SERVICE_NAME="../../bad" "$SERVICE_SCRIPT" status
'; then
  fail "an unsafe service identity was accepted"
fi

# Windows process matching and reinstall behaviour are executed by the
# PowerShell companion test on the windows-latest CI runner.
test -f "${SCRIPT_DIR}/test-digcrawl-cdp-browser-service.ps1"

echo "CDP browser service rendering checks passed"
