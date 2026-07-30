#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

search() {
  if command -v rg >/dev/null 2>&1; then
    rg -n --glob '*.go' --glob '!*_test.go' "$@"
  else
    grep -RInE --include='*.go' --exclude='*_test.go' "$@"
  fi
}

# Symbols the runner-first cleanup must have eliminated from production Go code.
patterns=(
  'agent\.Runtime\b'
  'agent\.SubagentManager\b'
  'tools\.Registry\b'
  'tools\.Tool\b'
  'ReplayToolCall\b'
  '"or3-intern/internal/agent"'
  '/internal/v1/turns'
  '/internal/v1/subagents'
)

paths=(cmd internal)

failed=0
for pattern in "${patterns[@]}"; do
  if search "$pattern" "${paths[@]}"; then
    printf 'FAIL: forbidden symbol %s found in production code\n' "$pattern"
    failed=1
  fi
done

exit "$failed"
