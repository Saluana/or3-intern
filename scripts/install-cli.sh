#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bin_dir="$(go env GOBIN)"
if [[ -z "$bin_dir" ]]; then
  gopath="$(go env GOPATH)"
  if [[ -n "$gopath" ]]; then
    bin_dir="$gopath/bin"
  else
    bin_dir="$HOME/go/bin"
  fi
fi

mkdir -p "$bin_dir"
go install ./cmd/or3-intern

binary_path="$bin_dir/or3-intern"
if [[ ! -x "$binary_path" ]]; then
  echo "install failed: $binary_path was not created" >&2
  exit 1
fi

path_with_colons=":${PATH:-}:"
if [[ "$path_with_colons" == *":$bin_dir:"* ]]; then
  echo "Installed or3-intern to $binary_path"
  echo "Command is ready: or3-intern version"
  exit 0
fi

for link_dir in /opt/homebrew/bin /usr/local/bin; do
  if [[ -d "$link_dir" && -w "$link_dir" ]]; then
    ln -sf "$binary_path" "$link_dir/or3-intern"
    echo "Installed or3-intern to $binary_path"
    echo "Linked $link_dir/or3-intern -> $binary_path"
    echo "Command is ready: or3-intern version"
    exit 0
  fi
done

echo "Installed or3-intern to $binary_path"
echo "Add this directory to your PATH to use the bare command:"
echo "  export PATH=\"$bin_dir:\$PATH\""
echo "Then run: or3-intern version"
