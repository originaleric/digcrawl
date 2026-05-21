#!/usr/bin/env bash
# 手动冒烟：平台 read 工具（需本地 diggate 运行 + 可选 cookie）
# 用法:
#   GATEWAY_URL=http://127.0.0.1:8787 ./scripts/smoke_platform_read.sh
# 环境变量 URL_* 可覆盖默认测试链接

set -euo pipefail
GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:8787}"

call_tool() {
  local tool="$1"
  local url="$2"
  echo "=== $tool ==="
  echo "url: $url"
  curl -sS -X POST "${GATEWAY_URL}/tools/call" \
    -H 'Content-Type: application/json' \
    -d "{\"type\":\"tool_call\",\"id\":\"smoke-${tool//./-}\",\"tool\":\"${tool}\",\"input\":{\"url\":\"${url}\",\"include_media\":true}}" \
    | jq '{platform, content_type, canonical_url, title, text: (.text[:80]), author, platform_metadata}'
  echo
}

# 小红书
call_tool "xiaohongshu.note.read" "${URL_XHS:-https://www.xiaohongshu.com/explore/REPLACE_ME}"
call_tool "xiaohongshu.note.read" "${URL_XHS_SHORT:-https://xhslink.com/REPLACE_ME}"

# 抖音
call_tool "douyin.video.read" "${URL_DOUYIN:-https://www.douyin.com/video/REPLACE_ME}"
call_tool "douyin.video.read" "${URL_DOUYIN_SHORT:-https://v.douyin.com/REPLACE_ME/}"

# X
call_tool "x.post.read" "${URL_X:-https://x.com/REPLACE/status/REPLACE_ME}"
call_tool "x.post.read" "${URL_TWITTER:-https://twitter.com/REPLACE/status/REPLACE_ME}"

echo "Done. 将 REPLACE_ME 换成真实公开链接；对比 platform_metadata.error_code 与 warnings。"
