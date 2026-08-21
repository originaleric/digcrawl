#!/usr/bin/env bash
# Manage DigCrawl watchlist serve as a user-level service on macOS or Linux.
#
# Usage:
#   ./scripts/digcrawl-watchlist-service.sh install
#   ./scripts/digcrawl-watchlist-service.sh build
#   ./scripts/digcrawl-watchlist-service.sh rebuild
#   ./scripts/digcrawl-watchlist-service.sh rebuild-restart
#   ./scripts/digcrawl-watchlist-service.sh configure-ffmpeg
#   ./scripts/digcrawl-watchlist-service.sh start|stop|restart|status|logs|uninstall
#
# Optional environment overrides:
#   DIGCRAWL_BIN=/path/to/digcrawl
#   DIGCRAWL_CONFIG=/path/to/config.yaml
#   DIGCRAWL_WORKDIR=/path/to/digcrawl
#   DIGCRAWL_LOG_DIR=/path/to/logs
#   DIGCRAWL_SERVICE_NAME=digcrawl-watchlist
#   DIGCRAWL_SERVICE_LABEL=com.digcrawl.watchlist
#   DIGCRAWL_AUTO_BUILD=1
#   DIGCRAWL_BUILD_TAGS=digflow_embedded
#   DIGCRAWL_BUILD_GOCACHE=/private/tmp/digcrawl-gocache
#   DIGCRAWL_BUILD_GOMODCACHE=<workdir>/.tmp/go-mod-watchlist
#   DIGCRAWL_SERVICE_PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
#   DIGCRAWL_FFMPEG_PATH=/path/to/ffmpeg

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd -P)"

SERVICE_NAME="${DIGCRAWL_SERVICE_NAME:-digcrawl-watchlist}"
SERVICE_LABEL="${DIGCRAWL_SERVICE_LABEL:-com.digcrawl.watchlist}"
WORKDIR="${DIGCRAWL_WORKDIR:-${ROOT_DIR}}"
CONFIG_PATH="${DIGCRAWL_CONFIG:-${WORKDIR}/config/config.yaml}"
LOG_DIR="${DIGCRAWL_LOG_DIR:-${WORKDIR}/logs}"
DIGCRAWL_AUTO_BUILD="${DIGCRAWL_AUTO_BUILD:-1}"
DIGCRAWL_BUILD_TAGS="${DIGCRAWL_BUILD_TAGS:-digflow_embedded}"
DIGCRAWL_BUILD_GOCACHE="${DIGCRAWL_BUILD_GOCACHE:-/private/tmp/digcrawl-gocache}"
DIGCRAWL_BUILD_GOMODCACHE="${DIGCRAWL_BUILD_GOMODCACHE:-${WORKDIR}/.tmp/go-mod-watchlist}"
DIGCRAWL_SERVICE_PATH="${DIGCRAWL_SERVICE_PATH:-/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin}"
DIGCRAWL_FFMPEG_PATH="${DIGCRAWL_FFMPEG_PATH:-}"

if [[ -n "${DIGCRAWL_BIN:-}" ]]; then
  BIN_PATH="${DIGCRAWL_BIN}"
elif [[ "${DIGCRAWL_AUTO_BUILD}" == "1" ]]; then
  BIN_PATH="${WORKDIR}/bin/digcrawl-watchlist"
elif [[ -x "${WORKDIR}/digcrawl" ]]; then
  BIN_PATH="${WORKDIR}/digcrawl"
else
  BIN_PATH="$(command -v digcrawl || true)"
fi

SELF_PATH="${SCRIPT_DIR}/$(basename "${BASH_SOURCE[0]}")"
OS_NAME="$(uname -s)"

usage() {
  cat <<EOF
Usage: $(basename "$0") <install|build|rebuild|rebuild-restart|configure-ffmpeg|start|stop|restart|status|logs|uninstall|run-foreground>

Environment:
  DIGCRAWL_BIN            default: ${BIN_PATH:-<not found>}
  DIGCRAWL_CONFIG         default: ${CONFIG_PATH}
  DIGCRAWL_WORKDIR        default: ${WORKDIR}
  DIGCRAWL_LOG_DIR        default: ${LOG_DIR}
  DIGCRAWL_SERVICE_NAME   default: ${SERVICE_NAME}
  DIGCRAWL_SERVICE_LABEL  default: ${SERVICE_LABEL}
  DIGCRAWL_AUTO_BUILD     default: ${DIGCRAWL_AUTO_BUILD}
  DIGCRAWL_BUILD_TAGS     default: ${DIGCRAWL_BUILD_TAGS}
  DIGCRAWL_BUILD_GOCACHE  default: ${DIGCRAWL_BUILD_GOCACHE}
  DIGCRAWL_BUILD_GOMODCACHE default: ${DIGCRAWL_BUILD_GOMODCACHE}
  DIGCRAWL_SERVICE_PATH   default: ${DIGCRAWL_SERVICE_PATH}
  DIGCRAWL_FFMPEG_PATH    default: ${DIGCRAWL_FFMPEG_PATH:-<auto-detect>}
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

detect_ffmpeg_path() {
  local candidates=()
  if [[ -n "${DIGCRAWL_FFMPEG_PATH:-}" ]]; then
    candidates+=("${DIGCRAWL_FFMPEG_PATH}")
  fi
  local path_ffmpeg
  path_ffmpeg="$(PATH="${DIGCRAWL_SERVICE_PATH}:${PATH:-}" command -v ffmpeg 2>/dev/null || true)"
  if [[ -n "${path_ffmpeg}" ]]; then
    candidates+=("${path_ffmpeg}")
  fi
  candidates+=("/opt/homebrew/bin/ffmpeg" "/usr/local/bin/ffmpeg" "/usr/bin/ffmpeg")
  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -n "${candidate}" && -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  return 1
}

ensure_ffmpeg_env() {
  local ffmpeg_path
  ffmpeg_path="$(detect_ffmpeg_path)" || fail "ffmpeg not found; install ffmpeg or set DIGCRAWL_FFMPEG_PATH=/path/to/ffmpeg"
  DIGCRAWL_FFMPEG_PATH="${ffmpeg_path}"
  export DIGCRAWL_FFMPEG_PATH
  local ffmpeg_dir
  ffmpeg_dir="$(dirname "${ffmpeg_path}")"
  case ":${DIGCRAWL_SERVICE_PATH}:" in
    *":${ffmpeg_dir}:"*) ;;
    *) DIGCRAWL_SERVICE_PATH="${ffmpeg_dir}:${DIGCRAWL_SERVICE_PATH}" ;;
  esac
  export DIGCRAWL_SERVICE_PATH
  export PATH="${DIGCRAWL_SERVICE_PATH}:${PATH:-}"
}

configure_ffmpeg() {
  require_config
  ensure_ffmpeg_env
  local tmp
  tmp="$(mktemp "${CONFIG_PATH}.tmp.XXXXXX")"
  local config_mode
  case "${OS_NAME}" in
    Darwin)
      config_mode="$(stat -f '%Lp' "${CONFIG_PATH}" 2>/dev/null || true)"
      ;;
    Linux)
      config_mode="$(stat -c '%a' "${CONFIG_PATH}" 2>/dev/null || true)"
      ;;
    *)
      config_mode=""
      ;;
  esac
  chmod "${config_mode:-600}" "${tmp}" 2>/dev/null || chmod 600 "${tmp}"
  awk '
    BEGIN { in_asr = 0; done = 0 }
    /^ASR:[[:space:]]*$/ { in_asr = 1 }
    /^[^[:space:]#][^:]*:/ && $0 !~ /^ASR:/ { if (in_asr) in_asr = 0 }
    in_asr && /^[[:space:]]*FFmpegPath:/ {
      print "  FFmpegPath: \"${DIGCRAWL_FFMPEG_PATH}\""
      done = 1
      next
    }
    { print }
    END { if (!done) exit 42 }
  ' "${CONFIG_PATH}" >"${tmp}" || {
    rm -f "${tmp}"
    fail "failed to update ASR.FFmpegPath in ${CONFIG_PATH}"
  }
  mv "${tmp}" "${CONFIG_PATH}"
  echo "configured ASR.FFmpegPath=\${DIGCRAWL_FFMPEG_PATH}"
  echo "detected DIGCRAWL_FFMPEG_PATH=${DIGCRAWL_FFMPEG_PATH}"
}

build_binary() {
  if [[ "${DIGCRAWL_AUTO_BUILD}" != "1" ]]; then
    require_binary
    return
  fi
  mkdir -p "$(dirname "${BIN_PATH}")"
  mkdir -p "${DIGCRAWL_BUILD_GOCACHE}" "${DIGCRAWL_BUILD_GOMODCACHE}"
  local go_bin
  local build_log
  go_bin="${GO:-go}"
  local build_args=()
  if [[ -n "${DIGCRAWL_BUILD_TAGS}" ]]; then
    build_args+=("-tags" "${DIGCRAWL_BUILD_TAGS}")
  fi
  build_log="$(mktemp "${DIGCRAWL_BUILD_GOCACHE%/}/digcrawl-build.XXXXXX")" || fail "failed to create Go build log"
  if ! GOCACHE="${DIGCRAWL_BUILD_GOCACHE}" GOMODCACHE="${DIGCRAWL_BUILD_GOMODCACHE}" "${go_bin}" build "${build_args[@]}" -o "${BIN_PATH}" ./cmd/digcrawl >"${build_log}" 2>&1; then
    cat "${build_log}" >&2
    rm -f "${build_log}"
    fail "failed to build digcrawl binary: ${BIN_PATH}"
  fi

  # Apple's compiler wrappers can emit this diagnostic when their optional
  # xcrun lookup cache cannot be persisted. A successful `go build` has
  # produced a valid binary, so retain every other diagnostic but omit this
  # misleading non-fatal line from normal installation output.
  awk '!/^(cc|clang|ar): error: couldn\x27t create cache file .+\/xcrun_db-[^\x27]+\x27 \(errno=Operation not permitted\)$/' "${build_log}" >&2
  rm -f "${build_log}"
  [[ -x "${BIN_PATH}" ]] || fail "failed to build digcrawl binary: ${BIN_PATH}"
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

installed_bin_path() {
  case "${OS_NAME}" in
    Darwin)
      local plist
      plist="$(mac_plist_path)"
      if [[ -f "${plist}" ]] && command -v plutil >/dev/null 2>&1; then
        plutil -extract EnvironmentVariables.DIGCRAWL_BIN raw -o - "${plist}" 2>/dev/null || true
      fi
      ;;
    Linux)
      local unit
      unit="$(linux_unit_path)"
      if [[ -f "${unit}" ]]; then
        sed -n 's/^Environment="DIGCRAWL_BIN=\([^"]*\)"$/\1/p' "${unit}" | head -n 1
      fi
      ;;
  esac
}

use_installed_bin_path_if_available() {
  if [[ -n "${DIGCRAWL_BIN:-}" ]]; then
    return
  fi
  local installed
  installed="$(installed_bin_path)"
  if [[ -n "${installed}" ]]; then
    BIN_PATH="${installed}"
  fi
}

install_macos() {
  require_config
  ensure_ffmpeg_env
  build_binary
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
    <key>DIGCRAWL_AUTO_BUILD</key>
    <string>$(xml_escape "${DIGCRAWL_AUTO_BUILD}")</string>
    <key>DIGCRAWL_BUILD_TAGS</key>
    <string>$(xml_escape "${DIGCRAWL_BUILD_TAGS}")</string>
    <key>DIGCRAWL_BUILD_GOCACHE</key>
    <string>$(xml_escape "${DIGCRAWL_BUILD_GOCACHE}")</string>
    <key>DIGCRAWL_BUILD_GOMODCACHE</key>
    <string>$(xml_escape "${DIGCRAWL_BUILD_GOMODCACHE}")</string>
    <key>DIGCRAWL_SERVICE_PATH</key>
    <string>$(xml_escape "${DIGCRAWL_SERVICE_PATH}")</string>
    <key>DIGCRAWL_FFMPEG_PATH</key>
    <string>$(xml_escape "${DIGCRAWL_FFMPEG_PATH}")</string>
    <key>PATH</key>
    <string>$(xml_escape "${DIGCRAWL_SERVICE_PATH}")</string>
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
  require_config
  ensure_ffmpeg_env
  build_binary
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
Environment=$(systemd_quote "DIGCRAWL_AUTO_BUILD=${DIGCRAWL_AUTO_BUILD}")
Environment=$(systemd_quote "DIGCRAWL_BUILD_TAGS=${DIGCRAWL_BUILD_TAGS}")
Environment=$(systemd_quote "DIGCRAWL_BUILD_GOCACHE=${DIGCRAWL_BUILD_GOCACHE}")
Environment=$(systemd_quote "DIGCRAWL_BUILD_GOMODCACHE=${DIGCRAWL_BUILD_GOMODCACHE}")
Environment=$(systemd_quote "DIGCRAWL_SERVICE_PATH=${DIGCRAWL_SERVICE_PATH}")
Environment=$(systemd_quote "DIGCRAWL_FFMPEG_PATH=${DIGCRAWL_FFMPEG_PATH}")
Environment=$(systemd_quote "PATH=${DIGCRAWL_SERVICE_PATH}")
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
  elif launchctl load "${plist}" >/dev/null 2>&1; then
    echo "started ${SERVICE_LABEL}"
  else
    fail "failed to start ${SERVICE_LABEL}; try: launchctl bootstrap gui/$(id -u) ${plist}"
  fi
}

stop_macos() {
  local plist
  plist="$(mac_plist_path)"
  if ! launchctl print "gui/${UID}/${SERVICE_LABEL}" >/dev/null 2>&1; then
    echo "stopped ${SERVICE_LABEL}"
    return
  fi
  if ! launchctl bootout "gui/${UID}" "${plist}" >/dev/null 2>&1 && ! launchctl unload "${plist}" >/dev/null 2>&1; then
    fail "failed to stop ${SERVICE_LABEL}; try: launchctl bootout gui/$(id -u) ${plist}"
  fi
  echo "stopped ${SERVICE_LABEL}"
}

mac_service_pid() {
  launchctl print "gui/${UID}/${SERVICE_LABEL}" 2>/dev/null | awk -F'= ' '/pid =/ {print $2; exit}'
}

restart_macos() {
  local plist
  plist="$(mac_plist_path)"
  [[ -f "${plist}" ]] || fail "plist not found; run install first: ${plist}"
  local old_pid=""
  local new_pid=""
  if launchctl print "gui/${UID}/${SERVICE_LABEL}" >/dev/null 2>&1; then
    old_pid="$(mac_service_pid)"
    if launchctl kickstart -k "gui/${UID}/${SERVICE_LABEL}" >/dev/null 2>&1; then
      sleep 1
      new_pid="$(mac_service_pid)"
      if [[ -n "${old_pid}" && "${new_pid}" == "${old_pid}" ]]; then
        fail "restart did not replace ${SERVICE_LABEL}; pid is still ${old_pid}"
      fi
      echo "restarted ${SERVICE_LABEL}"
      return
    fi
    echo "kickstart failed; falling back to bootout/bootstrap" >&2
  fi
  stop_macos
  start_macos
  sleep 1
  new_pid="$(mac_service_pid)"
  if [[ -n "${old_pid}" && "${new_pid}" == "${old_pid}" ]]; then
    fail "restart did not replace ${SERVICE_LABEL}; pid is still ${old_pid}. Try running this command in a local terminal: launchctl kickstart -k gui/$(id -u)/${SERVICE_LABEL}"
  fi
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

restart_linux() {
  systemctl --user restart "${SERVICE_NAME}.service"
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
  require_config
  require_binary
  ensure_ffmpeg_env
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
  build|rebuild)
    require_config
    use_installed_bin_path_if_available
    build_binary
    ;;
  configure-ffmpeg)
    configure_ffmpeg
    ;;
  rebuild-restart|deploy)
    require_config
    use_installed_bin_path_if_available
    build_binary
    dispatch_platform restart
    dispatch_platform status
    ;;
  start)
    dispatch_platform start
    ;;
  stop)
    dispatch_platform stop
    ;;
  restart)
    dispatch_platform restart
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
