#!/usr/bin/env bash
# Run the local WeChat Channels gateway using the existing service manager.
# Usage: ./scripts/digcrawl-wechat-video-service.sh install|start|stop|restart|status|logs|rebuild|rebuild-restart|uninstall
# Set GO to the Go 1.26.1 executable if the shell uses an older Go version.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
WORKDIR="$(cd "${DIGCRAWL_WORKDIR:-${SCRIPT_DIR}/..}" && pwd -P)"

# Preserve whether the caller explicitly requested a directory. The manager
# uses that distinction to retain the installed cwd on a default reinstall.
if [[ -n "${DIGCRAWL_WORKDIR:-}" ]]; then
  export DIGCRAWL_WORKDIR="${WORKDIR}"
fi
export DIGCRAWL_SERVICE_NAME="${DIGCRAWL_SERVICE_NAME:-digcrawl-wechat}"
export DIGCRAWL_SERVICE_LABEL="${DIGCRAWL_SERVICE_LABEL:-com.digcrawl.wechat}"
export DIGCRAWL_SERVICE_SCRIPT_NAME="$(basename "${BASH_SOURCE[0]}")"
export DIGCRAWL_SERVICE_COMMAND=gateway
export DIGCRAWL_SERVICE_LOG_PREFIX=wechat-video
export DIGCRAWL_GATEWAY_ADDR="${DIGCRAWL_GATEWAY_ADDR:-127.0.0.1:8788}"
export DIGCRAWL_LOG_DIR="${DIGCRAWL_LOG_DIR:-${WORKDIR}/storage/digcrawl/digcrawl-wechat-service/logs}"
export DIGCRAWL_BUILD_GOMODCACHE="${DIGCRAWL_BUILD_GOMODCACHE:-${WORKDIR}/.cache/go-mod-wechat-video}"
export DIGCRAWL_BUILD_TAGS="${DIGCRAWL_BUILD_TAGS-}"
export DIGCRAWL_CLI_NAME="${DIGCRAWL_CLI_NAME:-digcrawl-wechat}"

cd "${WORKDIR}"
exec bash "${SCRIPT_DIR}/digcrawl-watchlist-service.sh" "$@"
