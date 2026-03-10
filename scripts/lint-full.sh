#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOLANGCI_CONFIG_FILE="$ROOT_DIR/.golangci-full.yml"

"$ROOT_DIR/scripts/lint.sh" "$@"
