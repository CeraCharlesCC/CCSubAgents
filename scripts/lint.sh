#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${GOLANGCI_CONFIG_FILE:-$ROOT_DIR/.golangci.yml}"

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint is required. Install the pinned version from README.md first." >&2
  exit 127
fi

expected_golangci_version="2.9.0"
actual_golangci_version="$(
  golangci-lint version 2>/dev/null | sed -n 's/.*version \([0-9][^ ]*\).*/\1/p' | head -n 1
)"
if [ "$actual_golangci_version" != "$expected_golangci_version" ]; then
  echo "golangci-lint v${expected_golangci_version} is required; found v${actual_golangci_version:-unknown}. Install the pinned version from README.md first." >&2
  exit 127
fi

for module in ccsubagents local-artifact; do
  echo "==> gofmt (${module})"
  (
    cd "$ROOT_DIR/$module"
    mapfile -t go_files < <(find . -name '*.go' -type f)
    if [ "${#go_files[@]}" -gt 0 ]; then
      unformatted="$(gofmt -l "${go_files[@]}")"
      if [ -n "$unformatted" ]; then
        printf '%s\n' "$unformatted"
        exit 1
      fi
    fi
  )

  echo "==> golangci-lint (${module})"
  (
    cd "$ROOT_DIR/$module"
    if [ "$#" -eq 0 ]; then
      golangci-lint run --config "$CONFIG_FILE" ./...
    else
      golangci-lint run --config "$CONFIG_FILE" "$@"
    fi
  )
done
