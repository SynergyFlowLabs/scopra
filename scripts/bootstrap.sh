#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' 'Go 1.26.9 is required. Install the Go toolchain before continuing.' >&2
  exit 1
fi

printf 'Using %s\n' "$(go version)"
go mod download
