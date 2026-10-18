#!/usr/bin/env bash
# 校验 workflow 中的每个外部 Action 都固定到完整 40 位 commit SHA。
# 浮动 tag (v4 / main) 可被上游静默改写, 是供应链攻击的常见入口。
# 本地 action (./...) 与 docker:// 镜像不在本检查范围。

set -euo pipefail

bad=0
while IFS= read -r line; do
  ref="${line#*uses:}"
  ref="${ref%%#*}"
  ref="$(printf '%s' "$ref" | xargs)"

  case "$ref" in
    ""|./*|docker://*) continue ;;
  esac

  if [[ ! "$ref" =~ @[0-9a-f]{40}$ ]]; then
    echo "unpinned action: $line" >&2
    bad=1
  fi
done < <(grep -Rh -E '^[[:space:]]*-?[[:space:]]*uses:' .github/workflows || true)

exit "$bad"
