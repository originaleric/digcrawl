#!/usr/bin/env bash
# Manage DigCrawl watchlist serve as a user-level service on macOS or Linux.
#
# Usage:
#   ./scripts/digcrawl-watchlist-service.sh install
#   ./scripts/digcrawl-watchlist-service.sh start|stop|restart|status|logs|uninstall
#
# Optional environment overrides:
#   DIGCRAWL_BIN=/path/to/digcrawl
#   DIGCRAWL_CONFIG=/path/to/config.yaml
#   DIGCRAWL_WORKDIR=/path/to/digcrawl
#   DIGCRAWL_LOG_DIR=/path/to/logs
#   DIGCRAWL_SERVICE_NAME=digcrawl-watchlist
#   DIGCRAWL_SERVICE_LABEL=com.digcrawl.watchlist

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd -P)"

SERVICE_NAME="${DIGCRAWL_SERVICE_NAME:-digcrawl-watchlist}"
SERVICE_LABEL="${DIGCRAWL_SERVICE_LABEL:-com.digcrawl.watchlist}"
WORKDIR="${DIGCRAWL_WORKDIR:-${ROOT_DIR}}"
CONFIG_PATH="${DIGCRAWL_CONFIG:-${WORKDIR}/config/config.yaml}"
LOG_DIR="${DIGCRAWL_LOG_DIR:-${WORKDIR}/logs}"

if [[ -n "${DIGCRAWL_BIN:-}" ]]; then
  BIN_PATH="${DIGCRAWL_BIN}"
elif [[ -x "${WORKDIR}/digcrawl" ]]; then
  BIN_PATH="${WORKDIR}/digcrawl"
else
  BIN_PATH="$(command -v digcrawl || true)"
fi

SELF_PATH="${SCRIPT_DIR}/$(basename "${BASH_SOURCE[0]}")"
OS_NAME="$(uname -s)"

usage() {
  cat <<EOF
Usage: $(basename "$0") <install|start|stop|restart|status|logs|uninstall|run-foreground>

Environment:
  DIGCRAWL_BIN            default: ${BIN_PATH:-<not found>}
  DIGCRAWL_CONFIG         default: ${CONFIG_PATH}
  DIGCRAWL_WORKDIR        default: ${WORKDIR}
  DIGCRAWL_LOG_DIR        default: ${LOG_DIR}
  DIGCRAWL_SERVICE_NAME   default: ${SERVICE_NAME}
  DIGCRAWL_SERVICE_LABEL  default: ${SERVICE_LABEL}
EOF
}

fail() {
  echo "error: $*" >&2
  exit 1
}

require_binary() {
  [[ -n "${BIN_PATH}" ]] || fail "digcrawl binary not found; set DIGCRAWL_BIN=/path/to/digcrawl"
  [[ -x "${BIN_PATH}" ]] || fail "digcrawl binary is not executable: ${BIN_PATH}"
}

require_config() {
  [[ -f "${CONFIG_PATH}" ]] || fail "config file not found: ${CONFIG_PATH}"
}

xml_escape() {
  local value="$1"
  value="${value//&/&amp;}"
  value="${value//</&lt;}"
  value="${value//>/&gt;}"
  value="${value//\"/&quot;}"
  value="${value//\'/&apos;}"
  printf '%s' "${value}"
}

systemd_quote() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//%/%%}"
  printf '"%s"' "${value}"
}

mac_plist_path() {
  printf '%s/%s.plist' "${HOME}/Library/LaunchAgents" "${SERVICE_LABEL}"
}

linux_unit_path() {
  printf '%s/%s.service' "${HOME}/.config/systemd/user" "${SERVICE_NAME}"
}

install_macos() {
  require_binary
  require_config
  mkdir -p "${HOME}/Library/LaunchAgents" "${LOG_DIR}"
  local plist
  plist="$(mac_plist_path)"
  cat >"${plist}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$(xml_escape "${SERVICE_LABEL}")</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(xml_escape "${SELF_PATH}")</string>
    <string>run-foreground</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$(xml_escape "${WORKDIR}")</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DIGCRAWL_BIN</key>
    <string>$(xml_escape "${BIN_PATH}")</string>
    <key>DIGCRAWL_CONFIG</key>
    <string>$(xml_escape "${CONFIG_PATH}")</string>
    <key>DIGCRAWL_WORKDIR</key>
    <string>$(xml_escape "${WORKDIR}")</string>
    <key>DIGCRAWL_LOG_DIR</key>
    <string>$(xml_escape "${LOG_DIR}")</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$(xml_escape "${LOG_DIR}/watchlist.out.log")</string>
  <key>StandardErrorPath</key>
  <string>$(xml_escape "${LOG_DIR}/watchlist.err.log")</string>
</dict>
</plist>
EOF
  echo "installed launchd plist: ${plist}"
}

install_linux() {
  require_binary
  require_config
  mkdir -p "${HOME}/.config/systemd/user" "${LOG_DIR}"
  local unit
  unit="$(linux_unit_path)"
  cat >"${unit}" <<EOF
[Unit]
Description=DigCrawl Watchlist Scheduler
After=network-online.target

[Service]
Type=simple
WorkingDirectory=$(systemd_quote "${WORKDIR}")
Environment=$(systemd_quote "DIGCRAWL_BIN=${BIN_PATH}")
Environment=$(systemd_quote "DIGCRAWL_CONFIG=${CONFIG_PATH}")
Environment=$(systemd_quote "DIGCRAWL_WORKDIR=${WORKDIR}")
Environment=$(systemd_quote "DIGCRAWL_LOG_DIR=${LOG_DIR}")
ExecStart=$(systemd_quote "${SELF_PATH}") run-foreground
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable "${SERVICE_NAME}.service"
  echo "installed systemd user unit: ${unit}"
}

start_macos() {
  local plist
  plist="$(mac_plist_path)"
  [[ -f "${plist}" ]] || fail "plist not found; run install first: ${plist}"
  launchctl bootout "gui/${UID}" "${plist}" >/dev/null 2>&1 || true
  if launchctl bootstrap "gui/${UID}" "${plist}" >/dev/null 2>&1; then
    echo "started ${SERVICE_LABEL}"
  else
    launchctl load "${plist}"
    echo "started ${SERVICE_LABEL}"
  fi
}

stop_macos() {
  local plist
  plist="$(mac_plist_path)"
  launchctl bootout "gui/${UID}" "${plist}" >/dev/null 2>&1 || launchctl unload "${plist}" >/dev/null 2>&1 || true
  echo "stopped ${SERVICE_LABEL}"
}

status_macos() {
  if launchctl print "gui/${UID}/${SERVICE_LABEL}" >/dev/null 2>&1; then
    launchctl print "gui/${UID}/${SERVICE_LABEL}" | sed -n '1,40p'
  else
    launchctl list | grep -F "${SERVICE_LABEL}" || fail "service is not loaded: ${SERVICE_LABEL}"
  fi
}

uninstall_macos() {
  stop_macos
  rm -f "$(mac_plist_path)"
  echo "removed $(mac_plist_path)"
}

start_linux() {
  systemctl --user start "${SERVICE_NAME}.service"
}

stop_linux() {
  systemctl --user stop "${SERVICE_NAME}.service"
}

status_linux() {
  systemctl --user status "${SERVICE_NAME}.service" --no-pager
}

uninstall_linux() {
  systemctl --user disable --now "${SERVICE_NAME}.service" >/dev/null 2>&1 || true
  rm -f "$(linux_unit_path)"
  systemctl --user daemon-reload
  echo "removed $(linux_unit_path)"
}

logs_linux() {
  journalctl --user -u "${SERVICE_NAME}.service" -f
}

logs_file() {
  mkdir -p "${LOG_DIR}"
  touch "${LOG_DIR}/watchlist.out.log" "${LOG_DIR}/watchlist.err.log"
  tail -n 100 -f "${LOG_DIR}/watchlist.out.log" "${LOG_DIR}/watchlist.err.log"
}

run_foreground() {
  require_binary
  require_config
  mkdir -p "${LOG_DIR}"
  cd "${WORKDIR}"
  exec "${BIN_PATH}" watchlist serve --config "${CONFIG_PATH}"
}

dispatch_platform() {
  local action="$1"
  case "${OS_NAME}" in
    Darwin)
      "${action}_macos"
      ;;
    Linux)
      "${action}_linux"
      ;;
    *)
      fail "unsupported OS: ${OS_NAME}; only macOS and Linux are supported"
      ;;
  esac
}

cmd="${1:-}"
case "${cmd}" in
  install)
    dispatch_platform install
    ;;
  start)
    dispatch_platform start
    ;;
  stop)
    dispatch_platform stop
    ;;
  restart)
    dispatch_platform stop
    dispatch_platform start
    ;;
  status)
    dispatch_platform status
    ;;
  logs)
    if [[ "${OS_NAME}" == "Linux" ]] && [[ -f "$(linux_unit_path)" ]]; then
      logs_linux
    else
      logs_file
    fi
    ;;
  uninstall)
    dispatch_platform uninstall
    ;;
  run-foreground)
    run_foreground
    ;;
  -h|--help|help|"")
    usage
    ;;
  *)
    usage
    fail "unknown command: ${cmd}"
    ;;
esac
