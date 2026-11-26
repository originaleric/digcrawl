#!/usr/bin/env bash
# Regression coverage for the Watchlist service install lifecycle. It sources
# the production script in test mode and replaces launchd/systemd commands.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
SERVICE_SCRIPT="${SCRIPT_DIR}/digcrawl-watchlist-service.sh"

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  [[ "${haystack}" == *"${needle}"* ]] || {
    printf 'assertion failed: %s\n' "${message}" >&2
    exit 1
  }
}

setup_runtime() {
  TEST_ROOT="$(mktemp -d)"
  mkdir -p "${TEST_ROOT}/work/config" "${TEST_ROOT}/home"
  : >"${TEST_ROOT}/work/config/config.yaml"
  : >"${TEST_ROOT}/digcrawl"
  chmod +x "${TEST_ROOT}/digcrawl"
  export HOME="${TEST_ROOT}/home"
  export DIGCRAWL_WORKDIR="${TEST_ROOT}/work"
  export DIGCRAWL_CONFIG="${TEST_ROOT}/work/config/config.yaml"
  export DIGCRAWL_BIN="${TEST_ROOT}/digcrawl"
  export DIGCRAWL_AUTO_BUILD=0
  export DIGCRAWL_FFMPEG_PATH=/bin/true
  export DIGCRAWL_WATCHLIST_SERVICE_TEST=1
}

setup_plutil() {
  # macOS owns the real plist parser. The Linux CI runner simulates launchd,
  # so it needs only the plutil operations exercised by this test.
  if [[ "${DIGCRAWL_TEST_FORCE_MOCK_PLUTIL:-}" == "1" ]] || ! command -v plutil >/dev/null 2>&1; then
    plutil() {
      case "$1" in
        -lint)
          [[ -f "$2" ]]
          ;;
        -extract)
          [[ "$3" == "raw" && "$4" == "-o" && "$5" == "-" ]] || return 1
          python3 - "$2" "$6" <<'PY_EXTRACT'
import plistlib, sys
from pathlib import Path
value = plistlib.loads(Path(sys.argv[2]).read_bytes())
for key in sys.argv[1].split("."):
    value = value[key]
print(value)
PY_EXTRACT
          ;;
        -replace)
          [[ "$2" == "ProgramArguments" && "$3" == "-xml" && -f "$5" ]] || return 1
          python3 - "$4" "$5" <<'PY_PLIST'
import plistlib, sys
from pathlib import Path
path = Path(sys.argv[2])
value = plistlib.loads(path.read_bytes())
value["ProgramArguments"] = plistlib.loads(("<plist>" + sys.argv[1] + "</plist>").encode())
path.write_bytes(plistlib.dumps(value))
PY_PLIST
          ;;
        *)
          return 64
          ;;
      esac
    }
  fi
}

test_macos_install_starts_and_reports_running() {
  setup_runtime
  local lifecycle="${TEST_ROOT}/launchctl.log"
  local task_state="running"
  local task_pid="4242"
  uname() { printf 'Darwin\n'; }
  launchctl() {
    printf '%s\n' "$*" >>"${lifecycle}"
    case "$1" in
      kickstart)
        task_pid="$((task_pid + 1))"
        ;;
      print)
        if [[ "${task_state}" == "running" ]]; then
          printf '    state = running\n    pid = %s\n' "${task_pid}"
        else
          printf '    state = exited\n'
        fi
      ;;
    esac
  }
  setup_plutil
  source "${SERVICE_SCRIPT}"
  install_macos
  local calls
  calls="$(<"${lifecycle}")"
  assert_contains "${calls}" 'bootstrap gui/' 'macOS install must bootstrap launchd'
  local launcher="${HOME}/Library/Application Support/DigCrawl/bin/digcrawl-watchlist-service.sh"
  [[ -x "${launcher}" ]] || { printf 'assertion failed: macOS install must stage a private launcher\n' >&2; exit 1; }
  cmp -s "${SERVICE_SCRIPT}" "${launcher}" || { printf 'assertion failed: staged launcher must match current source\n' >&2; exit 1; }
  local plist="${HOME}/Library/LaunchAgents/com.digcrawl.watchlist.plist"
  assert_contains "$(<"${plist}")" "<string>${launcher}</string>" 'macOS plist must execute the staged launcher'
  command -v plutil >/dev/null 2>&1 || { printf 'assertion failed: macOS tests require plutil\n' >&2; exit 1; }
  plutil -lint "${plist}" >/dev/null
  local status
  status="$(status_macos)"
  assert_contains "${status}" $'status=\033[32mrunning\033[0m' 'macOS running status must be green'
  task_state="exited"
  if status="$(status_macos 2>&1)"; then
    printf 'assertion failed: macOS stopped status must fail\n' >&2
    exit 1
  fi
  assert_contains "${status}" $'status=\033[31mexited\033[0m' 'macOS non-running status must be red'
  # Include duplicated launchers left by older indexed migrations.
  plutil -replace ProgramArguments -xml "<array><string>legacy-launcher</string><string>legacy-launcher</string><string>run-foreground</string></array>" "${plist}"
  task_state="running"
  start_macos
  assert_contains "$(<"${plist}")" "<string>${launcher}</string>" 'macOS start must migrate a legacy plist to the staged launcher'
  start_macos
  python3 - "${plist}" "${launcher}" <<'PY_ARGV'
import plistlib, sys
from pathlib import Path
value = plistlib.loads(Path(sys.argv[1]).read_bytes())
assert value["ProgramArguments"] == [sys.argv[2], "run-foreground"], value["ProgramArguments"]
PY_ARGV
  printf 'stale\n' >"${launcher}"
  restart_macos
  cmp -s "${SERVICE_SCRIPT}" "${launcher}" || { printf 'assertion failed: macOS restart must refresh the staged launcher\n' >&2; exit 1; }
  python3 - "${plist}" "${launcher}" <<'PY_ARGV'
import plistlib, sys
from pathlib import Path
assert plistlib.loads(Path(sys.argv[1]).read_bytes())["ProgramArguments"] == [sys.argv[2], "run-foreground"]
PY_ARGV
  uninstall_macos
  [[ ! -e "${launcher}" ]] || { printf 'assertion failed: macOS uninstall must remove the staged launcher\n' >&2; exit 1; }
  rm -rf "${TEST_ROOT}"
}

test_linux_install_restarts_and_reports_running() {
  setup_runtime
  local lifecycle="${TEST_ROOT}/systemctl.log"
  local task_state="active"
  uname() { printf 'Linux\n'; }
  systemctl() {
    printf '%s\n' "$*" >>"${lifecycle}"
    if [[ "$*" == *'is-active --quiet'* ]]; then
      [[ "${task_state}" == "active" ]]
      return
    fi
    if [[ "$*" == *'is-active '* ]]; then
      printf '%s\n' "${task_state}"
    fi
  }
  source "${SERVICE_SCRIPT}"
  install_linux
  local calls
  calls="$(<"${lifecycle}")"
  assert_contains "${calls}" 'enable digcrawl-watchlist.service' 'Linux install must enable the service'
  assert_contains "${calls}" 'restart digcrawl-watchlist.service' 'Linux install must replace an already-running service instance'
  assert_contains "${calls}" 'is-active --quiet digcrawl-watchlist.service' 'Linux install must verify the running service'
  local status
  status="$(status_linux)"
  assert_contains "${status}" $'status=\033[32mrunning\033[0m' 'Linux running status must be green'
  task_state="failed"
  if status="$(status_linux 2>&1)"; then
    printf 'assertion failed: Linux failed status must fail\n' >&2
    exit 1
  fi
  assert_contains "${status}" $'status=\033[31mfailed\033[0m' 'Linux non-running status must be red'
  rm -rf "${TEST_ROOT}"
}

test_upgrade_prepares_before_stopping() (
  local test_os="$1"
  local test_command="$2"
  setup_runtime
  trap 'rm -rf "${TEST_ROOT}"' EXIT
  local lifecycle="${TEST_ROOT}/lifecycle.log"
  : >"${lifecycle}"
  printf 'old\n' >"${DIGCRAWL_BIN}"
  mkdir -p "${HOME}/Library/LaunchAgents" "${HOME}/.config/systemd/user"
  printf 'old service\n' >"${HOME}/Library/LaunchAgents/com.digcrawl.watchlist.plist"
  printf 'old service\n' >"${HOME}/.config/systemd/user/digcrawl-watchlist.service"
  local service_definition="${HOME}/.config/systemd/user/digcrawl-watchlist.service"
  if [[ "${test_os}" == "Darwin" ]]; then
    service_definition="${HOME}/Library/LaunchAgents/com.digcrawl.watchlist.plist"
  fi
  export DIGCRAWL_AUTO_BUILD=1
  export DIGCRAWL_BUILD_GOCACHE="${TEST_ROOT}/gocache"
  export DIGCRAWL_BUILD_GOMODCACHE="${TEST_ROOT}/gomodcache"
  export GO=mock_go
  local build_failure=1
  local replace_failure=0
  local definition_replace_failure=0
  local manager_failure=""
  local manager_failed=0
  uname() { printf '%s\n' "${test_os}"; }
  mock_go() {
    printf 'build:%s\n' "$(<"${DIGCRAWL_BIN}")" >>"${lifecycle}"
    local target=""
    while (( $# )); do
      if [[ "$1" == "-o" ]]; then target="$2"; break; fi
      shift
    done
    [[ "${target}" != "${DIGCRAWL_BIN}" && -n "${target}" ]] || return 1
    printf 'new\n' >"${target}"
    chmod +x "${target}"
    [[ "${build_failure}" == "0" ]]
  }
  launchctl() {
    case "$1" in
      print) printf '    state = running\n    pid = 4242\n' ;;
      bootout|unload) printf 'stop:%s\n' "$(<"${DIGCRAWL_BIN}")" >>"${lifecycle}" ;;
      bootstrap|load) printf 'start:%s\n' "$(<"${DIGCRAWL_BIN}")" >>"${lifecycle}" ;;
    esac
  }
  systemctl() {
    if [[ "$2" == "${manager_failure}" && "${manager_failed}" == "0" ]]; then
      manager_failed=1
      printf 'failure:%s\n' "$2" >>"${lifecycle}"
      return 1
    fi
    case "$2" in
      stop) printf 'stop:%s\n' "$(<"${DIGCRAWL_BIN}")" >>"${lifecycle}" ;;
      start|restart) printf 'start:%s\n' "$(<"${DIGCRAWL_BIN}")" >>"${lifecycle}" ;;
      is-active) printf 'active\n' ;;
    esac
  }
  plutil() { return 0; }
  mv() {
    local destination
    for destination in "$@"; do :; done
    if [[ "${destination}" == "${DIGCRAWL_BIN}" && "${replace_failure}" == "1" ]] ||
       [[ "${destination}" == "${service_definition}" && "${definition_replace_failure}" == "1" ]]; then
      return 1
    fi
    command mv "$@"
  }
  # Run the CLI in its own Bash process. Sourcing it inside an if-condition
  # suppresses errexit throughout its functions on Bash 5, unlike real usage.
  # Export only the mocks and fixture values needed by that process.
  export -f uname mock_go launchctl systemctl plutil mv
  export test_os lifecycle build_failure replace_failure definition_replace_failure
  export manager_failure manager_failed service_definition
  export DIGCRAWL_WATCHLIST_SERVICE_TEST=0
  if [[ "${test_command}" == "install" ]]; then
    export DIGCRAWL_LOG_DIR="${TEST_ROOT}/blocked-log"
    : >"${DIGCRAWL_LOG_DIR}"
    if "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1; then
      printf 'assertion failed: install must reject unavailable log directory\n' >&2; exit 1;
    fi
    [[ ! -s "${lifecycle}" && "$(<"${DIGCRAWL_BIN}")" == 'old' ]] || exit 1
    unset DIGCRAWL_LOG_DIR
  fi
  if "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1; then
    printf 'assertion failed: %s %s must reject failed build\n' "${test_os}" "${test_command}" >&2
    exit 1
  fi
  [[ "$(<"${lifecycle}")" == 'build:old' && "$(<"${DIGCRAWL_BIN}")" == 'old' ]] || {
    printf 'assertion failed: failed build stopped service or replaced old binary\n' >&2; exit 1;
  }
  [[ "$(<"${service_definition}")" == 'old service' ]] || exit 1

  # A CLI path collision is another preparation failure. It must also leave
  # the running service and its binary alone after compilation succeeds.
  build_failure=0
  mkdir -p "${HOME}/.local/bin"
  : >"${HOME}/.local/bin/digcrawl"
  : >"${lifecycle}"
  if "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1; then
    printf 'assertion failed: %s %s must reject CLI collision\n' "${test_os}" "${test_command}" >&2
    exit 1
  fi
  [[ "$(<"${lifecycle}")" == 'build:old' && "$(<"${DIGCRAWL_BIN}")" == 'old' ]] || {
    printf 'assertion failed: CLI failure stopped service or replaced old binary\n' >&2; exit 1;
  }
  rm "${HOME}/.local/bin/digcrawl"
  replace_failure=1
  : >"${lifecycle}"
  if "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1; then
    printf 'assertion failed: upgrade must reject failed binary replacement\n' >&2; exit 1;
  fi
  assert_contains "$(<"${lifecycle}")" $'build:old\nstop:old' 'replacement failure must follow successful preparation'
  assert_contains "$(<"${lifecycle}")" 'start:old' 'replacement failure must restart the old binary'
  [[ "$(<"${DIGCRAWL_BIN}")" == 'old' && "$(<"${service_definition}")" == 'old service' ]] || exit 1
  replace_failure=0
  if [[ "${test_command}" == "install" ]]; then
    definition_replace_failure=1
    : >"${lifecycle}"
    if "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1; then
      printf 'assertion failed: install must reject failed service definition replacement\n' >&2; exit 1;
    fi
    assert_contains "$(<"${lifecycle}")" 'start:new' 'definition replacement failure must restore service using its existing definition'
    [[ "$(<"${service_definition}")" == 'old service' ]] || exit 1
    definition_replace_failure=0
    printf 'old\n' >"${DIGCRAWL_BIN}"
  fi
  if [[ "${test_os}" == "Linux" ]]; then
    local failed_action
    local failed_actions=(start is-active)
    if [[ "${test_command}" == "install" ]]; then
      failed_actions=(daemon-reload enable restart is-active)
    fi
    for failed_action in "${failed_actions[@]}"; do
      manager_failure="${failed_action}"
      manager_failed=0
      : >"${lifecycle}"
      if "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1; then
        printf 'assertion failed: failed %s must remain an installation error\n' "${failed_action}" >&2; exit 1;
      fi
      assert_contains "$(<"${lifecycle}")" "failure:${failed_action}" 'test must reach the service manager failure'
      assert_contains "$(<"${lifecycle}")" $'\nstart:new' 'post-stop failure must attempt to restore service'
      printf 'old\n' >"${DIGCRAWL_BIN}"
    done
    manager_failure=""
  fi
  : >"${lifecycle}"
  "${BASH}" "${SERVICE_SCRIPT}" "${test_command}" >"${TEST_ROOT}/output" 2>&1 || {
    cat "${TEST_ROOT}/output" >&2; exit 1;
  }
  local calls
  calls="$(<"${lifecycle}")"
  assert_contains "${calls}" $'build:old\nstop:old' 'upgrade must stop the old process only after build succeeds'
  assert_contains "${calls}" 'start:new' 'upgrade must start the newly replaced binary'
  [[ "$(<"${DIGCRAWL_BIN}")" == 'new' ]] || exit 1
  local leftover
  for leftover in "${DIGCRAWL_BIN}".tmp.* "${DIGCRAWL_BUILD_GOCACHE}"/digcrawl-build.* "${service_definition}".tmp.*; do
    [[ ! -e "${leftover}" ]] || { printf 'assertion failed: staged build files were left behind\n' >&2; exit 1; }
  done
)

assert_macos_workdir() {
  python3 - "$1" "$2" <<'PY_WORKDIR'
import plistlib, sys
from pathlib import Path
value = plistlib.loads(Path(sys.argv[1]).read_bytes())
assert value["WorkingDirectory"] == sys.argv[2], value
assert value["EnvironmentVariables"]["DIGCRAWL_WORKDIR"] == sys.argv[2], value
PY_WORKDIR
}

test_macos_reinstall_preserves_workdir() (
  local entry="$1"
  setup_runtime
  trap 'rm -rf "${TEST_ROOT}"' EXIT
  setup_plutil
  local lifecycle="${TEST_ROOT}/lifecycle.log"
  export lifecycle
  uname() { printf 'Darwin\n'; }
  launchctl() {
    printf '%s\n' "$*" >>"${lifecycle}"
    if [[ "$1" == "print" ]]; then
      printf '    state = running\n    pid = 4242\n'
    fi
  }
  export -f uname launchctl
  if declare -F plutil >/dev/null; then export -f plutil; fi
  export DIGCRAWL_WATCHLIST_SERVICE_TEST=0
  export DIGCRAWL_LOG_DIR="${TEST_ROOT}/logs"
  local script="${SERVICE_SCRIPT}"
  local label=com.digcrawl.watchlist
  if [[ "${entry}" == "wrapper" ]]; then
    script="${SCRIPT_DIR}/digcrawl-wechat-video-service.sh"
    label=com.digcrawl.wechat
  fi
  local plist="${HOME}/Library/LaunchAgents/${label}.plist"
  local checkout
  checkout="$(cd "${SCRIPT_DIR}/.." && pwd -P)"
  # No previous installation: retain the existing source-directory default.
  unset DIGCRAWL_WORKDIR
  "${BASH}" "${script}" install >/dev/null
  assert_macos_workdir "${plist}" "${checkout}"
  # An explicit directory moves both persisted cwd fields together.
  local stable="${TEST_ROOT}/installed runtime & data"
  mkdir -p "${stable}"
  stable="$(cd "${stable}" && pwd -P)"
  DIGCRAWL_WORKDIR="${stable}" "${BASH}" "${script}" install >/dev/null
  assert_macos_workdir "${plist}" "${stable}"
  # A default reinstall builds from the checkout, even while preserving cwd.
  mock_go() {
    pwd >"${DIGCRAWL_LOG_DIR}/build-cwd.txt"
    while (( $# )); do
      if [[ "$1" == "-o" ]]; then
        cat >"$2" <<'MOCK_BINARY'
#!/usr/bin/env bash
pwd >"${DIGCRAWL_LOG_DIR}/runtime-cwd.txt"
printf '%s\n' "$@" >"${DIGCRAWL_LOG_DIR}/args.txt"
MOCK_BINARY
        chmod +x "$2"
        return 0
      fi
      shift
    done
    return 1
  }
  export -f mock_go
  export GO=mock_go DIGCRAWL_AUTO_BUILD=1
  export DIGCRAWL_BUILD_GOCACHE="${TEST_ROOT}/gocache"
  export DIGCRAWL_BUILD_GOMODCACHE="${TEST_ROOT}/gomodcache"
  local attempt
  for attempt in 1 2; do
    if [[ "${entry}" == "wrapper" ]]; then
      (cd /private/tmp && "${BASH}" "${script}" install >/dev/null)
    else
      (cd "${checkout}" && "${BASH}" "${script}" install >/dev/null)
    fi
    assert_macos_workdir "${plist}" "${stable}"
    [[ "$(<"${DIGCRAWL_LOG_DIR}/build-cwd.txt")" == "${checkout}" ]] || exit 1
  done
  # Execute the actual staged launcher with the persisted environment.
  python3 - "${plist}" "${stable}" <<'PY_LAUNCH'
import os, plistlib, subprocess, sys
from pathlib import Path
value = plistlib.loads(Path(sys.argv[1]).read_bytes())
assert len(value["ProgramArguments"]) == 2, value["ProgramArguments"]
env = dict(os.environ, **value["EnvironmentVariables"])
subprocess.run(value["ProgramArguments"], cwd=value["WorkingDirectory"], env=env, check=True)
assert Path(env["DIGCRAWL_LOG_DIR"], "runtime-cwd.txt").read_text().strip() == sys.argv[2]
PY_LAUNCH
  # A missing or unreadable installed cwd must fail before touching the service.
  rmdir "${stable}"
  local before_calls before_plist
  before_calls="$(<"${lifecycle}")"
  before_plist="$(<"${plist}")"
  if "${BASH}" "${script}" install >"${TEST_ROOT}/error" 2>&1; then
    printf 'assertion failed: reinstall accepted a missing runtime directory\n' >&2; exit 1
  fi
  assert_contains "$(<"${TEST_ROOT}/error")" 'working directory is unavailable' 'missing cwd must have an actionable error'
  [[ "$(<"${lifecycle}")" == "${before_calls}" && "$(<"${plist}")" == "${before_plist}" ]] || exit 1
  printf 'invalid plist\n' >"${plist}"
  if "${BASH}" "${script}" install >"${TEST_ROOT}/error" 2>&1; then
    printf 'assertion failed: reinstall accepted an unreadable installed cwd\n' >&2; exit 1
  fi
  assert_contains "$(<"${TEST_ROOT}/error")" 'cannot read installed WorkingDirectory' 'invalid plist must fail explicitly'
  [[ "$(<"${lifecycle}")" == "${before_calls}" && "$(<"${plist}")" == 'invalid plist' ]] || exit 1
)

test_gateway_uses_shared_service_lifecycle() (
  local test_os="$1"
  setup_runtime
  export DIGCRAWL_SERVICE_NAME=digcrawl-wechat-video
  export DIGCRAWL_SERVICE_LABEL=com.digcrawl.wechat-video
  export DIGCRAWL_SERVICE_COMMAND=gateway
  export DIGCRAWL_CLI_NAME=digcrawl-wechat
  export DIGCRAWL_SERVICE_LOG_PREFIX=wechat-video
  export DIGCRAWL_GATEWAY_ADDR=127.0.0.1:8788
  uname() { printf '%s\n' "${test_os}"; }
  launchctl() {
    if [[ "$1" == "print" ]]; then
      printf '    state = running\n    pid = 4242\n'
    fi
  }
  systemctl() { return 0; }
  source "${SERVICE_SCRIPT}"
  # A video gateway can start before optional transcription tools are installed.
  detect_ffmpeg_path() { return 1; }
  export DIGCRAWL_FFMPEG_PATH=""
  local definition
  if [[ "${test_os}" == "Darwin" ]]; then
    install_macos
    definition="$(mac_plist_path)"
    assert_contains "$(<"${definition}")" '<string>gateway</string>' 'launchd must preserve the gateway command'
    assert_contains "$(<"${definition}")" '<string>127.0.0.1:8788</string>' 'launchd must preserve the gateway address'
    assert_contains "$(<"${definition}")" 'digcrawl-wechat-video-service.sh' 'gateway must have its own staged launcher'
    assert_contains "$(<"${definition}")" 'wechat-video.err.log' 'gateway must have its own log files'
  else
    install_linux
    definition="$(linux_unit_path)"
    assert_contains "$(<"${definition}")" 'DIGCRAWL_SERVICE_COMMAND=gateway' 'systemd must preserve the gateway command'
    assert_contains "$(<"${definition}")" 'DIGCRAWL_GATEWAY_ADDR=127.0.0.1:8788' 'systemd must preserve the gateway address'
  fi
  # Exercise the public wrapper, including execution from outside the checkout.
  cat >"${DIGCRAWL_BIN}" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$@" >"${DIGCRAWL_WORKDIR}/args.txt"
pwd >"${DIGCRAWL_WORKDIR}/cwd.txt"
MOCK
  chmod +x "${DIGCRAWL_BIN}"
  export DIGCRAWL_WATCHLIST_SERVICE_TEST=0
  local wrapper="${SCRIPT_DIR}/digcrawl-wechat-video-service.sh"
  (cd /private/tmp && "${BASH}" "${wrapper}" run-foreground)
  local expected
  expected="$(printf '%s\n' gateway --config "${DIGCRAWL_CONFIG}" --addr 127.0.0.1:8788)"
  [[ "$(<"${DIGCRAWL_WORKDIR}/args.txt")" == "${expected}" ]] || {
    printf 'assertion failed: gateway launch arguments differ\n' >&2; exit 1;
  }
  [[ "$(<"${DIGCRAWL_WORKDIR}/cwd.txt")" == "$(cd "${DIGCRAWL_WORKDIR}" && pwd -P)" ]] || {
    printf 'assertion failed: gateway working directory differs\n' >&2; exit 1;
  }
)

(test_macos_reinstall_preserves_workdir manager)
(test_macos_reinstall_preserves_workdir wrapper)
(test_gateway_uses_shared_service_lifecycle Darwin)
(test_gateway_uses_shared_service_lifecycle Linux)
(test_macos_install_starts_and_reports_running)
(test_linux_install_restarts_and_reports_running)
for test_os in Darwin Linux; do
  for test_command in install deploy rebuild-restart; do
    test_upgrade_prepares_before_stopping "${test_os}" "${test_command}"
  done
done
printf 'Watchlist service install lifecycle regression checks passed\n'
