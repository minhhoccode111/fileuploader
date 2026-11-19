#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: $0 <size>[B|K|M|G] [output]" >&2
    echo "  output: file path or directory; defaults to testdata/manual-upload-<size>.bin" >&2
    echo "Examples: $0 5B, $0 5K /tmp/upload.bin, $0 5M /tmp/out/" >&2
    exit 2
}

if [[ $# -lt 1 || $# -gt 2 ]]; then
    usage
fi

size_arg="$1"
if [[ ! "$size_arg" =~ ^([0-9]+)([BbKkMmGg])$ ]]; then
    usage
fi

num="${BASH_REMATCH[1]}"
unit="${BASH_REMATCH[2]}"

case "$unit" in
    [Bb]) factor=1 ;;
    [Kk]) factor=1024 ;;
    [Mm]) factor=$((1024 * 1024)) ;;
    [Gg]) factor=$((1024 * 1024 * 1024)) ;;
esac

bytes=$((num * factor))
label="${num}${unit,,}"

if [[ $# -eq 1 ]]; then
    out="testdata/manual-upload-${label}.bin"
else
    target="$2"
    if [[ -d "$target" || "$target" == */ ]]; then
        mkdir -p "$target"
        out="${target%/}/manual-upload-${label}.bin"
    else
        out="$target"
        mkdir -p "$(dirname "$out")"
    fi
fi

echo "Generating ${bytes} bytes into ${out}"
head -c "$bytes" /dev/urandom > "$out"

actual="$(stat -c '%s' "$out")"
if [[ "$actual" -ne "$bytes" ]]; then
    echo "error: wrote ${actual} bytes, expected ${bytes}" >&2
    exit 1
fi

echo "Created ${out} (${actual} bytes)"