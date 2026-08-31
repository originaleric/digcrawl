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
  plutil -replace ProgramArguments.0 -string "${TEST_ROOT}/Documents/digcrawl-watchlist-service.sh" "${plist}"
  task_state="running"
  start_macos
  assert_contains "$(<"${plist}")" "<string>${launcher}</string>" 'macOS start must migrate a legacy plist to the staged launcher'
  printf 'stale\n' >"${launcher}"
  restart_macos
  cmp -s "${SERVICE_SCRIPT}" "${launcher}" || { printf 'assertion failed: macOS restart must refresh the staged launcher\n' >&2; exit 1; }
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

test_macos_install_starts_and_reports_running
test_linux_install_restarts_and_reports_running
printf 'Watchlist service install lifecycle regression checks passed\n'
