#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

patterns=(
  'agent\.Runtime'
  'agent\.SubagentManager'
  'tools\.Registry'
  'tools\.Tool'
  'ReplayToolCall'
  '/internal/v1/turns'
  '/internal/v1/subagents'
)

paths=(
  cmd
  internal
)

failed=0
for pattern in "${patterns[@]}"; do
  if rg -n --glob '*.go' "$pattern" "${paths[@]}"; then
    failed=1
  fi
done

exit "$failed"
