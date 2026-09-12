#!/usr/bin/env bash
# Manage the dedicated Chrome instance used by DigCrawl through CDP.
# It deliberately finds a system Chrome/Chromium and never depends on a
# Playwright versioned browser cache.
#
# Usage:
#   ./scripts/digcrawl-cdp-browser-service.sh install
#   ./scripts/digcrawl-cdp-browser-service.sh start|stop|restart|status|logs|uninstall
# Windows uses scripts/digcrawl-cdp-browser-service.ps1 instead.

set -euo pipefail
umask 077

OS_NAME="$(uname -s)"

# Remember which identity fields were supplied by this invocation. Management
# commands load an installed service's saved launch configuration; only install
# (or a legacy service without metadata) accepts runtime setting overrides.
SERVICE_NAME_EXPLICIT=0
SERVICE_LABEL_EXPLICIT=0
PORT_EXPLICIT=0
USER_DATA_DIR_EXPLICIT=0
LOG_DIR_EXPLICIT=0
CHROME_PATH_EXPLICIT=0
[[ -n "${DIGCRAWL_CDP_BROWSER_SERVICE_NAME+x}" ]] && SERVICE_NAME_EXPLICIT=1
[[ -n "${DIGCRAWL_CDP_BROWSER_SERVICE_LABEL+x}" ]] && SERVICE_LABEL_EXPLICIT=1
[[ -n "${DIGCRAWL_CDP_PORT+x}" ]] && PORT_EXPLICIT=1
[[ -n "${DIGCRAWL_CDP_USER_DATA_DIR+x}" ]] && USER_DATA_DIR_EXPLICIT=1
[[ -n "${DIGCRAWL_CDP_LOG_DIR+x}" ]] && LOG_DIR_EXPLICIT=1
[[ -n "${DIGCRAWL_CHROME_PATH+x}" ]] && CHROME_PATH_EXPLICIT=1

SERVICE_NAME="${DIGCRAWL_CDP_BROWSER_SERVICE_NAME:-digcrawl-cdp-browser}"
SERVICE_LABEL="${DIGCRAWL_CDP_BROWSER_SERVICE_LABEL:-com.digcrawl.cdp-browser}"
PORT="${DIGCRAWL_CDP_PORT:-9323}"
USER_DATA_DIR="${DIGCRAWL_CDP_USER_DATA_DIR:-${HOME}/.local/share/digcrawl/cdp-browser}"
LOG_DIR="${DIGCRAWL_CDP_LOG_DIR:-${HOME}/.local/state/digcrawl/cdp-browser}"
CHROME_PATH="${DIGCRAWL_CHROME_PATH:-}"
SELF_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/$(basename "${BASH_SOURCE[0]}")"
# Keep invocation-supplied runtime values separate from the mutable launch
# configuration. A reconfiguration must stop the old browser using its saved
# port/profile before these values become the new definition.
EXPLICIT_PORT_VALUE="${PORT}"
EXPLICIT_USER_DATA_DIR_VALUE="${USER_DATA_DIR}"
EXPLICIT_LOG_DIR_VALUE="${LOG_DIR}"
EXPLICIT_CHROME_PATH_VALUE="${CHROME_PATH}"

usage() {
  cat <<EOF
Usage: $(basename "$0") <install|start|stop|restart|status|logs|uninstall|run-foreground>

Environment overrides:
  DIGCRAWL_CDP_PORT                 default: ${PORT}
  DIGCRAWL_CDP_USER_DATA_DIR        default: ${USER_DATA_DIR}
  DIGCRAWL_CDP_LOG_DIR              default: ${LOG_DIR}
  DIGCRAWL_CHROME_PATH              default: auto-discover system Chrome/Chromium
  DIGCRAWL_CDP_BROWSER_SERVICE_NAME default: ${SERVICE_NAME}
  DIGCRAWL_CDP_BROWSER_SERVICE_LABEL default: ${SERVICE_LABEL}
EOF
}

fail() {
  echo "error: $*" >&2
  exit 1
}

validate_config() {
  [[ "${PORT}" =~ ^[1-9][0-9]{0,4}$ ]] || fail "DIGCRAWL_CDP_PORT must be a valid TCP port"
  ((PORT <= 65535)) || fail "DIGCRAWL_CDP_PORT must be a valid TCP port"
}

validate_service_identity() {
  # The identifier becomes part of a user-owned filename as well as a
  # launchd/systemd unit name. Restrict it rather than allowing path traversal
  # or manager-specific quoting surprises through environment variables.
  [[ "${SERVICE_NAME}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "DIGCRAWL_CDP_BROWSER_SERVICE_NAME may contain only letters, numbers, dot, underscore, and hyphen"
  [[ "${SERVICE_LABEL}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "DIGCRAWL_CDP_BROWSER_SERVICE_LABEL may contain only letters, numbers, dot, underscore, and hyphen"
}

metadata_dir() {
  case "${OS_NAME}" in
    Darwin) printf '%s/Library/Application Support/DigCrawl/cdp-browser-services' "${HOME}" ;;
    Linux) printf '%s/digcrawl/cdp-browser-services' "${XDG_STATE_HOME:-${HOME}/.local/state}" ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
}

service_identity() {
  case "${OS_NAME}" in
    Darwin) printf '%s' "${SERVICE_LABEL}" ;;
    Linux) printf '%s' "${SERVICE_NAME}" ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
}

metadata_path() {
  printf '%s/%s.tsv' "$(metadata_dir)" "$(service_identity)"
}

active_service_path() {
  printf '%s/active-service' "$(metadata_dir)"
}

validate_metadata_value() {
  local name="$1" value="$2"
  [[ "${value}" != *$'\n'* && "${value}" != *$'\r'* && "${value}" != *$'\t'* ]] || fail "${name} cannot contain a tab or newline"
}

save_service_metadata() {
  validate_config
  validate_service_identity
  [[ -n "${CHROME_PATH}" ]] || fail "Chrome path is empty after discovery"
  validate_metadata_value "DIGCRAWL_CDP_USER_DATA_DIR" "${USER_DATA_DIR}"
  validate_metadata_value "DIGCRAWL_CDP_LOG_DIR" "${LOG_DIR}"
  validate_metadata_value "DIGCRAWL_CHROME_PATH" "${CHROME_PATH}"
  local dir target temporary
  dir="$(metadata_dir)"
  target="$(metadata_path)"
  ensure_private_dirs "${dir}"
  temporary="${target}.tmp.$$"
  {
    printf 'version\t1\n'
    printf 'service_name\t%s\n' "${SERVICE_NAME}"
    printf 'service_label\t%s\n' "${SERVICE_LABEL}"
    printf 'port\t%s\n' "${PORT}"
    printf 'user_data_dir\t%s\n' "${USER_DATA_DIR}"
    printf 'log_dir\t%s\n' "${LOG_DIR}"
    printf 'chrome_path\t%s\n' "${CHROME_PATH}"
  } >"${temporary}"
  chmod 600 "${temporary}"
  mv -f "${temporary}" "${target}"
}

activate_service() {
  local dir temporary target
  dir="$(metadata_dir)"
  target="$(active_service_path)"
  ensure_private_dirs "${dir}"
  temporary="${target}.tmp.$$"
  printf '%s\n' "$(service_identity)" >"${temporary}"
  chmod 600 "${temporary}"
  mv -f "${temporary}" "${target}"
}

remove_service_metadata() {
  rm -f "$(metadata_path)"
  local active identity
  active="$(active_service_path)"
  if [[ -f "${active}" ]]; then
    IFS= read -r identity <"${active}" || true
    if [[ "${identity}" == "$(service_identity)" ]]; then
      rm -f "${active}"
    fi
  fi
}

load_service_metadata() {
  local path="$1" key value extra
  local saved_version="" saved_name="" saved_label="" saved_port="" saved_user_data_dir="" saved_log_dir="" saved_chrome_path=""
  [[ -f "${path}" ]] || return 1
  while IFS=$'\t' read -r key value extra || [[ -n "${key}" ]]; do
    [[ -z "${extra}" ]] || fail "invalid service metadata at ${path}; reinstall the CDP browser service"
    case "${key}" in
      version) saved_version="${value}" ;;
      service_name) saved_name="${value}" ;;
      service_label) saved_label="${value}" ;;
      port) saved_port="${value}" ;;
      user_data_dir) saved_user_data_dir="${value}" ;;
      log_dir) saved_log_dir="${value}" ;;
      chrome_path) saved_chrome_path="${value}" ;;
      *) fail "invalid service metadata at ${path}; reinstall the CDP browser service" ;;
    esac
  done <"${path}"
  [[ "${saved_version}" == "1" && -n "${saved_name}" && -n "${saved_label}" && -n "${saved_port}" && -n "${saved_user_data_dir}" && -n "${saved_log_dir}" && -n "${saved_chrome_path}" ]] || fail "incomplete service metadata at ${path}; reinstall the CDP browser service"
  SERVICE_NAME="${saved_name}"
  SERVICE_LABEL="${saved_label}"
  PORT="${saved_port}"
  USER_DATA_DIR="${saved_user_data_dir}"
  LOG_DIR="${saved_log_dir}"
  CHROME_PATH="${saved_chrome_path}"
  validate_config
  validate_service_identity
}

apply_explicit_runtime_overrides() {
  if (( PORT_EXPLICIT )); then PORT="${EXPLICIT_PORT_VALUE}"; fi
  if (( USER_DATA_DIR_EXPLICIT )); then USER_DATA_DIR="${EXPLICIT_USER_DATA_DIR_VALUE}"; fi
  if (( LOG_DIR_EXPLICIT )); then LOG_DIR="${EXPLICIT_LOG_DIR_VALUE}"; fi
  if (( CHROME_PATH_EXPLICIT )); then CHROME_PATH="${EXPLICIT_CHROME_PATH_VALUE}"; fi
}

validate_explicit_runtime_overrides() {
  local saved_port="${PORT}" saved_user_data_dir="${USER_DATA_DIR}" saved_log_dir="${LOG_DIR}" saved_chrome_path="${CHROME_PATH}"
  apply_explicit_runtime_overrides
  validate_config
  if (( USER_DATA_DIR_EXPLICIT )); then validate_metadata_value "DIGCRAWL_CDP_USER_DATA_DIR" "${USER_DATA_DIR}"; fi
  if (( LOG_DIR_EXPLICIT )); then validate_metadata_value "DIGCRAWL_CDP_LOG_DIR" "${LOG_DIR}"; fi
  if (( CHROME_PATH_EXPLICIT )); then validate_metadata_value "DIGCRAWL_CHROME_PATH" "${CHROME_PATH}"; fi
  PORT="${saved_port}"
  USER_DATA_DIR="${saved_user_data_dir}"
  LOG_DIR="${saved_log_dir}"
  CHROME_PATH="${saved_chrome_path}"
}

load_managed_service() {
  local load_default_metadata="${1:-0}"
  local target active identity require_metadata=0
  validate_service_identity
  case "${OS_NAME}" in
    Darwin)
      if (( SERVICE_LABEL_EXPLICIT )); then target="$(metadata_path)"; else target=""; fi
      ;;
    Linux)
      if (( SERVICE_NAME_EXPLICIT )); then target="$(metadata_path)"; else target=""; fi
      ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
  if [[ -z "${target}" ]]; then
    active="$(active_service_path)"
    if [[ -f "${active}" ]]; then
      IFS= read -r identity <"${active}" || true
      [[ "${identity}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "invalid active service metadata at ${active}; reinstall the CDP browser service"
      case "${OS_NAME}" in
        Darwin) SERVICE_LABEL="${identity}" ;;
        Linux) SERVICE_NAME="${identity}" ;;
      esac
      target="$(metadata_path)"
      require_metadata=1
    fi
  fi
  # An interrupted legacy migration can leave the default definition's
  # metadata behind without an active-service pointer. It still describes the
  # same default identity, so prefer it to silently resetting its port/profile
  # during a later bare install.
  if [[ -z "${target}" && "${load_default_metadata}" == "1" ]]; then
    target="$(metadata_path)"
  fi
  # Services created before metadata support remain operable with their
  # original default/environment configuration. Reinstall once to adopt the
  # persistent source of truth.
  if [[ -n "${target}" && -f "${target}" ]]; then
    load_service_metadata "${target}"
  elif (( require_metadata )); then
    fail "active service metadata is missing at ${target}; reinstall the CDP browser service"
  fi
}

discover_chrome() {
  local candidates=()
  if [[ -n "${CHROME_PATH}" ]]; then
    candidates+=("${CHROME_PATH}")
  fi
  case "${OS_NAME}" in
    Darwin)
      candidates+=(
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        "${HOME}/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        "/Applications/Chromium.app/Contents/MacOS/Chromium"
      )
      ;;
  esac
  local candidate
  # Bash 3 with `set -u` treats an empty declared array as unset when it is
  # expanded. Linux commonly starts with no path candidates, so guard that
  # expansion before falling back to command discovery.
  if ((${#candidates[@]})); then
    for candidate in "${candidates[@]}"; do
      if [[ -x "${candidate}" ]]; then
        printf '%s\n' "${candidate}"
        return 0
      fi
    done
  fi
  for candidate in google-chrome google-chrome-stable chromium chromium-browser; do
    if candidate="$(command -v "${candidate}" 2>/dev/null)" && [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  return 1
}

cdp_url() {
  printf 'http://127.0.0.1:%s/json/version' "${PORT}"
}

cdp_ready() {
  command -v curl >/dev/null 2>&1 || fail "curl is required to check the CDP endpoint"
  local version
  if ! version="$(curl --fail --silent --max-time 2 "$(cdp_url)")"; then
    return 1
  fi
  # A bare HTTP 200 is not enough: another local service could occupy the
  # configured port. CDP's version endpoint always exposes this field.
  grep -Eq '"webSocketDebuggerUrl"[[:space:]]*:' <<<"${version}"
}

browser_process_matches_config() {
  # CDP itself does not reveal Chromium's --user-data-dir. Pair the endpoint
  # probe with the exact main-process argument sequence emitted below. A
  # substring check would accept port 93230 for port 9323, a profile suffix,
  # or one of Chromium's renderer/helper processes.
  command -v ps >/dev/null 2>&1 || fail "ps is required to verify the CDP browser process"
  local command_line required_args
  required_args="--remote-debugging-port=${PORT} --user-data-dir=${USER_DATA_DIR} --no-first-run"
  while IFS= read -r command_line; do
    [[ "${command_line}" == *"${required_args}"* ]] || continue
    [[ "${command_line}" != *"--type="* ]] || continue
    return 0
  done < <(ps -ax -o command=)
  return 1
}

browser_process_mutes_audio() {
  # The service owns this browser, so its main process must retain the launch
  # invariant rather than merely expose a reachable CDP endpoint. Keep this
  # separate from profile matching to report an outdated staged launcher
  # precisely on macOS.
  local command_line required_args
  required_args="--remote-debugging-port=${PORT} --user-data-dir=${USER_DATA_DIR} --no-first-run"
  while IFS= read -r command_line; do
    [[ "${command_line}" == *"${required_args}"* ]] || continue
    [[ "${command_line}" != *"--type="* ]] || continue
    [[ "${command_line}" =~ (^|[[:space:]])--mute-audio([[:space:]]|$) ]] && return 0
  done < <(ps -ax -o command=)
  return 1
}

cdp_service_ready() {
	cdp_ready && browser_process_matches_config && browser_process_mutes_audio
}

wait_for_cdp() {
  local attempt
  for attempt in $(seq 1 15); do
    if cdp_service_ready; then
      return 0
    fi
    if cdp_ready; then
	  if browser_process_matches_config; then
	    echo "error: CDP browser is missing --mute-audio; run $0 restart to update the managed service" >&2
	    return 2
	  fi
      echo "error: CDP port ${PORT} is occupied by a browser that does not use this service profile" >&2
      return 2
    fi
    sleep 1
  done
  return 1
}

wait_for_cdp_to_stop() {
  local attempt
  for attempt in $(seq 1 10); do
    # Closing the CDP listener happens before Chrome has necessarily released
    # its persistent profile. A start/restart must not race that main process.
    if ! cdp_ready && ! browser_process_matches_config; then
      return 0
    fi
    sleep 1
  done
  return 1
}

prepare_install() {
  # Reinstall is allowed, but it must first stop the service being replaced.
  # Never kill an arbitrary Chrome merely because it occupies the chosen port.
  stop_service || fail "CDP port ${PORT} is already in use; stop the conflicting browser or choose a different DIGCRAWL_CDP_PORT"
}

mac_plist_path() {
  printf '%s/Library/LaunchAgents/%s.plist' "${HOME}" "${SERVICE_LABEL}"
}

mac_launcher_dir() {
  printf '%s/Library/Application Support/DigCrawl/bin' "${HOME}"
}

mac_launcher_path() {
  printf '%s/%s.sh' "$(mac_launcher_dir)" "${SERVICE_LABEL}"
}

stage_macos_launcher() {
  # launchd can be denied execution of a script located in protected folders
  # such as Documents, even though the interactive shell can run it. Keep an
  # owned, private copy in Application Support and update it atomically on
  # every install. The staged script derives SELF_PATH from its own location,
  # so start/restart continue to use the same runnable copy.
  local launcher_dir launcher_path temporary_path
  launcher_dir="$(mac_launcher_dir)"
  launcher_path="$(mac_launcher_path)"
  ensure_private_dirs "${launcher_dir}"
  temporary_path="${launcher_path}.tmp.$$"
  cp "${SELF_PATH}" "${temporary_path}"
  chmod 700 "${temporary_path}"
  mv -f "${temporary_path}" "${launcher_path}"
  printf '%s\n' "${launcher_path}"
}

linux_unit_path() {
  printf '%s/.config/systemd/user/%s.service' "${HOME}" "${SERVICE_NAME}"
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

ensure_private_dirs() {
  local dir
  for dir in "$@"; do
    mkdir -p "${dir}"
    chmod 700 "${dir}"
  done
}

run_foreground() {
  validate_config
  if cdp_ready; then
    if browser_process_matches_config; then
	  browser_process_mutes_audio || fail "managed CDP browser is missing --mute-audio; run $0 restart"
      # A service manager may briefly start a replacement while the previous
      # compatible Chrome is still exiting. Do not launch a second process
      # against the same profile; wait until the compatible one is gone.
      echo "CDP browser with this service profile is already available at $(cdp_url); monitoring it"
      while cdp_service_ready; do sleep 5; done
    else
      fail "CDP port ${PORT} is occupied by a browser that does not use this service profile"
    fi
  fi
  local chrome
  chrome="$(discover_chrome)" || fail "no supported system Chrome/Chromium found; install Chrome or set DIGCRAWL_CHROME_PATH"
  ensure_private_dirs "${USER_DATA_DIR}" "${LOG_DIR}"
  exec "${chrome}" \
    --headless=new \
    --remote-debugging-address=127.0.0.1 \
    --remote-debugging-port="${PORT}" \
    --user-data-dir="${USER_DATA_DIR}" \
    --no-first-run \
    --no-default-browser-check \
    --disable-gpu \
    --mute-audio \
    about:blank
}

install_macos() {
  local chrome launcher
  chrome="$(discover_chrome)" || fail "no supported system Chrome/Chromium found; install Chrome or set DIGCRAWL_CHROME_PATH"
  # The service definition and its metadata must record the same resolved
  # executable. Discovery used to populate only the plist, leaving a metadata
  # record that could not be loaded by later management commands.
  CHROME_PATH="${chrome}"
  mkdir -p "${HOME}/Library/LaunchAgents"
  ensure_private_dirs "${USER_DATA_DIR}" "${LOG_DIR}"
  launcher="$(stage_macos_launcher)"
  local plist
  plist="$(mac_plist_path)"
  cat >"${plist}" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$(xml_escape "${SERVICE_LABEL}")</string>
  <key>ProgramArguments</key><array>
    <string>$(xml_escape "${launcher}")</string><string>run-foreground</string>
  </array>
  <key>EnvironmentVariables</key><dict>
    <key>DIGCRAWL_CDP_PORT</key><string>$(xml_escape "${PORT}")</string>
    <key>DIGCRAWL_CDP_USER_DATA_DIR</key><string>$(xml_escape "${USER_DATA_DIR}")</string>
    <key>DIGCRAWL_CDP_LOG_DIR</key><string>$(xml_escape "${LOG_DIR}")</string>
    <key>DIGCRAWL_CHROME_PATH</key><string>$(xml_escape "${chrome}")</string>
  </dict>
  <key>StandardOutPath</key><string>$(xml_escape "${LOG_DIR}/stdout.log")</string>
  <key>StandardErrorPath</key><string>$(xml_escape "${LOG_DIR}/stderr.log")</string>
</dict></plist>
EOF
  local domain="gui/$(id -u)"
  launchctl bootstrap "${domain}" "${plist}"
  # A CDP browser must be an explicit user choice. In particular, do not set
  # RunAtLoad or KeepAlive here: a headless Chrome that starts at login can be
  # treated by macOS as the user's running Chrome application.
  echo "installed CDP browser launchd service: ${plist}; run $0 start to launch it"
}

start_macos() {
  local plist domain
  plist="$(mac_plist_path)"
  [[ -f "${plist}" ]] || fail "service is not installed; run $0 install"
  domain="gui/$(id -u)"
  launchctl bootstrap "${domain}" "${plist}" >/dev/null 2>&1 || true
  launchctl kickstart -k "${domain}/${SERVICE_LABEL}"
}

stop_macos() {
  local domain target plist
  domain="gui/$(id -u)"
  target="${domain}/${SERVICE_LABEL}"
  plist="$(mac_plist_path)"

  # Older installations used KeepAlive, so stop the manager before its Chrome
  # child. Do not hide a bootout failure: otherwise an upgraded installation
  # can report success while launchd continues to own and restart the browser.
  if ! launchctl_service_is_loaded "${target}"; then
    return 0
  fi
  if launchctl bootout "${target}" >/dev/null 2>&1; then
    :
  elif [[ -f "${plist}" ]] && launchctl bootout "${domain}" "${plist}" >/dev/null 2>&1; then
    :
  else
    fail "failed to unregister ${SERVICE_LABEL} from launchd; Chrome may be restarted automatically"
  fi
  if wait_for_launchctl_service_to_unload "${target}"; then
    return 0
  fi
  # `bootout gui/<uid>/<label>` can acknowledge before launchd has removed an
  # older job definition. Retry through the plist form only after that bounded
  # wait, then require the manager to actually forget the job.
  if [[ -f "${plist}" ]] && launchctl bootout "${domain}" "${plist}" >/dev/null 2>&1 && wait_for_launchctl_service_to_unload "${target}"; then
    return 0
  fi
  fail "${SERVICE_LABEL} is still registered with launchd after stop"
}

launchctl_service_is_loaded() {
  local target="$1" output query_exit
  if output="$(launchctl print "${target}" 2>&1)"; then
    return 0
  else
    query_exit=$?
  fi
  # launchctl uses EX_UNAVAILABLE (113) for a missing service. Do not turn a
  # permission or launchd communication error into a successful stop: KeepAlive
  # could still restart Chrome later.
  if [[ "${query_exit}" -eq 113 && "${output}" == *"Could not find service"* ]]; then
    return 1
  fi
  fail "unable to inspect ${SERVICE_LABEL} in launchd (launchctl exit ${query_exit}): ${output}"
}

wait_for_launchctl_service_to_unload() {
  local target="$1" attempt
  # launchctl can report a just-booted-out job for a brief propagation window.
  # Waiting here makes `stop` a reliable lifecycle boundary without treating a
  # manager communication failure as a successful unregister.
  for attempt in $(seq 1 10); do
    if ! launchctl_service_is_loaded "${target}"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

stop_service() {
  case "${OS_NAME}" in
    Darwin) stop_macos ;;
    Linux) stop_linux ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
  # Do not claim that Chrome is stopped until its dedicated CDP listener has
  # disappeared. This avoids returning before an old manager's restart race
  # has settled, and makes `stop` safe for callers to rely on programmatically.
  wait_for_cdp_to_stop || fail "CDP endpoint did not stop within 10 seconds"
}

status_macos() {
  if cdp_service_ready; then
	if ! cmp -s "${SELF_PATH}" "$(mac_launcher_path)"; then
	  echo "WARN CDP browser is running, but its staged launcher differs from the current source; run $0 restart to apply updates" >&2
	fi
    echo "OK   CDP endpoint: $(cdp_url)"
	return 0
  elif cdp_ready; then
	if browser_process_matches_config; then
	  echo "FAIL CDP browser is missing --mute-audio; run $0 restart to update the staged launcher" >&2
	  return 1
	fi
    echo "FAIL CDP endpoint is occupied by a browser outside this service profile: $(cdp_url)"
    return 1
  else
    echo "FAIL CDP endpoint unavailable: $(cdp_url)"
    return 1
  fi
}

status_service() {
  case "${OS_NAME}" in
    Darwin) status_macos ;;
    Linux) status_linux ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
}

uninstall_macos() {
  stop_service
  rm -f "$(mac_plist_path)"
  rm -f "$(mac_launcher_path)"
  remove_service_metadata
}

install_linux() {
  command -v systemctl >/dev/null 2>&1 || fail "systemctl is required on Linux"
  local chrome
  chrome="$(discover_chrome)" || fail "no supported system Chrome/Chromium found; install Chrome or set DIGCRAWL_CHROME_PATH"
  CHROME_PATH="${chrome}"
  mkdir -p "${HOME}/.config/systemd/user"
  ensure_private_dirs "${USER_DATA_DIR}" "${LOG_DIR}"
  local unit
  unit="$(linux_unit_path)"
  # Remove the enablement left by the old persistent unit before replacing it.
  # A stale Wants= symlink would otherwise preserve login startup even though
  # the new unit intentionally has no [Install] section.
  if [[ -f "${unit}" ]] && grep -Fq 'WantedBy=' "${unit}"; then
    systemctl --user disable "${SERVICE_NAME}" || fail "failed to disable the previous ${SERVICE_NAME} auto-start unit"
  fi
  cat >"${unit}" <<EOF
[Unit]
Description=DigCrawl dedicated Chrome CDP browser

[Service]
Type=simple
ExecStart=$(systemd_quote "${SELF_PATH}") run-foreground
Environment=$(systemd_quote "DIGCRAWL_CDP_PORT=${PORT}")
Environment=$(systemd_quote "DIGCRAWL_CDP_USER_DATA_DIR=${USER_DATA_DIR}")
Environment=$(systemd_quote "DIGCRAWL_CDP_LOG_DIR=${LOG_DIR}")
Environment=$(systemd_quote "DIGCRAWL_CHROME_PATH=${chrome}")
StandardOutput=journal
StandardError=journal
EOF
  systemctl --user daemon-reload
  echo "installed CDP browser systemd user service: ${unit}; run $0 start to launch it"
}

start_linux() { systemctl --user start "${SERVICE_NAME}"; }
systemd_unit_is_inactive_or_missing() {
  local status
  if systemctl --user is-active --quiet "${SERVICE_NAME}"; then
    return 1
  else
    status=$?
  fi
  # systemctl documents 3 for an inactive unit and 4 when it cannot find the
  # unit. Any other status is a manager/permission failure, not evidence that
  # it is safe to claim the browser service stopped.
  case "${status}" in
    3|4) return 0 ;;
    *) fail "unable to determine whether ${SERVICE_NAME} is active (systemctl exit ${status})" ;;
  esac
}

stop_linux() {
  if systemd_unit_is_inactive_or_missing; then
    return 0
  fi
  systemctl --user stop "${SERVICE_NAME}" || fail "failed to stop ${SERVICE_NAME}"
  systemd_unit_is_inactive_or_missing || fail "${SERVICE_NAME} remains active after stop"
}
status_linux() {
  systemctl --user --no-pager status "${SERVICE_NAME}"
  if cdp_service_ready; then
    echo "OK   CDP endpoint: $(cdp_url)"
	return 0
  elif cdp_ready; then
	if browser_process_matches_config; then
	  echo "FAIL CDP browser is missing --mute-audio; run $0 restart" >&2
	  return 1
	fi
    echo "FAIL CDP endpoint is occupied by a browser outside this service profile: $(cdp_url)"
    return 1
  else
    echo "FAIL CDP endpoint unavailable: $(cdp_url)"
    return 1
  fi
}

restart_service() {
  # macOS executes a private staged copy because launchd may reject project
  # directories. Refresh that copy during an explicit restart while retaining
  # the already-saved service identity, Chrome path and browser profile.
  case "${OS_NAME}" in
    Darwin)
      stop_service
      stage_macos_launcher >/dev/null
      start_macos
      ;;
    Linux) systemctl --user restart "${SERVICE_NAME}" ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
  wait_for_cdp || fail "CDP endpoint did not become ready within 15 seconds"
  status_service
}
uninstall_linux() {
  stop_service
  local unit
  unit="$(linux_unit_path)"
  if [[ -f "${unit}" ]]; then
    if grep -Fq 'WantedBy=' "${unit}"; then
      systemctl --user disable "${SERVICE_NAME}" || fail "failed to disable ${SERVICE_NAME}"
    fi
    rm -f "${unit}"
  fi
  systemctl --user daemon-reload || fail "failed to reload the user systemd manager"
  remove_service_metadata
}

logs() {
  case "${OS_NAME}" in
    Linux) journalctl --user --unit "${SERVICE_NAME}" --lines 100 --follow ;;
    Darwin)
      ensure_private_dirs "${LOG_DIR}"
      touch "${LOG_DIR}/stdout.log" "${LOG_DIR}/stderr.log"
      tail -n 100 -f "${LOG_DIR}/stdout.log" "${LOG_DIR}/stderr.log"
      ;;
    *) fail "unsupported OS: ${OS_NAME}" ;;
  esac
}

main() {
  local command="${1:-}"
  # `run-foreground` is executed by the service manager with the validated
  # launch environment already embedded in its definition. Every operator
  # command instead resolves the active installed service before acting.
  if [[ "${command}" == "install" ]]; then
    # A bare reinstall updates the current active definition, including a
    # custom identity, so its old auto-start wrapper is the one that gets
    # disabled. Keep the saved runtime configuration until that old browser
    # has fully exited; explicit runtime values are applied immediately before
    # writing the replacement definition below.
    load_managed_service 1
    validate_explicit_runtime_overrides
  elif [[ "${command}" != "run-foreground" ]]; then
    load_managed_service
  fi
  case "${command}" in
    install)
      validate_config
      validate_service_identity
      prepare_install
      apply_explicit_runtime_overrides
      validate_config
      validate_service_identity
      case "${OS_NAME}" in Darwin) install_macos ;; Linux) install_linux ;; *) fail "unsupported OS: ${OS_NAME}" ;; esac
      # Installation only records an explicit service configuration. Browser
      # startup is a separate, user-requested action so a login or a Chrome
      # crash can never take over the ordinary Chrome application.
      save_service_metadata
      activate_service
      ;;
    start)
      case "${OS_NAME}" in Darwin) start_macos ;; Linux) start_linux ;; *) fail "unsupported OS: ${OS_NAME}" ;; esac
      wait_for_cdp || fail "CDP endpoint did not become ready within 15 seconds"
      status_service
      ;;
    stop) stop_service ;;
    restart) restart_service ;;
    status) status_service ;;
    logs) logs ;;
    uninstall) case "${OS_NAME}" in Darwin) uninstall_macos ;; Linux) uninstall_linux ;; *) fail "unsupported OS: ${OS_NAME}" ;; esac ;;
    run-foreground) run_foreground ;;
    *) usage; exit 1 ;;
  esac
}

main "$@"
