#!/usr/bin/env bash
# Build the converge transport binary.
# Run from anywhere; the script cd's to the source tree.
set -euo pipefail

dir=$(cd "$(dirname "$0")" && pwd)
cd "$dir/go"

out="$dir/bin/converge"
mkdir -p "$dir/bin"

CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$out" ./cmd/converge
echo "built $out"
"$out" --help >/dev/null
echo "smoke: --help OK"
