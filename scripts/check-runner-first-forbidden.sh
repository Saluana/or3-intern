#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

search() {
  if command -v rg >/dev/null 2>&1; then
    rg -n --glob '*.go' --glob '!*_test.go' "$@"
  else
    grep -RIn --include='*.go' --exclude='*_test.go' "$@"
  fi
}

patterns=(
  'agent\.Runtime'
  'agent\.SubagentManager'
  'tools\.Registry'
  '"or3-intern/internal/agent"'
)

paths=(cmd internal)

failed=0
for pattern in "${patterns[@]}"; do
  if search "$pattern" "${paths[@]}"; then
    failed=1
  fi
done

exit "$failed"
